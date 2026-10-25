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
	Stops  [][]string
	Legend []string
}

func LayOutJourneys(list []irail.Journey, o JourneyOptions) JourneyLines {
	v := newJourneyView(list, o)
	var l JourneyLines
	var summaries [][]string
	for _, j := range list {
		changes := j.Changes()
		summaries = append(summaries, v.summary(j, changes))
		var stops []string
		for _, line := range v.stops(j, changes) {
			stops = append(stops, clip(line, o.Width))
		}
		l.Stops = append(l.Stops, stops)
	}
	for _, line := range alignSummaries(summaries) {
		l.Summaries = append(l.Summaries, clip(line, o.Width-len(indent)))
	}
	for _, line := range legend(noteTexts(v.notes), o.Width) {
		l.Legend = append(l.Legend, clip(indent+line, o.Width))
	}
	return l
}

type journeyView struct {
	o JourneyOptions
	// Widths of the delay, station and platform columns of the stop lines.
	delay, station, platform int
	notes                    []note
}

func newJourneyView(list []irail.Journey, o JourneyOptions) *journeyView {
	v := &journeyView{o: o}
	for _, j := range list {
		for _, leg := range j.Legs {
			for _, s := range []irail.Stop{leg.Departure, leg.Arrival} {
				if !s.Cancelled {
					v.delay = max(v.delay, lipgloss.Width(delayText(s.Delay)))
				}
				v.station = max(v.station, lipgloss.Width(s.Station))
				v.platform = max(v.platform, lipgloss.Width(platformCell(s.Platform, s.PlatformChanged, s.Cancelled).text))
			}
		}
	}
	if room := o.Width - v.beforeStation() - 2 - v.platform - 2 - minLabel; v.station > room {
		v.station = max(minStation, room)
	}
	return v
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

// summary is the parts of a journey's summary line: its times, how long it
// takes and its changes, or that it is cancelled.
func (v *journeyView) summary(j irail.Journey, changes []irail.Change) []string {
	dep, arr := j.Departure(), j.Arrival()
	start := v.day(dep.Scheduled) + bold.Render(clock(dep.Scheduled, v.o.Location))
	end := bold.Render(clock(arr.Scheduled, v.o.Location))
	if j.Cancelled() {
		return []string{start + " → " + end, alarm.Render("cancelled")}
	}
	times := start + v.delayMark(dep.Delay, " ") + " → " + end + v.delayMark(arr.Delay, " ")
	parts := []string{times, formatDuration(j.Duration()), changesText(len(changes))}
	if slices.ContainsFunc(changes, irail.Change.Missed) {
		parts = append(parts, alarm.Render("change missed"))
	}
	return parts
}

// alignSummaries pads every column but the last to its widest entry, so
// the durations and changes of all journeys line up.
func alignSummaries(rows [][]string) []string {
	var widths []int
	for _, parts := range rows {
		for i, part := range parts[:len(parts)-1] {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], lipgloss.Width(part))
		}
	}
	lines := make([]string, 0, len(rows))
	for _, parts := range rows {
		line := ""
		for i, part := range parts[:len(parts)-1] {
			line += part + strings.Repeat(" ", widths[i]-lipgloss.Width(part)) + "   "
		}
		lines = append(lines, line+parts[len(parts)-1])
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
		lines = append(lines, v.stop(leg.Departure, legLabel(leg)), v.stop(leg.Arrival, ""))
	}
	return lines
}

func legLabel(leg irail.Leg) string {
	switch {
	case leg.Walking:
		return faint.Render("walk")
	case leg.Direction != "":
		return leg.Train + faint.Render(" to "+leg.Direction)
	}
	return leg.Train
}

func (v *journeyView) stop(s irail.Stop, label string) string {
	timeStyle, delay := faint, ""
	if s.Cancelled {
		label = strings.TrimSpace(label + "  " + alarm.Render("cancelled"))
	} else {
		timeStyle, delay = plain, v.delayMark(s.Delay, "")
	}
	platform := platformCell(s.Platform, s.PlatformChanged, s.Cancelled)
	v.notes = addNote(v.notes, platform.note)
	line := stopIndent + timeStyle.Render(clock(s.Scheduled, v.o.Location)) + v.delayColumn(delay) + gap +
		pad(ansi.Truncate(s.Station, v.station, "…"), v.station, false) +
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
	return faint.Render(t.In(v.o.Location).Format("Mon 2 Jan")) + "  "
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
