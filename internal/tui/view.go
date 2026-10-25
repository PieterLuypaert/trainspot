package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PieterLuypaert/trainspot/internal/board"
	"github.com/PieterLuypaert/trainspot/internal/stations"
)

var (
	bold   = lipgloss.NewStyle().Bold(true)
	faint  = lipgloss.NewStyle().Faint(true)
	alarm  = lipgloss.NewStyle().Foreground(lipgloss.BrightRed).Bold(true)
	border = faint
)

const (
	indent = "  "
	// Before the first WindowSizeMsg the screen assumes a classic terminal.
	defaultWidth, defaultHeight = 80, 24
	labelWidth                  = 6
	// Below this the frame cannot be drawn and simply runs off the screen.
	minWidth, minHeight = 24, 6
)

// key is a key and what it does, for the hints on the frame's bottom edge.
type key struct{ name, does string }

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "trainspot"
	return v
}

func (m Model) render() string {
	width, height := m.width, m.height
	if width == 0 || height == 0 {
		width, height = defaultWidth, defaultHeight
	}
	width, height = max(width, minWidth), max(height, minHeight)
	inner := width - 4
	clock := faint.Render(m.cfg.Now().In(m.cfg.Location).Format("Mon 2 Jan 15:04"))
	switch m.screen {
	case journeys:
		return frame(m.tripTitle(), clock, m.journeysBody(inner), journeyKeys, width, height)
	case departures:
		return frame(m.departuresTitle(), clock, m.departuresBody(inner), departureKeys, width, height)
	}
	return frame(bold.Render("Plan a trip"), clock, m.planningBody(inner), m.planningKeys(), width, height)
}

var (
	journeyKeys   = []key{{"↑↓", "choose"}, {"d", "departures"}, {"e", "edit"}, {"s", "swap"}, {"r", "refresh"}, {"q", "quit"}}
	departureKeys = []key{{"esc", "journeys"}, {"e", "edit"}, {"r", "refresh"}, {"q", "quit"}}
)

func (m Model) planningKeys() []key {
	back := key{"esc", "quit"}
	if m.hasTrip {
		back = key{"esc", "back"}
	}
	return []key{{"tab", "next field"}, {"↑↓", "choose"}, {"enter", "search"}, back}
}

func (m Model) tripTitle() string {
	return bold.Render(m.from.Name(m.cfg.Lang)) + " → " + bold.Render(m.to.Name(m.cfg.Lang))
}

func (m Model) departuresTitle() string {
	title := "Departures from " + bold.Render(m.from.Name(m.cfg.Lang))
	if local := m.from.LocalName(m.cfg.Lang); local != "" {
		title += " " + faint.Render("("+local+")")
	}
	return title
}

func (m Model) planningBody(width int) []string {
	body := []string{""}
	for i, f := range m.fields {
		body = append(body, m.fieldLine(i, f))
		if i == m.focus {
			body = append(body, m.suggestionLines(width)...)
		}
	}
	switch {
	case m.note != "":
		body = append(body, "", indent+alarm.Render(m.note))
	case m.fields[m.focus].text == "":
		body = append(body, "", indent+faint.Render("Type a station's name in any language, such as gent, liège or brussels airport."))
	}
	return body
}

func (m Model) fieldLine(i int, f field) string {
	label := faint.Render(fmt.Sprintf("%-*s", labelWidth, f.label))
	if i == m.focus {
		label = bold.Render(fmt.Sprintf("%-*s", labelWidth, f.label))
	}
	switch {
	case f.station != nil:
		value := bold.Render(f.text)
		if local := f.station.LocalName(m.cfg.Lang); local != "" {
			value += "  " + faint.Render(local)
		}
		return indent + label + value
	case i == m.focus:
		return indent + label + f.text + "▌"
	case f.text == "":
		return indent + label + faint.Render("station name")
	}
	return indent + label + f.text
}

func (m Model) suggestionLines(width int) []string {
	list, typos := m.suggestions()
	pad := strings.Repeat(" ", len(indent)+labelWidth)
	var lines []string
	if typos {
		query := strings.TrimSpace(m.fields[m.focus].text)
		if len(list) == 0 {
			return []string{pad + faint.Render(fmt.Sprintf("no station is called %q", query))}
		}
		lines = append(lines, pad+faint.Render(fmt.Sprintf("no station is called %q; did you mean:", query)))
	}
	nameWidth := 0
	for _, s := range list {
		nameWidth = max(nameWidth, lipgloss.Width(s.Name(m.cfg.Lang)))
	}
	for i, s := range list {
		lines = append(lines, ansi.Truncate(pad+m.suggestion(s, i == m.choice, nameWidth), width, "…"))
	}
	return lines
}

func (m Model) suggestion(s stations.Station, chosen bool, nameWidth int) string {
	name := s.Name(m.cfg.Lang)
	name += strings.Repeat(" ", nameWidth-lipgloss.Width(name))
	marker := "  "
	if chosen {
		marker, name = bold.Render("▸ "), bold.Render(name)
	}
	var notes []string
	if local := s.LocalName(m.cfg.Lang); local != "" {
		notes = append(notes, local)
	}
	if !s.Belgian() {
		notes = append(notes, "abroad")
	}
	if len(notes) == 0 {
		return strings.TrimRight(marker+name, " ")
	}
	return marker + name + "  " + faint.Render(strings.Join(notes, " · "))
}

func (m Model) journeysBody(width int) []string {
	list := m.trip.Journeys
	switch {
	case len(list) == 0 && m.loading:
		return []string{"", indent + faint.Render("Looking up journeys…")}
	case len(list) == 0 && m.err != nil:
		return []string{"", indent + alarm.Render("Could not get the journeys: "+m.err.Error()), indent + faint.Render("Press r to try again.")}
	case len(list) == 0:
		return []string{"", indent + "No journeys found."}
	}
	lines := board.LayOutJourneys(list, board.JourneyOptions{Width: width, Now: m.cfg.Now(), Updated: m.trip.Updated, Location: m.cfg.Location})
	body := []string{""}
	for i, summary := range lines.Summaries {
		marker := indent
		if i == m.selected {
			marker = bold.Render("▸ ")
		}
		body = append(body, marker+summary)
	}
	// The rule sets the selected journey's stops apart from the list.
	rule := indent + faint.Render(strings.Repeat("─", max(0, width-2*len(indent))))
	body = append(append(body, rule), lines.Stops[m.selected]...)
	body = append(append(body, ""), lines.Legend...)
	return append(body, m.status(board.Footer(m.trip.Updated, m.cfg.Location)))
}

func (m Model) departuresBody(width int) []string {
	switch {
	case m.board.Updated.IsZero() && m.loading:
		return []string{"", indent + faint.Render("Looking up departures…")}
	case m.board.Updated.IsZero() && m.err != nil:
		return []string{"", indent + alarm.Render("Could not get the departures: "+m.err.Error()), indent + faint.Render("Press r to try again.")}
	}
	rows, legend := board.DepartureLines(m.board, board.Options{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location})
	body := append([]string{""}, rows...)
	body = append(append(body, ""), legend...)
	return append(body, m.status(board.Footer(m.board.Updated, m.cfg.Location)))
}

// status is the source line, or what is happening instead: a refresh on
// its way, or one that failed while the older data stays on screen.
func (m Model) status(source string) string {
	switch {
	case m.loading:
		return indent + faint.Render("updating…")
	case m.err != nil:
		return indent + alarm.Render("could not refresh: "+m.err.Error())
	case m.note != "":
		return indent + alarm.Render(m.note)
	}
	return source
}

// frame draws a rounded box over the whole terminal: the title and clock
// on its top edge, the body inside and the keys on its bottom edge.
func frame(title, clock string, body []string, keys []key, width, height int) string {
	inner := width - 4
	lines := []string{topEdge(title, clock, width)}
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(body) {
			line = ansi.Truncate(body[i], inner, "…")
		}
		lines = append(lines, border.Render("│ ")+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+border.Render(" │"))
	}
	return strings.Join(append(lines, bottomEdge(keys, width)), "\n")
}

// topEdge drops the clock, then shortens the title, when they do not fit.
func topEdge(title, clock string, width int) string {
	room := width - 8
	if lipgloss.Width(title)+1+lipgloss.Width(clock) > room {
		clock = ""
		title = ansi.Truncate(title, room, "…")
	}
	dashes := room - lipgloss.Width(title) - lipgloss.Width(clock)
	if clock == "" {
		return border.Render("╭─ ") + title + border.Render(" "+strings.Repeat("─", max(0, dashes+3))+"╮")
	}
	return border.Render("╭─ ") + title + border.Render(" "+strings.Repeat("─", max(0, dashes))+" ") + clock + border.Render(" ─╮")
}

func bottomEdge(keys []key, width int) string {
	var hints []string
	for _, k := range keys {
		hints = append(hints, bold.Render(k.name)+" "+faint.Render(k.does))
	}
	text := ansi.Truncate(strings.Join(hints, faint.Render(" · ")), width-6, "…")
	return border.Render("╰─ ") + text + border.Render(" "+strings.Repeat("─", max(0, width-6-lipgloss.Width(text)))+"─╯")
}
