package board

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PieterLuypaert/trainspot/internal/irail"
)

// The detailed view draws a journey, or one train's run, as a route line:
// per train a header with how long it rides and how often it stops, then
// every station down a line of markers, and each change as a block of its
// own with the platforms to walk between.

const (
	// endMarker is where a train is boarded or left, hereMarker where the
	// train is now, callMarker every other station on the way.
	endMarker  = "●"
	hereMarker = "◆"
	callMarker = "│"
)

// JourneyStops draws every stop of one journey down a route line, with the
// legend for what it shows. Stations the train has passed are dimmed.
func JourneyStops(j irail.Journey, o JourneyOptions) (lines, legendLines []string) {
	r := newRouteLine([]irail.Leg(j.Legs), o, "")
	changes := j.Changes()
	for i, leg := range j.Legs {
		if i > 0 && !leg.Walking {
			lines = append(lines, "", r.change(changes[0], j.Legs[i-1].Arrival, leg.Departure), "")
			changes = changes[1:]
		}
		lines = append(lines, r.header(leg))
		if !leg.Walking {
			lines = append(lines, r.leg(leg)...)
		}
	}
	return clipAll(lines, o.Width), r.legend()
}

// RouteStops draws every stop of a train's run down a route line, with the
// legend. A caller names the train itself; mark is a station to draw like
// a leg's end, such as the station whose board the train was chosen on.
func RouteStops(rt irail.Route, o JourneyOptions, mark string) (lines, legendLines []string) {
	leg := rt.Leg()
	r := newRouteLine([]irail.Leg{leg}, o, mark)
	return clipAll(r.leg(leg), o.Width), r.legend()
}

// JourneySummary says how long a journey takes, how often you change, and
// the platforms it leaves from and arrives at.
func JourneySummary(j irail.Journey) string {
	if j.Cancelled() {
		return alarm.Render("cancelled")
	}
	changes := j.Changes()
	parts := []string{formatDuration(j.Duration()), changesText(len(changes))}
	if slices.ContainsFunc(changes, irail.Change.Missed) {
		parts = append(parts, alarm.Render("change missed"))
	}
	if p := j.Departure().Platform; p != "" {
		parts = append(parts, "leaves platform "+bold.Render(p))
	}
	if p := j.Arrival().Platform; p != "" {
		parts = append(parts, "arrives platform "+bold.Render(p))
	}
	return strings.Join(parts, faint.Render(" · "))
}

// RouteSummary says how long a train's run takes, between which stations,
// and how often it stops on the way.
func RouteSummary(rt irail.Route) string {
	leg := rt.Leg()
	if len(rt.Stops) < 2 {
		return ""
	}
	return formatDuration(leg.Arrival.Expected().Sub(leg.Departure.Expected())) + faint.Render(" · from ") +
		leg.Departure.Station + faint.Render(" to ") + leg.Arrival.Station + faint.Render(" · ") + stopsText(leg.Calls)
}

type routeLine struct {
	o JourneyOptions
	// mark is a station drawn like a leg's end.
	mark string
	// Widths of the delay and station columns.
	delay, station int
	notes          []note
}

func newRouteLine(legs []irail.Leg, o JourneyOptions, mark string) *routeLine {
	r := &routeLine{o: o, mark: mark}
	fit := func(delay time.Duration, station string, cancelled bool) {
		if !cancelled {
			r.delay = max(r.delay, lipgloss.Width(delayText(delay)))
		}
		r.station = max(r.station, lipgloss.Width(station))
	}
	for _, leg := range legs {
		if leg.Walking {
			continue
		}
		fit(leg.Departure.Delay, leg.Departure.Station, leg.Departure.Cancelled)
		fit(leg.Arrival.Delay, leg.Arrival.Station, leg.Arrival.Cancelled)
		for _, c := range leg.Calls {
			_, delay := callTime(c)
			fit(delay, c.Station, c.Cancelled)
		}
	}
	return r
}

// header names a leg's train and where it heads, with how long it rides
// and how often it stops, on the right.
func (r *routeLine) header(leg irail.Leg) string {
	took := formatDuration(leg.Arrival.Expected().Sub(leg.Departure.Expected()))
	if leg.Walking {
		return r.spread(indent+faint.Render("Walk to "+leg.Arrival.Station), faint.Render(took), "")
	}
	return r.spread(indent+legLabel(leg, bold), faint.Render(took+" · "+stopsText(leg.Calls)), "")
}

// leg draws a leg's stations down the route line, marking where the train
// is: at a station, or heading for the next one.
func (r *routeLine) leg(leg irail.Leg) []string {
	lines := []string{r.end(leg.Departure, position(leg.Departure.Arrived, leg.Departure.Passed, false))}
	// passed follows the train along the leg. A skipped stop is never
	// left, so it does not hold the train back.
	passed := leg.Departure.Passed
	for _, c := range leg.Calls {
		if c.Cancelled {
			lines = append(lines, r.call(c, ""))
			continue
		}
		lines = append(lines, r.call(c, position(c.Arrived, c.Passed, passed)))
		passed = c.Passed
	}
	where := position(false, leg.Arrival.Passed, passed)
	if leg.Arrival.Cancelled {
		where = ""
	}
	return append(lines, r.end(leg.Arrival, where))
}

// end draws where a leg leaves or arrives, in bold with its platform.
func (r *routeLine) end(s irail.Stop, where string) string {
	style, delay, label := bold, r.delayMark(s.Delay), positionLabel(where)
	switch {
	case s.Cancelled:
		style, delay, label = faint, "", alarm.Render("cancelled")
	case s.Passed:
		style = faint
	}
	marker := endMarker
	if where == hereLabel {
		marker = hereMarker
	}
	platform := platformCell(s.Platform, s.PlatformChanged, s.Cancelled || s.Passed)
	return r.line(s.Scheduled, delay, marker, style, s.Station, label, platform)
}

// call draws a station on the way with its departure time. The marked
// station is drawn like a leg's end.
func (r *routeLine) call(c irail.Call, where string) string {
	at, late := callTime(c)
	marked := r.mark != "" && c.Station == r.mark
	style, delay, label := plain, r.delayMark(late), positionLabel(where)
	if marked {
		style = bold
	}
	switch {
	case c.Cancelled:
		style, delay, label = faint, "", alarm.Render("cancelled")
	case c.EndsHere:
		label = strings.TrimSpace(label + "  " + alarm.Render("train ends here"))
	case c.StartsHere:
		label = strings.TrimSpace(label + "  " + alarm.Render("train starts here"))
	case c.Extra:
		label = strings.TrimSpace(label + "  " + faint.Render("extra stop"))
	}
	marker, platform := callMarker, cell{}
	if marked {
		marker, platform = endMarker, platformCell(c.Platform, c.PlatformChanged, c.Cancelled || c.Passed)
	}
	if where == hereLabel {
		marker = hereMarker
	}
	if c.Passed {
		style = faint
	}
	return r.line(at, delay, marker, style, c.Station, label, platform)
}

// change is the block between two trains: where to change, the time the
// delays leave, and the platforms to walk between.
func (r *routeLine) change(c irail.Change, from, to irail.Stop) string {
	real, planned := formatDuration(c.Real), formatDuration(c.Planned)
	var took string
	switch {
	case c.Missed():
		took = alarm.Render("missed by " + formatDuration(max(time.Minute, -c.Real)))
	case c.Tight():
		took = yellow.Render(real + ", tight")
	case real != planned:
		took = real + faint.Render(" instead of "+planned)
	default:
		took = real
	}
	parts := []string{"Change at " + bold.Render(to.Station), took}
	switch {
	case from.Platform == "" || to.Platform == "":
	case from.Platform == to.Platform:
		parts = append(parts, "same platform "+bold.Render(to.Platform))
	default:
		parts = append(parts, "platform "+bold.Render(from.Platform)+" → "+bold.Render(to.Platform))
	}
	return indent + strings.Join(parts, faint.Render(" · "))
}

// line lays a station out: its time and delay, the marker on the route
// line, the station and what it says about the train, and its platform on
// the right.
func (r *routeLine) line(at time.Time, delay, marker string, style lipgloss.Style, station, label string, platform cell) string {
	left := indent + style.Render(clock(at, r.o.Location)) + r.delayColumn(delay) + gap +
		style.Render(marker) + gap + style.Render(station)
	if label != "" {
		left += strings.Repeat(" ", max(0, r.station-lipgloss.Width(station))) + gap + label
	}
	right := ""
	if platform.text != "" {
		r.notes = addNote(r.notes, platform.note)
		right = faint.Render("platform ") + platform.style.Render(platform.text)
	}
	return r.spread(left, right, platform.style.Render(platform.text))
}

// spread puts right at the right edge of the width, leaving the margin
// the left side starts with, or short instead of right when that does not
// fit.
func (r *routeLine) spread(left, right, short string) string {
	room := r.o.Width - len(indent)
	for _, side := range []string{right, short} {
		if space := room - lipgloss.Width(left) - lipgloss.Width(side); side != "" && space >= len(gap) {
			return left + strings.Repeat(" ", space) + side
		}
	}
	return left
}

func (r *routeLine) delayMark(delay time.Duration) string {
	text := delayText(delay)
	if text == "" {
		return ""
	}
	r.notes = addNote(r.notes, lateNote(minutes(delay)))
	return delayStyle(delay).Render(text)
}

// delayColumn pads a delay mark to the column, which takes no room when no
// station is late.
func (r *routeLine) delayColumn(mark string) string {
	if r.delay == 0 {
		return ""
	}
	return " " + mark + strings.Repeat(" ", r.delay-lipgloss.Width(mark))
}

func (r *routeLine) legend() []string {
	var lines []string
	for _, line := range legend(noteTexts(slices.Clone(r.notes)), r.o.Width) {
		lines = append(lines, clip(indent+line, r.o.Width))
	}
	return lines
}

const hereLabel = "train is here"

func positionLabel(where string) string {
	if where == "" {
		return ""
	}
	return green.Render(where)
}

// position says where the train is, for a stop it has arrived at or
// passed, after the stop before it was passed or not: at this stop, or
// heading for it.
func position(arrived, passed, passedBefore bool) string {
	switch {
	case arrived && !passed:
		return hereLabel
	case passedBefore && !passed:
		return "next stop"
	}
	return ""
}

// callTime is the time a call shows: when the train leaves, or when it
// arrives where it now ends. A skipped stop shows no delay.
func callTime(c irail.Call) (time.Time, time.Duration) {
	switch {
	case c.Cancelled:
		return c.Departure, 0
	case c.EndsHere:
		return c.Arrival, c.ArrivalDelay
	}
	return c.Departure, c.DepartureDelay
}

// stopsText counts the stations a train stops at on the way, leaving out
// the ones it skips.
func stopsText(calls []irail.Call) string {
	n := 0
	for _, c := range calls {
		if !c.Cancelled {
			n++
		}
	}
	switch n {
	case 0:
		return "no stops on the way"
	case 1:
		return "1 stop on the way"
	}
	return fmt.Sprintf("%d stops on the way", n)
}

func clipAll(lines []string, width int) []string {
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return lines
}
