package tui

import (
	"context"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/PieterLuypaert/trainspot/internal/board"
	"github.com/PieterLuypaert/trainspot/internal/irail"
)

// In a wide window the departure board shows the chosen train's route
// beside it. A route is fetched once the choice has rested for a moment,
// so running down the board with the arrow keys does not ask iRail about
// every train on the way, and routes already fetched are kept.

const settleDelay = 400 * time.Millisecond

type settleMsg struct{ key string }

type peekMsg struct {
	key   string
	route irail.Route
	err   error
}

// peeked is a route fetched for the pane beside the board.
type peeked struct {
	route irail.Route
	at    time.Time
	err   error
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
	width, _ := m.size()
	return width-4 >= twoPanesWidth
}

// settle waits for the choice to rest before its route is fetched.
func (m Model) settle() tea.Cmd {
	d, ok := m.chosenDeparture()
	if !ok || m.screen != departures || !m.wideBoard() {
		return nil
	}
	key, delay := departureKey(d), m.cfg.SettleAfter
	if delay == 0 {
		delay = settleDelay
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return settleMsg{key} })
}

// peek fetches the route of the chosen departure once the choice has
// rested on it, unless a fresh one is on hand.
func (m Model) peek(msg settleMsg) tea.Cmd {
	d, ok := m.chosenDeparture()
	if !ok || m.screen != departures || departureKey(d) != msg.key {
		return nil
	}
	if p, ok := m.peeks[msg.key]; ok && !m.stale(p.at) {
		return nil
	}
	source, station, lang, loc := m.cfg.Source, m.boardStation(), m.cfg.Lang, m.cfg.Location
	return func() tea.Msg {
		route, err := source.RouteThrough(context.Background(), d.Vehicle, station, d.Scheduled.In(loc), lang)
		return peekMsg{key: msg.key, route: route, err: err}
	}
}

func (m *Model) gotPeek(msg peekMsg) {
	m.peeks[msg.key] = peeked{route: msg.route, at: m.cfg.Now(), err: msg.err}
}

// peekLines are the chosen train's route for the pane beside the board, in
// at most rows lines.
func (m Model) peekLines(width, rows int) []string {
	d, ok := m.chosenDeparture()
	if !ok {
		return nil
	}
	lines := []string{"", indent + bold.Render(d.Train) + faint.Render(" to "+d.Destination)}
	p, ok := m.peeks[departureKey(d)]
	switch {
	case !ok:
		return append(lines, indent+faint.Render("Looking up its route…"))
	case p.err != nil:
		return append(lines, indent+alarm.Render("Could not get its route: "+p.err.Error()))
	}
	o := board.JourneyOptions{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location}
	stops, _ := board.RouteStops(p.route, o, board.Mark{Station: m.boardStation(), At: d.Scheduled})
	lines = append(append(lines, indent+board.RouteSummary(p.route), ""), stops...)
	return cut(lines, rows, "enter shows the whole run")
}
