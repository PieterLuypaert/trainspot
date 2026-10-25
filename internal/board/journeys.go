package board

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PieterLuypaert/trainspot/internal/irail"
)

type JourneyOptions struct {
	From     string
	To       string
	Width    int
	Now      time.Time
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
	var out strings.Builder
	title := bold.Render(o.From) + " → " + bold.Render(o.To)
	out.WriteString(header([]string{title}, o.Now.In(o.Location), o.Width) + "\n\n")
	if len(list) == 0 {
		out.WriteString(indent + "No journeys found.\n\n")
	}
	v := newJourneyView(list, o)
	for _, j := range list {
		out.WriteString(indent + v.summary(j) + "\n")
		for _, line := range v.stops(j) {
			out.WriteString(line + "\n")
		}
		out.WriteString("\n")
	}
	for _, line := range legend(noteTexts(v.notes), o.Width) {
		out.WriteString(indent + line + "\n")
	}
	out.WriteString(indent + faint.Render("Live NMBS data via iRail, updated "+o.Now.In(o.Location).Format("15:04")) + "\n")
	return out.String()
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
				v.delay = max(v.delay, lipgloss.Width(delayText(s.Delay)))
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

func (v *journeyView) summary(j irail.Journey) string {
	dep, arr := j.Departure(), j.Arrival()
	times := bold.Render(v.clock(dep.Scheduled)) + v.delayMark(dep.Delay, " ") +
		" → " + bold.Render(v.clock(arr.Scheduled)) + v.delayMark(arr.Delay, " ")
	if j.Cancelled() {
		return times + "   " + alarm.Render("cancelled")
	}
	parts := []string{times, formatDuration(j.Duration()), changesText(len(j.Legs) - 1)}
	if j.Missed() {
		parts = append(parts, alarm.Render("change missed"))
	}
	return strings.Join(parts, "   ")
}

func (v *journeyView) stops(j irail.Journey) []string {
	var lines []string
	changes := j.Changes()
	for i, leg := range j.Legs {
		// The change before a walk is no change at all: the walk starts on
		// arrival.
		if i > 0 && !leg.Walking {
			lines = append(lines, v.change(changes[i-1]))
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
	timeStyle := bold
	if s.Cancelled {
		timeStyle = faint
		label = strings.TrimSpace(label + "  " + alarm.Render("cancelled"))
	}
	platform := platformCell(s.Platform, s.PlatformChanged, s.Cancelled)
	v.notes = addNote(v.notes, platform.note)
	line := stopIndent + timeStyle.Render(v.clock(s.Scheduled)) + v.delayColumn(v.delayMark(s.Delay, "")) + gap +
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
	real, planned := int(c.Real/time.Minute), int(c.Planned/time.Minute)
	var text string
	switch {
	case c.Missed():
		text = alarm.Render(fmt.Sprintf("change missed by %d min", max(1, -real)))
	case c.Tight():
		text = yellow.Render(fmt.Sprintf("change %d min, tight", real))
	case real != planned:
		text = faint.Render(fmt.Sprintf("change %d min instead of %d", real, planned))
	default:
		text = faint.Render(fmt.Sprintf("change %d min", real))
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
	v.notes = addNote(v.notes, lateNote(int(delay/time.Minute)))
	return space + delayStyle(delay).Render(text)
}

func delayText(delay time.Duration) string {
	if late := int(delay / time.Minute); late > 0 {
		return fmt.Sprintf("+%d", late)
	}
	return ""
}

func (v *journeyView) clock(t time.Time) string {
	return t.In(v.o.Location).Format("15:04")
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
