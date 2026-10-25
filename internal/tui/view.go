package tui

import (
	"fmt"
	"slices"
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

// inside is the room within the frame's borders.
func (m Model) inside() (width, room int) {
	width, height := m.size()
	return width - 4, height - 2
}

func (m Model) render() string {
	width, height := m.size()
	inner, room := m.inside()
	clock := faint.Render(m.cfg.Now().In(m.cfg.Location).Format("Mon 2 Jan 15:04"))
	switch m.screen {
	case journeys:
		return frame(m.tripTitle(), clock, m.journeysBody(inner, room), journeyKeys, width, height)
	case departures:
		return frame(m.departuresTitle(), clock, m.departuresBody(inner, room), departureKeys, width, height)
	case stops:
		main, tail, shown := m.scrollView()
		return frame(m.stopsTitle(), clock, scrollBody(main, tail, shown, m.scroll), stopKeys, width, height)
	case route:
		main, tail, shown := m.scrollView()
		return frame(m.routeTitle(), clock, scrollBody(main, tail, shown, m.scroll), routeKeys, width, height)
	}
	return frame(bold.Render("Plan a trip"), clock, m.planningBody(inner, room), m.planningKeys(), width, height)
}

var (
	journeyKeys = []key{{name: "↑↓", does: "choose"}, {name: "enter", does: "stops"}, {name: "d", does: "departures"},
		{name: "e", does: "edit"}, {name: "s", does: "swap"}, {name: "r", does: "refresh", optional: true}, {name: "q", does: "quit"}}
	departureKeys = []key{{name: "↑↓", does: "choose"}, {name: "enter", does: "route"}, {name: "esc", does: "journeys"},
		{name: "e", does: "edit", optional: true}, {name: "r", does: "refresh", optional: true}, {name: "q", does: "quit"}}
	stopKeys  = []key{{name: "↑↓", does: "scroll"}, {name: "esc", does: "journeys"}, {name: "r", does: "refresh", optional: true}, {name: "q", does: "quit"}}
	routeKeys = []key{{name: "↑↓", does: "scroll"}, {name: "esc", does: "departures"}, {name: "r", does: "refresh", optional: true}, {name: "q", does: "quit"}}
)

func (m Model) planningKeys() []key {
	back := key{name: "esc", does: "quit"}
	if m.hasTrip() {
		back = key{name: "esc", does: "back"}
	}
	keys := []key{{name: "tab", does: "next field"}, {name: "↑↓", does: "choose"}, {name: "enter", does: "search"}}
	if m.picksByNumber() {
		numbers := "1"
		if n := len(m.picks); n > 1 {
			numbers = fmt.Sprintf("1-%d", n)
		}
		keys = append(keys, key{name: numbers, does: "recent trip", optional: true})
	}
	return append(keys, back)
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

// planningBody puts the form with the recent trips under it on the left
// and, beside it in a wide window or under it in a narrow one, the next
// departures from the station in the From field. The departures take the
// larger part, so the board keeps its columns.
func (m Model) planningBody(width, room int) []string {
	left := formWidth(width)
	form := append(m.formLines(left), m.recentLines(left)...)
	if left == width {
		return append(form, m.planningSide(width, room-len(form))...)
	}
	return sideBySide(form, m.planningSide(width-left-lipgloss.Width(divider), room), left)
}

// formWidth is how wide the form is: all of width in a narrow window, two
// fifths of it beside the departures in a wide one.
func formWidth(width int) int {
	if width < twoPanesWidth {
		return width
	}
	return width * 2 / 5
}

// narrowSideRows are the rows the departures under a narrow form keep
// while it suggests stations: enough for the title, the column titles,
// two departures, the legend and the status line.
const narrowSideRows = 9

func (m Model) formLines(width int) []string {
	body := []string{""}
	for i, f := range m.fields {
		body = append(body, m.fieldLine(i, f))
		if i == m.focus {
			body = append(body, m.suggestionLines(width)...)
		}
	}
	switch {
	case m.note != "":
		body = append(append(body, ""), wrap(m.note, width, alarm)...)
	case m.fields[m.focus].text == "":
		body = append(append(body, ""), wrap("Type a station's name in any language, such as gent, liège or brussels airport.", width, faint)...)
	}
	return body
}

// wrap breaks text into indented lines in style that fit width.
func wrap(text string, width int, style lipgloss.Style) []string {
	var lines []string
	for _, line := range strings.Split(ansi.Wordwrap(text, max(1, width-len(indent)), ""), "\n") {
		lines = append(lines, indent+style.Render(line))
	}
	return lines
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
	shown := list[:m.shownSuggestions()]
	// The local names and "abroad" line up after the names that leave them
	// room; a longer name goes without them rather than show a stub.
	noteRoom := width - len(pad) - 2 - 2 - minNotesWidth
	nameWidth := 0
	for _, s := range shown {
		if w := lipgloss.Width(s.Name(m.cfg.Lang)); w <= noteRoom {
			nameWidth = max(nameWidth, w)
		}
	}
	for i, s := range shown {
		lines = append(lines, ansi.Truncate(pad+m.suggestion(s, i == m.choice, nameWidth), width, "…"))
	}
	if more := len(list) - len(shown); more > 0 {
		lines = append(lines, pad+faint.Render(fmt.Sprintf("… %d more; type more of the name", more)))
	}
	return lines
}

// shownSuggestions is how many suggestions the form shows: all that fit,
// and at least minSuggestions rows. The recent trips make way while the
// form suggests, and the departures under a narrow form keep
// narrowSideRows.
func (m Model) shownSuggestions() int {
	width, room := m.inside()
	// The blank line over the fields and the two fields take three rows,
	// and a note under them the rows it wraps to after a blank line.
	space := room - 3
	if m.note != "" {
		space -= 1 + len(wrap(m.note, formWidth(width), alarm))
	}
	if formWidth(width) == width {
		space -= narrowSideRows
	}
	if len(m.suggested) <= space {
		return len(m.suggested)
	}
	// One of the rows says how many are left out.
	return min(len(m.suggested), max(minSuggestions, space)-1)
}

// minNotesWidth is the least room a suggestion's notes get.
const minNotesWidth = 8

// suggestion is a station to pick, with its notes after the name column
// nameWidth when its name fits that column.
func (m Model) suggestion(s stations.Station, chosen bool, nameWidth int) string {
	name := s.Name(m.cfg.Lang)
	withNotes := lipgloss.Width(name) <= nameWidth
	name += strings.Repeat(" ", max(0, nameWidth-lipgloss.Width(name)))
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
	if len(notes) == 0 || !withNotes {
		return strings.TrimRight(marker+name, " ")
	}
	return marker + name + "  " + faint.Render(strings.Join(notes, " · "))
}

// journeysBody lays the journeys out: the list, with the chosen journey's
// route line beside it in a wide window or under it in a narrow one, and
// the legend and status line below both.
func (m Model) journeysBody(width, room int) []string {
	switch list := m.trip.Journeys; {
	case len(list) == 0 && m.loading():
		return []string{"", indent + faint.Render("Looking up journeys…")}
	case len(list) == 0 && m.err != nil:
		return []string{"", indent + alarm.Render("Could not get the journeys: "+m.err.Error()), indent + faint.Render("Press r to try again.")}
	case len(list) == 0:
		return []string{"", indent + "No journeys found."}
	}
	o := board.JourneyOptions{Width: width, Now: m.cfg.Now(), Updated: m.trip.Updated, Location: m.cfg.Location}
	summaries, listLegend := board.JourneySummaries(m.trip.Journeys, o)
	natural := 0
	for _, s := range summaries {
		natural = max(natural, lipgloss.Width(s))
	}
	// The list keeps its full width, alarms and days included: the route
	// line beside it gives way, and below minRouteWidth the panes stack.
	if left := natural + len(indent); width >= twoPanesWidth && left+lipgloss.Width(divider)+minRouteWidth <= width {
		return m.journeyPanes(o, room, summaries, listLegend, left)
	}
	return m.journeyStack(o, room, summaries, listLegend)
}

const (
	seeAllStops = "enter shows all stops"
	// minRouteWidth is the narrowest the route line beside the list gets.
	minRouteWidth = 50
)

func (m Model) journeyPanes(o board.JourneyOptions, room int, summaries []string, listLegend board.Legend, left int) []string {
	right := o
	right.Width = o.Width - left - lipgloss.Width(divider)
	stops, stopsLegend := board.JourneyStops(m.trip.Journeys[m.selected], right)
	legend := listLegend.With(stopsLegend)
	rows := room - len(m.journeyTail(legend.Lines(o.Width)))
	list := chosenWindow(m.markedSummaries(summaries), m.selected, rows)
	route := cut(append([]string{""}, stops...), rows, seeAllStops)
	shown := legend.In(append(slices.Clone(list), route...))
	return append(sideBySide(list, route, left), m.journeyTail(shown.Lines(o.Width))...)
}

// journeyStack puts the chosen journey's route line under the list, set
// apart by a rule; the list takes at most a third of the room.
func (m Model) journeyStack(o board.JourneyOptions, room int, summaries []string, listLegend board.Legend) []string {
	stops, stopsLegend := board.JourneyStops(m.trip.Journeys[m.selected], o)
	legend := listLegend.With(stopsLegend)
	rows := room - len(m.journeyTail(legend.Lines(o.Width)))
	list := chosenWindow(m.markedSummaries(summaries), m.selected, min(len(summaries)+1, max(4, rows/3)))
	rule := indent + faint.Render(strings.Repeat("─", max(0, o.Width-2*len(indent))))
	route := cut(stops, rows-len(list)-1, seeAllStops)
	body := append(append(list, rule), route...)
	return append(body, m.journeyTail(legend.In(body).Lines(o.Width))...)
}

// markedSummaries puts the marker in front of the chosen journey.
func (m Model) markedSummaries(summaries []string) []string {
	lines := make([]string, len(summaries))
	for i, s := range summaries {
		marker := indent
		if i == m.selected {
			marker = bold.Render("▸ ")
		}
		lines[i] = marker + s
	}
	return lines
}

func (m Model) journeyTail(legend []string) []string {
	return m.tail(legend, board.Footer(m.trip.Updated, m.cfg.Location))
}

// tail is the legend and status line under a screen's body.
func (m Model) tail(legend []string, source string) []string {
	return append(append([]string{""}, legend...), m.status(source))
}

// departuresBody shows the board with the chosen departure marked, the
// rows around it when they do not all fit, and the legend and status line.
func (m Model) departuresBody(width, room int) []string {
	switch {
	case m.board.Updated.IsZero() && m.loading():
		return []string{"", indent + faint.Render("Looking up departures…")}
	case m.board.Updated.IsZero() && m.err != nil:
		return []string{"", indent + alarm.Render("Could not get the departures: "+m.err.Error()), indent + faint.Render("Press r to try again.")}
	}
	left := m.boardWidth(width)
	head, rows, tail, shown := m.departuresView(width, room)
	if left == width {
		return append(head, scrollBody(rows, tail, shown, m.boardScroll)...)
	}
	// In a wide window the chosen train's route stands beside the board.
	departures := append(head, scrollBody(rows, nil, shown, m.boardScroll)...)
	route := m.peekLines(width-left-lipgloss.Width(divider), room-len(tail))
	return append(sideBySide(departures, route, left), tail...)
}

// departuresView lays the board out for the window: the column titles,
// the rows with the chosen one marked, the legend and status line, and how
// many rows fit. The rows take the board's width; the legend and status
// line run under the route beside it too.
func (m Model) departuresView(width, room int) (head, rows, tail []string, shown int) {
	lines, legend := board.DepartureLines(m.board, board.Options{Width: m.boardWidth(width), Now: m.cfg.Now(), Location: m.cfg.Location})
	head, rows = []string{"", lines[0]}, slices.Clone(lines[1:])
	if m.boardChoice < len(rows) {
		rows[m.boardChoice] = bold.Render("▸ ") + strings.TrimPrefix(rows[m.boardChoice], indent)
	}
	footer := board.Footer(m.board.Updated, m.cfg.Location)
	shown = len(rows)
	if space := room - len(head) - len(m.tail(legend.Lines(width), footer)); len(rows) > space {
		shown = max(0, space-1)
	}
	// The rows that fit are counted beside the legend of the whole board,
	// so the board keeps its height as it scrolls; the legend keeps only
	// the notes for the rows in view, which never take more lines.
	first := max(0, min(m.boardScroll, len(rows)-shown))
	return head, rows, m.tail(legend.In(rows[first:first+shown]).Lines(width), footer), shown
}

// showBoardChoice scrolls the board just enough to keep the chosen
// departure in view.
func (m *Model) showBoardChoice() {
	m.boardChoice = max(0, min(m.boardChoice, len(m.board.Departures)-1))
	width, room := m.inside()
	_, rows, _, shown := m.departuresView(width, room)
	if m.boardChoice < m.boardScroll {
		m.boardScroll = m.boardChoice
	}
	if shown > 0 && m.boardChoice >= m.boardScroll+shown {
		m.boardScroll = m.boardChoice - shown + 1
	}
	m.boardScroll = max(0, min(m.boardScroll, len(rows)-shown))
}

// moreLine says how many lines lie above and below what shows.
func moreLine(above, below int) string {
	var where []string
	if above > 0 {
		where = append(where, fmt.Sprintf("↑ %d more", above))
	}
	if below > 0 {
		where = append(where, fmt.Sprintf("↓ %d more", below))
	}
	return indent + faint.Render(strings.Join(where, "  "))
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
	journey := m.shownJourney()
	lines, legend := board.JourneyStops(journey, board.JourneyOptions{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location})
	source := board.Footer(m.trip.Updated, m.cfg.Location)
	switch {
	case m.left != nil && arrived(*m.left):
		source = indent + faint.Render("This journey has arrived.")
	case m.left != nil && m.leftAt.IsZero():
		source = indent + faint.Render("Its train has left iRail's list of journeys, so trainspot follows the train itself.")
	case m.left != nil:
		source = indent + faint.Render("Following the train live, updated "+m.leftAt.In(m.cfg.Location).Format("15:04"))
	}
	tail = m.tail(legend.Lines(width), source)
	return append([]string{"", indent + board.JourneySummary(journey), ""}, lines...), tail
}

// routeTitle names the train, where it heads, and when it leaves the
// station of the board.
func (m Model) routeTitle() string {
	d := m.routeOf
	return bold.Render(d.Train) + faint.Render(" to "+d.Destination) + faint.Render(" · from ") +
		m.from.Name(m.cfg.Lang) + " " + bold.Render(d.Scheduled.In(m.cfg.Location).Format("15:04"))
}

// routeBody returns every stop of the chosen train's run, and the legend
// and status line that must stay in view.
func (m Model) routeBody(width int) (main, tail []string) {
	r := m.routes[departureKey(m.routeOf)].route
	switch {
	case len(r.Stops) == 0 && m.loading():
		return []string{"", indent + faint.Render("Looking up the train's route…")}, nil
	case len(r.Stops) == 0 && m.err != nil:
		return []string{"", indent + alarm.Render("Could not get the train's route: "+m.err.Error()), indent + faint.Render("Press r to try again.")}, nil
	case len(r.Stops) < 2:
		return []string{"", indent + "iRail has no route for this train."}, nil
	}
	mark := board.Mark{Station: m.boardStation(), At: m.routeOf.Scheduled}
	lines, legend := board.RouteStops(r, board.JourneyOptions{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location}, mark)
	tail = m.tail(legend.Lines(width), board.Footer(r.Updated, m.cfg.Location))
	return append([]string{"", indent + board.RouteSummary(r), ""}, lines...), tail
}

// boardStation is the name of the station whose board is on hand, as iRail
// names it in the routes.
func (m Model) boardStation() string {
	if m.board.Station != "" {
		return m.board.Station
	}
	return m.from.Name(m.cfg.Lang)
}

// scrollView lays the stops or the route out once for the window: the
// lines, the part that stays in view, and how many lines show above that
// part.
func (m Model) scrollView() (main, tail []string, shown int) {
	width, room := m.inside()
	if m.screen == route {
		main, tail = m.routeBody(width)
	} else {
		main, tail = m.stopsBody(width)
	}
	if space := room - len(tail); len(main) > space {
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
	view := append(append([]string{}, main[scroll:scroll+shown]...), moreLine(scroll, len(main)-scroll-shown))
	return append(view, tail...)
}

// maxScroll is how far the stops can scroll in the current window.
func (m Model) maxScroll() int {
	main, _, shown := m.scrollView()
	return max(0, len(main)-shown)
}

// page is how many lines a page up or down moves: as many as show.
func (m Model) page() int {
	_, _, shown := m.scrollView()
	return max(1, shown)
}
