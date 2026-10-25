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
// Optional keys make way first on a narrow terminal.
type key struct {
	name, does string
	optional   bool
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "trainspot"
	return v
}

func (m Model) size() (width, height int) {
	width, height = m.width, m.height
	if width == 0 || height == 0 {
		width, height = defaultWidth, defaultHeight
	}
	return max(width, minWidth), max(height, minHeight)
}

func (m Model) render() string {
	width, height := m.size()
	inner, room := width-4, height-2
	clock := faint.Render(m.cfg.Now().In(m.cfg.Location).Format("Mon 2 Jan 15:04"))
	switch m.screen {
	case journeys:
		main, tail := m.journeysBody(inner)
		return frame(m.tripTitle(), clock, fitBody(main, tail, room), journeyKeys, width, height)
	case departures:
		main, tail := m.departuresBody(inner)
		return frame(m.departuresTitle(), clock, fitBody(main, tail, room), departureKeys, width, height)
	case stops:
		main, tail, shown := m.stopsView()
		return frame(m.stopsTitle(), clock, scrollBody(main, tail, shown, m.scroll), stopKeys, width, height)
	}
	return frame(bold.Render("Plan a trip"), clock, m.planningBody(inner), m.planningKeys(), width, height)
}

// fitBody keeps the tail, the legend and status line, in view and cuts
// the main part to the room left, saying how much it left out.
func fitBody(main, tail []string, room int) []string {
	if len(main)+len(tail) <= room {
		return append(main, tail...)
	}
	keep := max(0, room-len(tail)-1)
	note := indent + faint.Render(fmt.Sprintf("… %d more lines; a taller window shows them", len(main)-keep))
	return append(append(main[:keep:keep], note), tail...)
}

var (
	journeyKeys = []key{{name: "↑↓", does: "choose"}, {name: "enter", does: "stops"}, {name: "d", does: "departures"},
		{name: "e", does: "edit"}, {name: "s", does: "swap"}, {name: "r", does: "refresh", optional: true}, {name: "q", does: "quit"}}
	departureKeys = []key{{name: "esc", does: "journeys"}, {name: "e", does: "edit"}, {name: "r", does: "refresh", optional: true}, {name: "q", does: "quit"}}
	stopKeys      = []key{{name: "↑↓", does: "scroll"}, {name: "esc", does: "journeys"}, {name: "r", does: "refresh", optional: true}, {name: "q", does: "quit"}}
)

func (m Model) planningKeys() []key {
	back := key{name: "esc", does: "quit"}
	if m.hasTrip() {
		back = key{name: "esc", does: "back"}
	}
	return []key{{name: "tab", does: "next field"}, {name: "↑↓", does: "choose"}, {name: "enter", does: "search"}, back}
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
	list, typos := m.suggested, m.typos
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

// journeysBody returns the list with the chosen journey's stops, and the
// legend and status line that must stay in view.
func (m Model) journeysBody(width int) (main, tail []string) {
	list := m.trip.Journeys
	switch {
	case len(list) == 0 && m.loading():
		return []string{"", indent + faint.Render("Looking up journeys…")}, nil
	case len(list) == 0 && m.err != nil:
		return []string{"", indent + alarm.Render("Could not get the journeys: "+m.err.Error()), indent + faint.Render("Press r to try again.")}, nil
	case len(list) == 0:
		return []string{"", indent + "No journeys found."}, nil
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
	tail = append(append([]string{""}, lines.Legends[m.selected]...), m.status(board.Footer(m.trip.Updated, m.cfg.Location)))
	return body, tail
}

func (m Model) departuresBody(width int) (main, tail []string) {
	switch {
	case m.board.Updated.IsZero() && m.loading():
		return []string{"", indent + faint.Render("Looking up departures…")}, nil
	case m.board.Updated.IsZero() && m.err != nil:
		return []string{"", indent + alarm.Render("Could not get the departures: "+m.err.Error()), indent + faint.Render("Press r to try again.")}, nil
	}
	rows, legend := board.DepartureLines(m.board, board.Options{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location})
	tail = append(append([]string{""}, legend...), m.status(board.Footer(m.board.Updated, m.cfg.Location)))
	return append([]string{""}, rows...), tail
}

// status is the source line, or what is happening instead: a refresh on
// its way, or one that failed while the older data stays on screen.
func (m Model) status(source string) string {
	switch {
	case m.loading():
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
	text := hintText(keys)
	for i := len(keys) - 1; i >= 0 && lipgloss.Width(text) > width-6; i-- {
		if keys[i].optional {
			keys = append(keys[:i:i], keys[i+1:]...)
			text = hintText(keys)
		}
	}
	text = ansi.Truncate(text, width-6, "…")
	return border.Render("╰─ ") + text + border.Render(" "+strings.Repeat("─", max(0, width-6-lipgloss.Width(text)))+"─╯")
}

func hintText(keys []key) string {
	var hints []string
	for _, k := range keys {
		hints = append(hints, bold.Render(k.name)+" "+faint.Render(k.does))
	}
	return strings.Join(hints, faint.Render(" · "))
}

// stopsTitle gives the journey's times, its day when it does not leave
// today and whether it is cancelled, as its summary line does.
func (m Model) stopsTitle() string {
	j, loc := m.shownJourney(), m.cfg.Location
	leaves := j.Departure().Scheduled.In(loc)
	title := bold.Render(leaves.Format("15:04")) + " → " + bold.Render(j.Arrival().Scheduled.In(loc).Format("15:04"))
	if leaves.Format("2006-01-02") != m.cfg.Now().In(loc).Format("2006-01-02") {
		title = faint.Render(leaves.Format("Mon 2 Jan")) + " " + title
	}
	if j.Cancelled() {
		title += " " + alarm.Render("cancelled")
	}
	return title + faint.Render(" · ") + m.tripTitle()
}

// stopsBody returns every stop of the journey on screen, and the legend
// and status line that must stay in view.
func (m Model) stopsBody(width int) (main, tail []string) {
	lines, legend := board.JourneyStops(m.shownJourney(), board.JourneyOptions{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location})
	source := board.Footer(m.trip.Updated, m.cfg.Location)
	if m.left != nil {
		source = indent + faint.Render("Its train has left, so iRail no longer lists this journey; these stops stay as they were.")
	}
	tail = append(append([]string{""}, legend...), m.status(source))
	return append([]string{""}, lines...), tail
}

// stopsView lays the stops screen out once for the window: the lines, the
// part that stays in view, and how many lines show above that part.
func (m Model) stopsView() (main, tail []string, shown int) {
	width, height := m.size()
	main, tail = m.stopsBody(width - 4)
	if space := height - 2 - len(tail); len(main) > space {
		return main, tail, max(0, space-1)
	}
	return main, tail, len(main)
}

// scrollBody shows the shown lines of main that scroll points at, keeping
// the tail in view, with a line that says how much lies above and below.
func scrollBody(main, tail []string, shown, scroll int) []string {
	if shown >= len(main) {
		return append(main, tail...)
	}
	scroll = max(0, min(scroll, len(main)-shown))
	view := append([]string{}, main[scroll:scroll+shown]...)
	var where []string
	if scroll > 0 {
		where = append(where, fmt.Sprintf("↑ %d more", scroll))
	}
	if below := len(main) - scroll - shown; below > 0 {
		where = append(where, fmt.Sprintf("↓ %d more", below))
	}
	view = append(view, indent+faint.Render(strings.Join(where, "  ")))
	return append(view, tail...)
}

// maxScroll is how far the stops can scroll in the current window.
func (m Model) maxScroll() int {
	main, _, shown := m.stopsView()
	return max(0, len(main)-shown)
}

// page is how many lines a page up or down moves: as many as show.
func (m Model) page() int {
	_, _, shown := m.stopsView()
	return max(1, shown)
}
