package tui

import (
	"context"
	"errors"
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
	key   string
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
	m.screen, m.waiting, m.err, m.note, m.scroll = route, 0, nil, "", 0
	m.routeOf = d
	if known := m.routes[departureKey(d)]; !m.stale(known.at) {
		// The route on hand is refreshed a minute after it came, not after
		// the board did.
		m.triedAt = known.at
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
	id, key, ask := m.begin(), departureKey(m.routeOf), m.askRoute(m.routeOf)
	known := m.routes[key]
	known.tried = m.cfg.Now()
	m.routes[key] = known
	return func() tea.Msg {
		route, err := ask()
		return routeMsg{id: id, key: key, route: route, err: err}
	}
}

// gotRoute keeps every route that comes, also one for a screen the user
// has left, since the pane beside the board shows it too.
func (m *Model) gotRoute(msg routeMsg) {
	m.keepRoute(msg.key, msg.route, msg.err)
	if msg.id == m.waiting {
		m.waiting, m.err = 0, msg.err
	}
}

// fetchFollow asks for the route of every train in the journey the stops
// screen keeps that has yet to arrive, to follow the journey now that the
// journey list has dropped it. One train iRail cannot give does not keep
// the others from being followed.
func (m *Model) fetchFollow() tea.Cmd {
	id, source, journey, lang, loc := m.begin(), m.cfg.Source, *m.left, m.cfg.Lang, m.cfg.Location
	return func() tea.Msg {
		msg := followMsg{id: id, journey: irail.Journey{Legs: slices.Clone(journey.Legs)}}
		var problem error
		followed := 0
		for i, leg := range msg.journey.Legs {
			if leg.Vehicle == "" || leg.Arrival.Passed {
				continue
			}
			r, err := source.RouteThrough(context.Background(), leg.Vehicle, leg.Departure.Station, leg.Departure.Scheduled.In(loc), lang)
			if err != nil {
				problem = err
				continue
			}
			if next, ok := leg.Follow(r); ok {
				msg.journey.Legs[i], msg.updated = next, r.Updated
				followed++
			}
		}
		if followed == 0 && problem == nil && !arrived(journey) {
			problem = errors.New("iRail's routes do not show this journey's trains")
		}
		if followed == 0 {
			msg.err = problem
		}
		return msg
	}
}

func (m *Model) gotFollow(msg followMsg) {
	if msg.id != m.waiting {
		return
	}
	m.waiting, m.err = 0, msg.err
	if msg.err == nil {
		m.left = &msg.journey
		if !msg.updated.IsZero() {
			m.leftAt = msg.updated
		}
	}
}

// arrived tells whether every train of the journey has reached where its
// leg ends.
func arrived(j irail.Journey) bool {
	return !slices.ContainsFunc(j.Legs, func(l irail.Leg) bool { return !l.Walking && !l.Arrival.Passed })
}

// keepBoardChoice finds the chosen departure on a refreshed board, by its
// train and time, so the choice does not jump when an earlier train has
// left.
func (m *Model) keepBoardChoice(next irail.Liveboard) {
	if chosen, ok := m.chosenDeparture(); ok {
		key := departureKey(chosen)
		if i := slices.IndexFunc(next.Departures, func(d irail.Departure) bool { return departureKey(d) == key }); i >= 0 {
			m.boardChoice = i
			return
		}
	}
	m.boardChoice = max(0, min(m.boardChoice, len(next.Departures)-1))
}
