// Package board draws a station's departures the way a station board does,
// fitted to the terminal's width.
package board

import (
	"fmt"
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
	out.WriteString(header(o) + "\n\n")
	if len(b.Departures) == 0 {
		out.WriteString(indent + "No departures are listed right now.\n")
	} else {
		out.WriteString(departures(b.Departures, o))
	}
	out.WriteString("\n")
	for _, line := range legend(b.Departures, o.Width) {
		out.WriteString(indent + line + "\n")
	}
	updated := b.Updated.In(o.Location).Format("15:04")
	out.WriteString(indent + faint.Render("Live NMBS data via iRail, updated "+updated) + "\n")
	return out.String()
}

// header gives up the clock, then the local name, before it shortens the
// station's name to fit the width.
func header(o Options) string {
	room := o.Width - 2*len(indent)
	title := "Departures from " + bold.Render(o.Station)
	full := title
	if o.LocalName != "" {
		full += " " + faint.Render("("+o.LocalName+")")
	}
	clock := faint.Render(o.Now.In(o.Location).Format("Mon 2 Jan 15:04"))
	switch space := room - lipgloss.Width(full) - lipgloss.Width(clock); {
	case space >= 2:
		return indent + full + strings.Repeat(" ", space) + clock
	case lipgloss.Width(full) <= room:
		return indent + full
	case lipgloss.Width(title) <= room:
		return indent + title
	}
	return indent + ansi.Truncate(title, room, "…")
}

// legend explains only the marks that are on the board, on one line when
// that fits the width.
func legend(list []irail.Departure, width int) []string {
	var parts []string
	for _, d := range list {
		if late := minutesLate(d); late > 0 && !d.Cancelled {
			parts = append(parts, faint.Render(fmt.Sprintf("+%d = %d min late", late, late)))
			break
		}
	}
	for _, d := range list {
		if d.PlatformChanged && !d.Cancelled && !d.Departed {
			parts = append(parts, changed.Render(d.Platform+" !")+faint.Render(" = platform changed"))
			break
		}
	}
	line := strings.Join(parts, faint.Render("  ·  "))
	if len(parts) < 2 || len(indent)+lipgloss.Width(line) <= width {
		return []string{line}
	}
	return parts
}

func minutesLate(d irail.Departure) int {
	return int(d.Delay / time.Minute)
}

func clock(t time.Time, o Options) string {
	return t.In(o.Location).Format("15:04")
}

type cell struct {
	text  string
	style lipgloss.Style
}

type column struct {
	title string
	cell  func(irail.Departure, Options) cell
}

var (
	timeColumn = column{"Time", func(d irail.Departure, o Options) cell {
		if d.Cancelled || d.Departed {
			return cell{clock(d.Scheduled, o), faint}
		}
		return cell{clock(d.Scheduled, o), bold}
	}}
	expectedColumn = column{"Expected", func(d irail.Departure, o Options) cell {
		late := minutesLate(d)
		switch {
		case d.Cancelled:
			return cell{"cancelled", alarm}
		case d.Departed:
			return cell{"departed", faint}
		case late == 0:
			return cell{"on time", green}
		case d.Delay < lateAfter:
			return cell{fmt.Sprintf("%s +%d", clock(d.Expected(), o), late), yellow}
		default:
			return cell{fmt.Sprintf("%s +%d", clock(d.Expected(), o), late), alarm}
		}
	}}
	destinationColumn = column{"To", func(d irail.Departure, o Options) cell {
		if d.Cancelled || d.Departed {
			return cell{d.Destination, faint}
		}
		return cell{d.Destination, plain}
	}}
	platformColumn = column{"Platform", func(d irail.Departure, o Options) cell {
		switch {
		case d.Platform == "":
			return cell{"–", faint}
		case d.Cancelled || d.Departed:
			return cell{d.Platform, faint}
		case d.PlatformChanged:
			return cell{d.Platform + " !", changed}
		default:
			return cell{d.Platform, bold}
		}
	}}
	trainColumn = column{"Train", func(d irail.Departure, o Options) cell {
		if d.Extra {
			return cell{d.Train + " extra", faint}
		}
		return cell{d.Train, faint}
	}}
	crowdColumn = column{"Crowd", func(d irail.Departure, o Options) cell {
		switch d.Occupancy {
		case "low":
			return cell{"low", faint}
		case "medium":
			return cell{"medium", plain}
		case "high":
			return cell{"high", bold}
		}
		return cell{"", plain}
	}}
)

// layouts go from the widest to the narrowest; the first that fits wins.
var layouts = [][]column{
	{timeColumn, expectedColumn, destinationColumn, platformColumn, trainColumn, crowdColumn},
	{timeColumn, expectedColumn, destinationColumn, platformColumn, trainColumn},
	{timeColumn, expectedColumn, destinationColumn, platformColumn},
}

func departures(list []irail.Departure, o Options) string {
	columns, widths := fit(list, o)
	var out strings.Builder
	out.WriteString(indent)
	for i, c := range columns {
		out.WriteString(pad(faint.Render(c.title), widths[i], i == len(columns)-1))
	}
	out.WriteString("\n")
	for _, d := range list {
		out.WriteString(indent)
		for i, c := range columns {
			cl := c.cell(d, o)
			text := ansi.Truncate(cl.text, widths[i], "…")
			out.WriteString(pad(cl.style.Render(text), widths[i], i == len(columns)-1))
		}
		out.WriteString("\n")
	}
	return out.String()
}

// fit picks the widest layout that fits o.Width. Optional columns go
// before destinations get shorter than roomyDestination.
func fit(list []irail.Departure, o Options) ([]column, []int) {
	for _, layout := range layouts[:len(layouts)-1] {
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
