package tui

import (
	"context"
	"maps"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/PieterLuypaert/trainspot/internal/board"
	"github.com/PieterLuypaert/trainspot/internal/irail"
)

// In a wide window the departure board shows the chosen train's route
// beside it. A route is fetched once the choice has rested for a moment,
// so running down the board with the arrow keys does not ask iRail about
// every train on the way. The routes fetched are kept for the pane and the
// route screen alike, and the chosen one is fetched again with the board.

const settleDelay = 400 * time.Millisecond

// settleMsg says the choice may have rested on the departure key; again
// asks for its route even when one is on hand, as when the board itself
// was fetched again.
type settleMsg struct {
	key   string
	again bool
}

type peekMsg struct {
	key   string
	route irail.Route
	err   error
}

// knownRoute is a train's route as last fetched: the route, when it came,
// what went wrong with the last fetch, when that fetch was asked for, and
// whether the pane's fetch is on its way.
type knownRoute struct {
	route  irail.Route
	at     time.Time
	err    error
	tried  time.Time
	asking bool
}

func departureKey(d irail.Departure) string {
	return d.Vehicle + "@" + strconv.FormatInt(d.Scheduled.Unix(), 10)
}

// chosenDeparture is the departure chosen on the board, if any.
func (m Model) chosenDeparture() (irail.Departure, bool) {
	if m.boardChoice >= len(m.board.Departures) {
		return irail.Departure{}, false
	}
	return m.board.Departures[m.boardChoice], true
}

func (m Model) wideBoard() bool {
	width, _ := m.inside()
	return m.boardWidth(width) < width
}

// boardWidth is how wide the board is drawn in width. The chosen train's
// route stands beside it only when the board keeps every column and the
// route gets minRouteWidth, as the journeys do; the board then takes up to
// its full width and the route the rest. Otherwise the board takes all of
// it.
func (m Model) boardWidth(width int) int {
	full, least := board.Widths(m.board, board.Options{Now: m.cfg.Now(), Location: m.cfg.Location})
	room := width - lipgloss.Width(divider) - minRouteWidth
	if width < twoPanesWidth || least > room {
		return width
	}
	return min(full, room)
}

// settle waits for the choice to rest before its route is fetched.
func (m Model) settle(again bool) tea.Cmd {
	d, ok := m.chosenDeparture()
	if !ok || m.screen != departures || !m.wideBoard() {
		return nil
	}
	key, delay := departureKey(d), m.cfg.SettleAfter
	if delay == 0 {
		delay = settleDelay
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return settleMsg{key: key, again: again} })
}

// peek fetches the route of the chosen departure once the choice has
// rested on it, unless it is on its way or was asked for less than a
// minute ago and the board was not fetched again since.
func (m *Model) peek(msg settleMsg) tea.Cmd {
	d, ok := m.chosenDeparture()
	if !ok || m.screen != departures || !m.wideBoard() || departureKey(d) != msg.key {
		return nil
	}
	known, ok := m.routes[msg.key]
	if known.asking || ok && !msg.again && !m.stale(known.tried) {
		return nil
	}
	known.asking, known.tried = true, m.cfg.Now()
	m.routes[msg.key] = known
	ask := m.askRoute(d)
	return func() tea.Msg {
		route, err := ask()
		return peekMsg{key: msg.key, route: route, err: err}
	}
}

// askRoute fetches the run of departure d that stops at the board's
// station.
func (m Model) askRoute(d irail.Departure) func() (irail.Route, error) {
	source, station, lang, loc := m.cfg.Source, m.boardStation(), m.cfg.Lang, m.cfg.Location
	return func() (irail.Route, error) {
		return source.RouteThrough(context.Background(), d.Vehicle, station, d.Scheduled.In(loc), lang)
	}
}

func (m *Model) gotPeek(msg peekMsg) {
	m.keepRoute(msg.key, msg.route, msg.err)
}

// keepRoute takes the answer to a route fetch. A failed one keeps the
// route already on hand.
func (m *Model) keepRoute(key string, route irail.Route, err error) {
	known := m.routes[key]
	known.asking, known.err = false, err
	if err == nil {
		known.route, known.at = route, m.cfg.Now()
	}
	m.routes[key] = known
}

// forgetRoutes drops the routes of the trains that have left the board.
func (m *Model) forgetRoutes() {
	onBoard := map[string]bool{}
	for _, d := range m.board.Departures {
		onBoard[departureKey(d)] = true
	}
	maps.DeleteFunc(m.routes, func(key string, _ knownRoute) bool { return !onBoard[key] })
}

// peekLines are the chosen train's route for the pane beside the board, in
// at most rows lines, with the legend for what shows of it.
func (m Model) peekLines(width, rows int) []string {
	d, ok := m.chosenDeparture()
	if !ok {
		return nil
	}
	lines := []string{"", indent + bold.Render(d.Train) + faint.Render(" to "+d.Destination)}
	known := m.routes[departureKey(d)]
	switch {
	case len(known.route.Stops) == 0 && known.err != nil && !known.asking:
		return append(lines, wrap("Could not get its route: "+known.err.Error(), width, alarm)...)
	case known.at.IsZero():
		return append(lines, indent+faint.Render("Looking up its route…"))
	case len(known.route.Stops) < 2:
		return append(lines, indent+"iRail has no route for this train.")
	}
	o := board.JourneyOptions{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location}
	stops, legend := board.RouteStops(known.route, o, board.Mark{Station: m.boardStation(), At: d.Scheduled})
	var tail []string
	if known.err != nil {
		tail = wrap("could not refresh its route: "+known.err.Error(), width, alarm)
	}
	lines = append(append(lines, indent+board.RouteSummary(known.route), ""), stops...)
	lines = cut(lines, rows-len(legend.Lines(width))-len(tail)-1, "enter shows the whole run")
	return append(append(append(lines, ""), legend.In(lines).Lines(width)...), tail...)
}
