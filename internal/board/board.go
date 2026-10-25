// Package board draws a station's departures the way a station board does,
// and journeys between two stations, fitted to the terminal's width.
package board

import (
	"fmt"
	"regexp"
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
	rows, legendLines := DepartureLines(b, o)
	lines := append([]string{boardHeader(o), ""}, rows...)
	lines = append(append(lines, ""), legendLines...)
	lines = append(lines, Footer(b.Updated, o.Location))
	return strings.Join(lines, "\n") + "\n"
}

// DepartureLines draws the board without its header and source line, for
// a caller that frames it: the rows under their column titles, or a note
// that nothing leaves, and the legend. Every line is indented.
func DepartureLines(b irail.Liveboard, o Options) (rows, legendLines []string) {
	if len(b.Departures) == 0 {
		return []string{indent + "No departures are listed right now."}, nil
	}
	rows, notes := departures(b.Departures, o)
	for _, line := range legend(notes, o.Width) {
		legendLines = append(legendLines, indent+line)
	}
	return rows, legendLines
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

// Footer says where the data comes from and when iRail worked it out.
func Footer(updated time.Time, loc *time.Location) string {
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

// marks lays out the delay column of a view and collects the notes its
// legend explains, for the journey list and the route line alike.
type marks struct {
	// delay is the width of the delay column.
	delay int
	notes []note
}

func (k *marks) fitDelay(delay time.Duration) {
	k.delay = max(k.delay, lipgloss.Width(delayText(delay)))
}

// lateMark is the coloured +N of a delay, or nothing on time, with its
// note for the legend.
func (k *marks) lateMark(delay time.Duration) string {
	text := delayText(delay)
	if text == "" {
		return ""
	}
	k.note(lateNote(minutes(delay)))
	return delayStyle(delay).Render(text)
}

// delayColumn pads a delay mark to the column, which takes no room at all
// when nothing is late.
func (k *marks) delayColumn(mark string) string {
	if k.delay == 0 {
		return ""
	}
	return " " + mark + strings.Repeat(" ", k.delay-lipgloss.Width(mark))
}

func (k *marks) note(n *note) {
	k.notes = addNote(k.notes, n)
}

// Legend explains the marks on what was drawn. The legends of parts drawn
// side by side join into one, each mark explained once.
type Legend struct{ notes []note }

func (l Legend) With(other Legend) Legend {
	notes := slices.Clone(l.notes)
	for _, n := range other.notes {
		notes = addNote(notes, &n)
	}
	return Legend{notes}
}

// In keeps the notes for the marks that show in lines, so the legend of a
// part cut short explains only what is on screen.
func (l Legend) In(lines []string) Legend {
	text := ansi.Strip(strings.Join(lines, "\n"))
	var kept []note
	for _, n := range l.notes {
		if markPattern[n.kind].MatchString(text) {
			kept = append(kept, n)
		}
	}
	return Legend{kept}
}

// markPattern finds the mark a note explains in drawn text.
var markPattern = map[int]*regexp.Regexp{
	lateKind:     regexp.MustCompile(`\+\d+`),
	platformKind: regexp.MustCompile(`\S+ !`),
}

// Lines lays the legend out for the width, each line indented.
func (l Legend) Lines(width int) []string {
	var lines []string
	for _, line := range legend(noteTexts(slices.Clone(l.notes)), width) {
		lines = append(lines, clip(indent+line, width))
	}
	return lines
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
func departures(list []irail.Departure, o Options) ([]string, []string) {
	columns, widths := fit(list, o)
	titles := indent
	for i, c := range columns {
		titles += pad(faint.Render(c.title), widths[i], i == len(columns)-1)
	}
	rows := []string{titles}
	var notes []note
	for _, d := range list {
		row := indent
		for i, c := range columns {
			cl := c.cell(d, o)
			text := ansi.Truncate(cl.text, widths[i], "…")
			row += pad(cl.style.Render(text), widths[i], i == len(columns)-1)
			notes = addNote(notes, cl.note)
		}
		rows = append(rows, row)
	}
	return rows, noteTexts(notes)
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
