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
	// minRouteStation is how narrow the station column gets before lines
	// are cut at the edge instead.
	minRouteStation = 10
)

// JourneyStops draws every stop of one journey down a route line, with the
// legend for what it shows. Stations the train has passed are dimmed.
func JourneyStops(j irail.Journey, o JourneyOptions) ([]string, Legend) {
	r := newRouteLine(j.Legs, o, Mark{})
	var lines []string
	changes := j.Changes()
	rode := false
	for i, leg := range j.Legs {
		switch {
		case leg.Walking && i > 0:
			lines = append(lines, "")
		case !leg.Walking && rode:
			lines = append(lines, "", r.change(changes[0], j.Legs[i-1].Arrival, leg.Departure), "")
			changes = changes[1:]
		}
		lines = append(lines, r.header(leg))
		if !leg.Walking {
			lines = append(lines, r.leg(leg)...)
			rode = true
		}
	}
	return clipAll(lines, o.Width), Legend{r.notes}
}

// Mark is a station on a train's run to draw like a leg's end, such as the
// station whose board the train was chosen on: found by its name and the
// time the train calls there, as a run can pass a station twice.
type Mark struct {
	Station string
	At      time.Time
}

func (k Mark) is(c irail.Call) bool {
	return k.Station != "" && c.Station == k.Station && (c.Departure.Equal(k.At) || c.Arrival.Equal(k.At))
}

// RouteStops draws every stop of a train's run down a route line, with the
// legend. A caller names the train itself.
func RouteStops(rt irail.Route, o JourneyOptions, mark Mark) ([]string, Legend) {
	if len(rt.Stops) < 2 {
		return nil, Legend{}
	}
	leg := rt.Leg()
	r := newRouteLine([]irail.Leg{leg}, o, mark)
	return clipAll(r.leg(leg), o.Width), Legend{r.notes}
}

// JourneySummary says how long a journey takes, how often you change, and
// the platforms it leaves from and arrives at.
func JourneySummary(j irail.Journey) string {
	if j.Cancelled() {
		return alarm.Render("cancelled")
	}
	parts := durationAndChanges(j, j.Changes())
	if p := j.Departure().Platform; p != "" {
		parts = append(parts, "leaves platform "+bold.Render(p))
	}
	if p := j.Arrival().Platform; p != "" {
		parts = append(parts, "arrives platform "+bold.Render(p))
	}
	return strings.Join(parts, faint.Render(" · "))
}

// RouteSummary says how long a train's run takes, between which stations,
// and how often it stops on the way, up to where it now ends.
func RouteSummary(rt irail.Route) string {
	if len(rt.Stops) < 2 {
		return ""
	}
	leg := rt.Leg()
	end, at := legEnd(leg)
	return formatDuration(at.Sub(leg.Departure.Expected())) + faint.Render(" · from ") +
		leg.Departure.Station + faint.Render(" to ") + end + faint.Render(" · ") + stopsText(leg.Calls)
}

// legEnd is where and when a leg really ends: at a call where the train
// now ends, or else where it arrives.
func legEnd(leg irail.Leg) (string, time.Time) {
	for _, c := range leg.Calls {
		if c.EndsHere {
			return c.Station, c.Arrival.Add(c.ArrivalDelay)
		}
	}
	return leg.Arrival.Station, leg.Arrival.Expected()
}

type routeLine struct {
	o    JourneyOptions
	mark Mark
	marks
	// station is the width of the station column; shortPlatforms leaves
	// out the word platform on every line when the long form does not fit.
	station        int
	shortPlatforms bool
}

func newRouteLine(legs []irail.Leg, o JourneyOptions, mark Mark) *routeLine {
	r := &routeLine{o: o, mark: mark}
	platform, label := 0, labelRoom(legs)
	fit := func(delay time.Duration, station, platformName string, cancelled bool) {
		if !cancelled {
			r.fitDelay(delay)
		}
		r.station = max(r.station, lipgloss.Width(station))
		if platformName != "" {
			platform = max(platform, lipgloss.Width(platformName+" !"))
		}
	}
	for _, leg := range legs {
		if leg.Walking {
			continue
		}
		fit(leg.Departure.Delay, leg.Departure.Station, leg.Departure.Platform, leg.Departure.Cancelled)
		fit(leg.Arrival.Delay, leg.Arrival.Station, leg.Arrival.Platform, leg.Arrival.Cancelled)
		for _, c := range leg.Calls {
			_, delay := callTime(c)
			name := ""
			if mark.is(c) {
				name = c.Platform
			}
			fit(delay, c.Station, name, c.Cancelled)
		}
	}
	// The station column gives way first, down to minRouteStation, so the
	// platform and what is said about the train stay in view.
	room := o.Width - len(indent) - r.beforeStation() - len(gap)
	if r.station > room-max(label, platform+len("platform ")) {
		r.shortPlatforms = true
		r.station = max(minRouteStation, min(r.station, room-max(label, platform)))
	}
	return r
}

// labelRoom is the room the widest thing said about a station takes, such
// as where the train is or that it ends there.
func labelRoom(legs []irail.Leg) int {
	room := 0
	for _, leg := range legs {
		if leg.Departure.Passed || leg.Departure.Arrived {
			room = max(room, len(hereLabel))
		}
		for _, c := range leg.Calls {
			switch {
			case c.Cancelled:
				room = max(room, len("cancelled"))
			case c.StartsHere:
				room = max(room, len("train starts here"))
			case c.EndsHere:
				room = max(room, len("train ends here"))
			case c.Extra:
				room = max(room, len("extra stop"))
			}
			if c.Passed || c.Arrived {
				room = max(room, len(hereLabel))
			}
		}
	}
	return room
}

// beforeStation is how far in the station starts on a line.
func (r *routeLine) beforeStation() int {
	return len(indent) + len("15:04") + lipgloss.Width(r.delayColumn("")) + len(gap) + 1 + len(gap)
}

// header names a leg's train and where it heads, with how long it rides
// and how often it stops, on the right.
func (r *routeLine) header(leg irail.Leg) string {
	end, at := legEnd(leg)
	took := formatDuration(at.Sub(leg.Departure.Expected()))
	if leg.Walking {
		return r.spread(indent+faint.Render("Walk to "+end), faint.Render(took))
	}
	right := faint.Render(took + " · " + stopsText(leg.Calls))
	if leg.Arrival.Cancelled && end == leg.Arrival.Station {
		right = alarm.Render("cancelled")
	}
	return r.spread(indent+legLabel(leg, bold), right)
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
	if s.Cancelled {
		return r.line(s.Scheduled, "", endMarker, faint, s.Station, alarm.Render("cancelled"), platformCell(s.Platform, s.PlatformChanged, true))
	}
	style := bold
	if s.Passed {
		style = faint
	}
	return r.line(s.Scheduled, r.lateMark(s.Delay), marker(endMarker, where), style, s.Station, positionLabel(where),
		platformCell(s.Platform, s.PlatformChanged, s.Passed))
}

// call draws a station on the way with its departure time. The marked
// station is drawn like a leg's end.
func (r *routeLine) call(c irail.Call, where string) string {
	at, late := callTime(c)
	marked := r.mark.is(c)
	style, base, platform := plain, callMarker, cell{}
	if marked {
		style, base, platform = bold, endMarker, platformCell(c.Platform, c.PlatformChanged, c.Cancelled || c.Passed)
	}
	if c.Cancelled {
		return r.line(at, "", base, faint, c.Station, alarm.Render("cancelled"), platform)
	}
	label := positionLabel(where)
	switch {
	case c.EndsHere:
		label = strings.TrimSpace(label + "  " + alarm.Render("train ends here"))
	case c.StartsHere:
		label = strings.TrimSpace(label + "  " + alarm.Render("train starts here"))
	case c.Extra:
		label = strings.TrimSpace(label + "  " + faint.Render("extra stop"))
	}
	if c.Passed {
		style = faint
	}
	return r.line(at, r.lateMark(late), marker(base, where), style, c.Station, label, platform)
}

// marker is the diamond where the train is, else the station's own marker.
func marker(base, where string) string {
	if where == hereLabel {
		return hereMarker
	}
	return base
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
// the right. A platform that does not fit is left out, and so is its note.
func (r *routeLine) line(at time.Time, delay, mark string, style lipgloss.Style, station, label string, platform cell) string {
	name := ansi.Truncate(station, r.station, "…")
	left := indent + style.Render(clock(at, r.o.Location)) + r.delayColumn(delay) + gap +
		style.Render(mark) + gap + style.Render(name)
	if label != "" {
		left += strings.Repeat(" ", max(0, r.station-lipgloss.Width(name))) + gap + label
	}
	if platform.text == "" {
		return left
	}
	right := platform.style.Render(platform.text)
	if !r.shortPlatforms {
		right = faint.Render("platform ") + right
	}
	line := r.spread(left, right)
	if line != left {
		r.note(platform.note)
	}
	return line
}

// spread puts right at the right edge of the width, leaving the margin
// the left side starts with, or leaves it out when it does not fit.
func (r *routeLine) spread(left, right string) string {
	space := r.o.Width - len(indent) - lipgloss.Width(left) - lipgloss.Width(right)
	if right == "" || space < len(gap) {
		return left
	}
	return left + strings.Repeat(" ", space) + right
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
		lines[i] = clip(lines[i], width)
	}
	return lines
}

// durationAndChanges are the parts of a summary that say how long a
// journey takes and how often you change, and whether a change is missed.
func durationAndChanges(j irail.Journey, changes []irail.Change) []string {
	parts := []string{formatDuration(j.Duration()), changesText(len(changes))}
	if slices.ContainsFunc(changes, irail.Change.Missed) {
		parts = append(parts, alarm.Render("change missed"))
	}
	return parts
}
