// Package board draws a station's departures the way a station board does,
// and journeys between two stations, fitted to the terminal's width.
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

type Options struct {
	Station string
	// LocalName is the station's official name when Station is a
	// translation, such as Mons for Bergen.
	LocalName string
	Width     int
	Now       time.Time
	Location  *time.Location
}

const (
	indent = "  "
	gap    = "  "
	// Destinations are shortened to roomyDestination before a column goes,
	// and to minDestination when only the essential columns are left.
	roomyDestination = 20
	minDestination   = 12
	// destination is where the To column sits in every layout.
	destination = 2
	// lateAfter is the delay from which a train shows in red, not yellow.
	lateAfter = 5 * time.Minute
)

var (
	bold   = lipgloss.NewStyle().Bold(true)
	faint  = lipgloss.NewStyle().Faint(true)
	plain  = lipgloss.NewStyle()
	green  = lipgloss.NewStyle().Foreground(lipgloss.Green)
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	// alarm is bold bright red, which reads on light and dark themes alike.
	alarm = lipgloss.NewStyle().Foreground(lipgloss.BrightRed).Bold(true)
	// changed marks a platform like a station board does: black on yellow,
	// since yellow text vanishes on a light theme.
	changed = lipgloss.NewStyle().Foreground(lipgloss.Black).Background(lipgloss.BrightYellow)
)

func Render(b irail.Liveboard, o Options) string {
	var out strings.Builder
	out.WriteString(boardHeader(o) + "\n\n")
	if len(b.Departures) == 0 {
		out.WriteString(indent + "No departures are listed right now.\n")
	}
	rows, notes := departures(b.Departures, o)
	out.WriteString(rows + "\n")
	for _, line := range legend(notes, o.Width) {
		out.WriteString(indent + line + "\n")
	}
	out.WriteString(footer(b.Updated, o.Location) + "\n")
	return out.String()
}

// boardHeader gives up the clock, then the local name, before it shortens
// the station's name to fit the width.
func boardHeader(o Options) string {
	title := "Departures from " + bold.Render(o.Station)
	titles := []string{title}
	if o.LocalName != "" {
		titles = []string{title + " " + faint.Render("("+o.LocalName+")"), title}
	}
	return header(titles, o.Now.In(o.Location), o.Width)
}

// header shows the first title beside the clock when they fit together.
// Otherwise it shows the first title that fits alone, or shortens the last.
func header(titles []string, now time.Time, width int) string {
	room := width - 2*len(indent)
	clock := faint.Render(now.Format("Mon 2 Jan 15:04"))
	if space := room - lipgloss.Width(titles[0]) - lipgloss.Width(clock); space >= 2 {
		return indent + titles[0] + strings.Repeat(" ", space) + clock
	}
	for _, title := range titles {
		if lipgloss.Width(title) <= room {
			return indent + title
		}
	}
	return indent + ansi.Truncate(titles[len(titles)-1], room, "…")
}

// legend puts the notes for the marks on the board on one line when that
// fits the width.
func legend(notes []string, width int) []string {
	line := strings.Join(notes, faint.Render("  ·  "))
	if len(notes) > 1 && len(indent)+lipgloss.Width(line) > width {
		return notes
	}
	if line == "" {
		return nil
	}
	return []string{line}
}

// footer says where the data comes from and when iRail worked it out.
func footer(updated time.Time, loc *time.Location) string {
	return indent + faint.Render("Live NMBS data via iRail, updated "+clock(updated, loc))
}

// minutes counts whole minutes, which is how NMBS gives delays.
func minutes(d time.Duration) int {
	return int(d / time.Minute)
}

func clock(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("15:04")
}

type cell struct {
	text  string
	style lipgloss.Style
	// note explains the cell's mark in the legend, when it has one.
	note *note
}

// note is a legend entry. Each kind is explained once, in kind order.
type note struct {
	kind int
	text string
}

const (
	lateKind = iota
	platformKind
)

func lateNote(minutes int) *note {
	return &note{lateKind, faint.Render(fmt.Sprintf("+%d = %d min late", minutes, minutes))}
}

func platformNote(mark string) *note {
	return &note{platformKind, changed.Render(mark) + faint.Render(" = platform changed")}
}

// addNote keeps the first note of each kind.
func addNote(notes []note, n *note) []note {
	if n == nil || slices.ContainsFunc(notes, func(m note) bool { return m.kind == n.kind }) {
		return notes
	}
	return append(notes, *n)
}

func noteTexts(notes []note) []string {
	slices.SortFunc(notes, func(a, b note) int { return a.kind - b.kind })
	var texts []string
	for _, n := range notes {
		texts = append(texts, n.text)
	}
	return texts
}

// delayStyle is yellow for a few minutes late and red from lateAfter.
func delayStyle(delay time.Duration) lipgloss.Style {
	if delay < lateAfter {
		return yellow
	}
	return alarm
}

// platformCell marks a changed platform; faded is for trains that are gone
// or cancelled, whose platform no longer matters.
func platformCell(platform string, platformChanged, faded bool) cell {
	switch {
	case platform == "":
		return cell{text: "–", style: faint}
	case faded:
		return cell{text: platform, style: faint}
	case platformChanged:
		mark := platform + " !"
		return cell{text: mark, style: changed, note: platformNote(mark)}
	default:
		return cell{text: platform, style: bold}
	}
}

type column struct {
	title string
	// optional columns are left out when none of their cells say anything.
	optional bool
	cell     func(irail.Departure, Options) cell
}

var (
	timeColumn = column{title: "Time", cell: func(d irail.Departure, o Options) cell {
		if d.Cancelled || d.Departed {
			return cell{text: clock(d.Scheduled, o.Location), style: faint}
		}
		return cell{text: clock(d.Scheduled, o.Location), style: bold}
	}}
	expectedColumn = column{title: "Expected", cell: func(d irail.Departure, o Options) cell {
		switch {
		case d.Cancelled:
			return cell{text: "cancelled", style: alarm}
		case d.Departed:
			return cell{text: "departed", style: faint}
		case minutes(d.Delay) <= 0:
			return cell{text: "on time", style: green}
		}
		late := minutes(d.Delay)
		return cell{text: fmt.Sprintf("%s +%d", clock(d.Expected(), o.Location), late), style: delayStyle(d.Delay), note: lateNote(late)}
	}}
	destinationColumn = column{title: "To", cell: func(d irail.Departure, o Options) cell {
		if d.Cancelled || d.Departed {
			return cell{text: d.Destination, style: faint}
		}
		return cell{text: d.Destination, style: plain}
	}}
	platformColumn = column{title: "Platform", cell: func(d irail.Departure, o Options) cell {
		return platformCell(d.Platform, d.PlatformChanged, d.Cancelled || d.Departed)
	}}
	trainColumn = column{title: "Train", optional: true, cell: func(d irail.Departure, o Options) cell {
		if d.Extra {
			return cell{text: d.Train + " extra", style: faint}
		}
		return cell{text: d.Train, style: faint}
	}}
	crowdColumn = column{title: "Crowd", optional: true, cell: func(d irail.Departure, o Options) cell {
		switch d.Occupancy {
		case "low":
			return cell{text: "low", style: faint}
		case "medium":
			return cell{text: "medium", style: plain}
		case "high":
			return cell{text: "high", style: bold}
		}
		return cell{text: "", style: plain}
	}}
)

// layouts go from the widest to the narrowest; the first that fits wins.
var layouts = [][]column{
	{timeColumn, expectedColumn, destinationColumn, platformColumn, trainColumn, crowdColumn},
	{timeColumn, expectedColumn, destinationColumn, platformColumn, trainColumn},
	{timeColumn, expectedColumn, destinationColumn, platformColumn},
}

// departures draws the rows and returns the legend notes for the marks
// that ended up in them.
func departures(list []irail.Departure, o Options) (string, []string) {
	if len(list) == 0 {
		return "", nil
	}
	columns, widths := fit(list, o)
	var out strings.Builder
	out.WriteString(indent)
	for i, c := range columns {
		out.WriteString(pad(faint.Render(c.title), widths[i], i == len(columns)-1))
	}
	out.WriteString("\n")
	var notes []note
	for _, d := range list {
		out.WriteString(indent)
		for i, c := range columns {
			cl := c.cell(d, o)
			text := ansi.Truncate(cl.text, widths[i], "…")
			out.WriteString(pad(cl.style.Render(text), widths[i], i == len(columns)-1))
			notes = addNote(notes, cl.note)
		}
		out.WriteString("\n")
	}
	return out.String(), noteTexts(notes)
}

// fit picks the widest layout that fits o.Width. Optional columns go
// before destinations get shorter than roomyDestination.
func fit(list []irail.Departure, o Options) ([]column, []int) {
	for _, layout := range layouts[:len(layouts)-1] {
		layout = withoutEmpty(layout, list, o)
		widths := natural(layout, list, o)
		if shorten(widths, o.Width, roomyDestination) {
			return layout, widths
		}
	}
	last := layouts[len(layouts)-1]
	widths := natural(last, list, o)
	shorten(widths, o.Width, minDestination)
	return last, widths
}

func withoutEmpty(layout []column, list []irail.Departure, o Options) []column {
	return slices.DeleteFunc(slices.Clone(layout), func(c column) bool {
		return c.optional && !slices.ContainsFunc(list, func(d irail.Departure) bool { return c.cell(d, o).text != "" })
	})
}

// shorten narrows the To column until the row fits width, but not below
// shortest, and reports whether the row fits.
func shorten(widths []int, width, shortest int) bool {
	if over := total(widths) - width; over > 0 {
		widths[destination] = max(min(shortest, widths[destination]), widths[destination]-over)
	}
	return total(widths) <= width
}

func natural(columns []column, list []irail.Departure, o Options) []int {
	widths := make([]int, len(columns))
	for i, c := range columns {
		widths[i] = lipgloss.Width(c.title)
		for _, d := range list {
			widths[i] = max(widths[i], lipgloss.Width(c.cell(d, o).text))
		}
	}
	return widths
}

func total(widths []int) int {
	sum := len(indent) + len(gap)*(len(widths)-1)
	for _, w := range widths {
		sum += w
	}
	return sum
}

func pad(s string, width int, last bool) string {
	if last {
		return s
	}
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s))) + gap
}
