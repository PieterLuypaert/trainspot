package tui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/PieterLuypaert/trainspot/internal/irail"
)

// The route screen shows one train's whole run, live, for a departure
// chosen on the board. The same routes let the stops screen follow a
// journey that has left iRail's list of journeys.

type routeMsg struct {
	id    int
	route irail.Route
	err   error
}

type followMsg struct {
	id      int
	journey irail.Journey
	updated time.Time
	err     error
}

func (m Model) toRoute(d irail.Departure) (tea.Model, tea.Cmd) {
	m.screen, m.waiting, m.err, m.note = route, 0, nil, ""
	if d.Vehicle != m.routeOf.Vehicle || !d.Scheduled.Equal(m.routeOf.Scheduled) {
		m.route, m.routeAt, m.scroll = irail.Route{}, time.Time{}, 0
	}
	m.routeOf = d
	if !m.stale(m.routeAt) {
		return m, nil
	}
	fetch := m.fetchRoute()
	return m, fetch
}

func (m Model) routeKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc", "left", "h", "backspace":
		return m.toDepartures()
	case "e", "/":
		return m.toPlanning()
	case "r":
		return m.fetchAgain()
	}
	return m.scrollKey(k), nil
}

// scrollKey moves through the stops or a route; Update keeps the result
// within the list.
func (m Model) scrollKey(k tea.KeyPressMsg) Model {
	switch k.String() {
	case "up", "k":
		m.scroll = max(0, m.scroll-1)
	case "down", "j":
		m.scroll++
	case "pgup":
		m.scroll = max(0, m.scroll-m.page())
	case "pgdown", "space":
		m.scroll += m.page()
	}
	return m
}

func (m *Model) fetchRoute() tea.Cmd {
	id, source, d, lang, loc := m.begin(), m.cfg.Source, m.routeOf, m.cfg.Lang, m.cfg.Location
	return func() tea.Msg {
		route, err := source.Route(context.Background(), d.Vehicle, d.Scheduled.In(loc), lang)
		return routeMsg{id: id, route: route, err: err}
	}
}

func (m *Model) gotRoute(msg routeMsg) {
	if msg.id != m.waiting {
		return
	}
	m.waiting, m.err = 0, msg.err
	if msg.err == nil {
		m.route, m.routeAt = msg.route, m.cfg.Now()
	}
}

// fetchFollow asks for the route of every train in the journey the stops
// screen keeps, to follow it now that the journey list has dropped it.
func (m *Model) fetchFollow() tea.Cmd {
	id, source, journey, lang, loc := m.begin(), m.cfg.Source, *m.left, m.cfg.Lang, m.cfg.Location
	return func() tea.Msg {
		followed := irail.Journey{Legs: slices.Clone(journey.Legs)}
		var updated time.Time
		for i, leg := range followed.Legs {
			if leg.Vehicle == "" {
				continue
			}
			r, err := source.Route(context.Background(), leg.Vehicle, leg.Departure.Scheduled.In(loc), lang)
			if err != nil {
				return followMsg{id: id, err: err}
			}
			followed.Legs[i], updated = leg.Follow(r), r.Updated
		}
		return followMsg{id: id, journey: followed, updated: updated}
	}
}

func (m *Model) gotFollow(msg followMsg) {
	if msg.id != m.waiting {
		return
	}
	m.waiting, m.err = 0, msg.err
	if msg.err == nil {
		m.left, m.leftAt = &msg.journey, msg.updated
	}
}

// keepBoardChoice finds the chosen departure on a refreshed board, by its
// train and time, so the choice does not jump when an earlier train has
// left.
func (m *Model) keepBoardChoice(next irail.Liveboard) {
	if m.boardChoice < len(m.board.Departures) {
		chosen := m.board.Departures[m.boardChoice]
		for i, d := range next.Departures {
			if d.Vehicle == chosen.Vehicle && d.Scheduled.Equal(chosen.Scheduled) {
				m.boardChoice = i
				return
			}
		}
	}
	m.boardChoice = max(0, min(m.boardChoice, len(next.Departures)-1))
}
