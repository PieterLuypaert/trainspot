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

type JourneyOptions struct {
	From  string
	To    string
	Width int
	Now   time.Time
	// Updated is when iRail worked the journeys out.
	Updated  time.Time
	Location *time.Location
}

const (
	stopIndent = "    "
	minStation = 14
	// minLabel is the room kept for the train beside a long station name.
	minLabel = 10
)

// Journeys lists each journey as a summary line with its stops and changes
// below it.
func Journeys(list []irail.Journey, o JourneyOptions) string {
	title := bold.Render(o.From) + " → " + bold.Render(o.To)
	lines := []string{header([]string{title}, o.Now.In(o.Location), o.Width), ""}
	if len(list) == 0 {
		lines = append(lines, indent+"No journeys found.", "")
	}
	l := LayOutJourneys(list, o)
	for i, summary := range l.Summaries {
		lines = append(append(lines, indent+summary), l.Stops[i]...)
		lines = append(lines, "")
	}
	lines = append(lines, l.Legend...)
	lines = append(lines, clip(Footer(o.Updated, o.Location), o.Width))
	return strings.Join(lines, "\n") + "\n"
}

// JourneyLines are the journeys drawn line by line, for a caller that lays
// them out itself.
type JourneyLines struct {
	// Summaries holds a line per journey without indent, short enough for
	// a two-column marker in front of it.
	Summaries []string
	// Stops holds the indented stop and change lines of each journey.
	Stops [][]string
	// Legend explains the marks of all summaries and stops, and Legends[i]
	// those of the summaries and journey i's stops, for a caller that
	// shows the stops of one journey only.
	Legend  []string
	Legends [][]string
}

func LayOutJourneys(list []irail.Journey, o JourneyOptions) JourneyLines {
	v := newJourneyView(list, o, false)
	var l JourneyLines
	var summaries [][]string
	changes := make([][]irail.Change, len(list))
	for i, j := range list {
		changes[i] = j.Changes()
		summaries = append(summaries, v.summary(j, changes[i]))
	}
	summaryNotes, all := v.notes, slices.Clone(v.notes)
	for i, j := range list {
		v.notes = slices.Clone(summaryNotes)
		var stops []string
		for _, line := range v.stops(j, changes[i]) {
			stops = append(stops, clip(line, o.Width))
		}
		l.Stops = append(l.Stops, stops)
		l.Legends = append(l.Legends, v.legendLines(v.notes))
		for _, n := range v.notes {
			all = addNote(all, &n)
		}
	}
	for _, line := range alignSummaries(summaries) {
		l.Summaries = append(l.Summaries, clip(line, o.Width-len(indent)))
	}
	l.Legend = v.legendLines(all)
	return l
}

// JourneyStops draws every stop of one journey: per leg its train, where
// it leaves, each station on the way and where it arrives, with the
// changes in between, and the legend for what it shows. Stations the train
// has passed are dimmed.
func JourneyStops(j irail.Journey, o JourneyOptions) (lines, legendLines []string) {
	v := newJourneyView([]irail.Journey{j}, o, true)
	for _, line := range v.stops(j, j.Changes()) {
		lines = append(lines, clip(line, o.Width))
	}
	return lines, v.legendLines(v.notes)
}

// RouteStops draws every stop of a train's run, as JourneyStops does for a
// leg, without the train's own line: a caller names the train itself.
func RouteStops(r irail.Route, o JourneyOptions) (lines, legendLines []string) {
	lines, legendLines = JourneyStops(irail.Journey{Legs: []irail.Leg{r.Leg()}}, o)
	return lines[1:], legendLines
}

type journeyView struct {
	o JourneyOptions
	// detailed puts each train on a line of its own above its leg, lists
	// the calls on the way and dims what the train has passed. Otherwise a
	// leg is two stop lines with the train beside the first.
	detailed bool
	// Widths of the delay, station and platform columns of the stop lines.
	delay, station, platform int
	notes                    []note
}

// newJourneyView sizes the columns for the lines the view will draw.
func newJourneyView(list []irail.Journey, o JourneyOptions, detailed bool) *journeyView {
	v := &journeyView{o: o, detailed: detailed}
	for _, j := range list {
		for _, leg := range j.Legs {
			for _, s := range []irail.Stop{leg.Departure, leg.Arrival} {
				v.fit(s.Delay, s.Station, v.stopPlatform(s), s.Cancelled)
			}
			if detailed {
				for _, c := range leg.Calls {
					_, delay := callTime(c)
					v.fit(delay, callMark+c.Station, callPlatform(c), c.Cancelled)
				}
			}
		}
	}
	if room := o.Width - v.beforeStation() - 2 - v.platform - 2 - minLabel; v.station > room {
		v.station = max(minStation, room)
	}
	return v
}

func (v *journeyView) fit(delay time.Duration, station string, platform cell, cancelled bool) {
	if !cancelled {
		v.delay = max(v.delay, lipgloss.Width(delayText(delay)))
	}
	v.station = max(v.station, lipgloss.Width(station))
	v.platform = max(v.platform, lipgloss.Width(platform.text))
}

// beforeStation is how far in the station column starts.
func (v *journeyView) beforeStation() int {
	return len(stopIndent) + len("15:04") + lipgloss.Width(v.delayColumn("")) + len(gap)
}

// delayColumn pads a delay mark to the column, which takes no room at all
// when no stop is late.
func (v *journeyView) delayColumn(mark string) string {
	if v.delay == 0 {
		return ""
	}
	return " " + mark + strings.Repeat(" ", v.delay-lipgloss.Width(mark))
}

func (v *journeyView) legendLines(notes []note) []string {
	var lines []string
	for _, line := range legend(noteTexts(slices.Clone(notes)), v.o.Width) {
		lines = append(lines, clip(indent+line, v.o.Width))
	}
	return lines
}

// summary is the parts of a journey's summary line: its times, how long it
// takes and its changes, or that it is cancelled, and last the day when it
// does not leave today, so that a narrow terminal cuts that first.
func (v *journeyView) summary(j irail.Journey, changes []irail.Change) []string {
	dep, arr := j.Departure(), j.Arrival()
	start := bold.Render(clock(dep.Scheduled, v.o.Location))
	end := bold.Render(clock(arr.Scheduled, v.o.Location))
	var parts []string
	if j.Cancelled() {
		parts = []string{start + " → " + end, alarm.Render("cancelled")}
	} else {
		times := start + v.delayMark(dep.Delay, " ") + " → " + end + v.delayMark(arr.Delay, " ")
		parts = []string{times, formatDuration(j.Duration()), changesText(len(changes))}
		if slices.ContainsFunc(changes, irail.Change.Missed) {
			parts = append(parts, alarm.Render("change missed"))
		}
	}
	if day := v.day(dep.Scheduled); day != "" {
		parts = append(parts, day)
	}
	return parts
}

// alignedColumns is how many summary columns line up: the times and the
// durations.
const alignedColumns = 2

// alignSummaries pads the first columns to their widest entry, so the
// durations and changes of all journeys line up.
func alignSummaries(rows [][]string) []string {
	widths := make([]int, alignedColumns)
	for _, parts := range rows {
		for i := range min(alignedColumns, len(parts)-1) {
			widths[i] = max(widths[i], lipgloss.Width(parts[i]))
		}
	}
	lines := make([]string, 0, len(rows))
	for _, parts := range rows {
		padded := slices.Clone(parts)
		for i := range min(alignedColumns, len(parts)-1) {
			padded[i] += strings.Repeat(" ", widths[i]-lipgloss.Width(parts[i]))
		}
		lines = append(lines, strings.Join(padded, "   "))
	}
	return lines
}

// stops lists every stop, with a change before each train after the
// first, which is how Changes counts them.
func (v *journeyView) stops(j irail.Journey, changes []irail.Change) []string {
	var lines []string
	for i, leg := range j.Legs {
		if i > 0 && !leg.Walking {
			lines = append(lines, v.change(changes[0]))
			changes = changes[1:]
		}
		if !v.detailed {
			lines = append(lines, v.stop(leg.Departure, legLabel(leg, plain)), v.stop(leg.Arrival, ""))
			continue
		}
		lines = append(lines, indent+legLabel(leg, bold), v.stop(leg.Departure, ""))
		// passed follows the train along the leg, to mark where it is.
		passed := leg.Departure.Passed
		for _, c := range leg.Calls {
			lines = append(lines, v.call(c, position(c.Arrived, c.Passed, passed)))
			passed = c.Passed
		}
		lines = append(lines, v.stop(leg.Arrival, position(false, leg.Arrival.Passed, passed)))
	}
	return lines
}

func legLabel(leg irail.Leg, train lipgloss.Style) string {
	switch {
	case leg.Walking:
		return faint.Render("walk")
	case leg.Direction != "":
		return train.Render(leg.Train) + faint.Render(" to "+leg.Direction)
	}
	return train.Render(leg.Train)
}

// stop draws where a leg leaves or arrives. A stop the train has passed
// is dimmed.
func (v *journeyView) stop(s irail.Stop, label string) string {
	style, delay := plain, ""
	switch {
	case s.Cancelled:
		style = faint
		label = strings.TrimSpace(label + "  " + alarm.Render("cancelled"))
	case v.dims(s.Passed):
		style, delay = faint, v.delayMark(s.Delay, "")
	default:
		delay = v.delayMark(s.Delay, "")
	}
	return v.line(style, s.Scheduled, delay, s.Station, v.stopPlatform(s), label)
}

// dims tells whether to dim a passed stop, which only the detailed view
// does.
func (v *journeyView) dims(passed bool) bool {
	return v.detailed && passed
}

// stopPlatform is the platform a stop is drawn with, so that the columns
// are measured on it too.
func (v *journeyView) stopPlatform(s irail.Stop) cell {
	return platformCell(s.Platform, s.PlatformChanged, s.Cancelled || v.dims(s.Passed))
}

// callMark sets the stations on the way apart from where legs leave and
// arrive.
const callMark = "· "

// call draws a station the train stops at on the way, with its departure
// time; dimmed once the train has left it.
// call draws a station the train stops at on the way, after the label
// that says where the train is, if any.
func (v *journeyView) call(c irail.Call, where string) string {
	at, late := callTime(c)
	style, delay, label := plain, v.delayMark(late, ""), where
	switch {
	case c.Cancelled:
		style, delay, label = faint, "", alarm.Render("cancelled")
	case c.EndsHere:
		label = strings.TrimSpace(where + "  " + alarm.Render("train ends here"))
	case c.Extra:
		label = strings.TrimSpace(where + "  " + faint.Render("extra stop"))
	}
	if c.Passed {
		style = faint
	}
	return v.line(style, at, delay, callMark+c.Station, callPlatform(c), label)
}

// position says where the train is, for a stop it has arrived at or
// passed, after the stop before it was passed or not: at this stop, or
// heading for it.
func position(arrived, passed, passedBefore bool) string {
	switch {
	case arrived && !passed:
		return green.Render("train is here")
	case passedBefore && !passed:
		return green.Render("next stop")
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

// callPlatform leaves the platform blank when NMBS does not know it, which
// on the way is nearly always: a column of dashes would only be noise.
func callPlatform(c irail.Call) cell {
	if c.Platform == "" {
		return cell{}
	}
	return platformCell(c.Platform, c.PlatformChanged, c.Cancelled || c.Passed)
}

func (v *journeyView) line(style lipgloss.Style, at time.Time, delay, station string, platform cell, label string) string {
	v.notes = addNote(v.notes, platform.note)
	line := stopIndent + style.Render(clock(at, v.o.Location)) + v.delayColumn(delay) + gap +
		pad(style.Render(ansi.Truncate(station, v.station, "…")), v.station, false) +
		pad(platform.style.Render(platform.text), v.platform, label == "")
	if label != "" {
		if room := v.o.Width - lipgloss.Width(line); room > 0 {
			line += ansi.Truncate(label, room, "…")
		}
	}
	return strings.TrimRight(line, " ")
}

func (v *journeyView) change(c irail.Change) string {
	real, planned := formatDuration(c.Real), formatDuration(c.Planned)
	var text string
	switch {
	case c.Missed():
		text = alarm.Render("change missed by " + formatDuration(max(time.Minute, -c.Real)))
	case c.Tight():
		text = yellow.Render("change " + real + ", tight")
	case real != planned:
		text = faint.Render("change " + real + " instead of " + planned)
	default:
		text = faint.Render("change " + real)
	}
	return strings.Repeat(" ", v.beforeStation()) + text
}

// delayMark is the coloured +N after a time, with the space before it,
// or nothing for a train on time.
func (v *journeyView) delayMark(delay time.Duration, space string) string {
	text := delayText(delay)
	if text == "" {
		return ""
	}
	v.notes = addNote(v.notes, lateNote(minutes(delay)))
	return space + delayStyle(delay).Render(text)
}

func delayText(delay time.Duration) string {
	if late := minutes(delay); late > 0 {
		return fmt.Sprintf("+%d", late)
	}
	return ""
}

// day names the day of a journey that does not leave today, as at night.
func (v *journeyView) day(t time.Time) string {
	today := v.o.Now.In(v.o.Location).Format("2006-01-02")
	if t.In(v.o.Location).Format("2006-01-02") == today {
		return ""
	}
	return faint.Render(t.In(v.o.Location).Format("Mon 2 Jan"))
}

// clip shortens a line that would not fit the width even after the
// columns made way, as in a very narrow terminal.
func clip(line string, width int) string {
	return ansi.Truncate(line, width, "…")
}

func formatDuration(d time.Duration) string {
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%dh%02d", minutes/60, minutes%60)
}

func changesText(n int) string {
	switch n {
	case 0:
		return "direct"
	case 1:
		return "1 change"
	}
	return fmt.Sprintf("%d changes", n)
}
