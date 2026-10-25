// Package tui is the full-screen trip planner that plain trainspot opens:
// a form that suggests stations as you type, the journeys between them and
// the departures from where the trip starts, all kept up to date.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/PieterLuypaert/trainspot/internal/irail"
	"github.com/PieterLuypaert/trainspot/internal/stations"
)

// Source is where the screen gets its journeys, departures and routes.
type Source interface {
	Journeys(ctx context.Context, fromID, toID, lang string) (irail.Trip, error)
	Liveboard(ctx context.Context, stationID, lang string) (irail.Liveboard, error)
	RouteThrough(ctx context.Context, vehicleID, station string, at time.Time, lang string) (irail.Route, error)
}

type Config struct {
	Source   Source
	Stations *stations.Directory
	Lang     string
	Location *time.Location
	Now      func() time.Time
	// From and To are the station ids of the trip to open on, such as the
	// last one; without them the screen opens on the form.
	From, To string
	// Remember saves a trip so that the next start opens on it.
	Remember func(fromID, toID string) error
	// TickEvery is how often the screen updates its clock and checks
	// whether to refresh; 15 seconds when zero.
	TickEvery time.Duration
}

type screen int

const (
	planning screen = iota
	journeys
	departures
	// stops shows every stop of the chosen journey.
	stops
	// route shows the whole run of a train chosen on the board.
	route
)

const (
	refreshEvery   = time.Minute
	tickEvery      = 15 * time.Second
	maxSuggestions = 6
)

type Model struct {
	cfg           Config
	width, height int
	screen        screen

	fields [2]field
	focus  int
	// suggested holds the stations the focused field can mean, worked out
	// when its text or the focus changes; typos says they are near misses.
	suggested []stations.Station
	typos     bool
	// choice is the highlighted suggestion under the focused field.
	choice int

	from, to stations.Station
	trip     irail.Trip
	board    irail.Liveboard
	// tripAt and boardAt are when the journeys and the departures on hand
	// were fetched, so that switching screens reuses them while fresh.
	tripAt, boardAt time.Time
	selected        int
	// scroll is how many lines of the chosen journey's stops are scrolled
	// past.
	scroll int
	// left holds the journey on the stops screen once it has left iRail's
	// list, which only has journeys still to come, so the screen keeps it
	// and follows its trains instead; leftAt is when it was last followed.
	left   *irail.Journey
	leftAt time.Time
	// boardChoice is the chosen departure and boardScroll the first one
	// shown.
	boardChoice, boardScroll int
	// route is the run of routeOf, the departure chosen on the board.
	route   irail.Route
	routeOf irail.Departure
	routeAt time.Time

	// waiting is the id of the fetch the screen waits for, or 0. An answer
	// to any other fetch, such as one for a screen the user has left, is
	// dropped.
	waiting, requests int
	// triedAt is when the screen last asked iRail, for the refresh.
	triedAt time.Time
	// err is what went wrong with the last fetch for the screen on view.
	err error
	// note tells the user something that went wrong on the side, such as
	// a trip that could not be saved.
	note string
	// first is the fetch of the trip the screen opens on. New prepares it,
	// since Init cannot keep the id it would start.
	first tea.Cmd
}

type tripMsg struct {
	id   int
	trip irail.Trip
	err  error
}

type boardMsg struct {
	id    int
	board irail.Liveboard
	err   error
}

type tickMsg time.Time

func New(cfg Config) Model {
	m := Model{cfg: cfg, fields: [2]field{{label: "From"}, {label: "To"}}}
	from, hasFrom := cfg.Stations.ByID(cfg.From)
	to, hasTo := cfg.Stations.ByID(cfg.To)
	if hasFrom && hasTo && from.ID != to.ID {
		m.setTrip(from, to)
		m.screen = journeys
		m.first = m.fetchTrip()
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.first, m.tick())
}

func (m Model) tick() tea.Cmd {
	every := m.cfg.TickEvery
	if every == 0 {
		every = tickEvery
	}
	return tea.Tick(every, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) hasTrip() bool {
	return m.from.ID != ""
}

func (m Model) loading() bool {
	return m.waiting != 0
}

// stale tells whether data fetched at t is due to be fetched again.
func (m Model) stale(t time.Time) bool {
	return t.IsZero() || m.cfg.Now().Sub(t) >= refreshEvery
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	m = next.(Model)
	switch msg.(type) {
	case tea.KeyPressMsg, tea.WindowSizeMsg, tripMsg, boardMsg, routeMsg, followMsg:
		m.keepInView()
	}
	return m, cmd
}

// keepInView holds the scroll within what there is to scroll through and
// the chosen departure on screen, after a key, a resize or new data.
func (m *Model) keepInView() {
	switch m.screen {
	case stops, route:
		m.scroll = min(m.scroll, m.maxScroll())
	case departures:
		m.showBoardChoice()
	}
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m.refresh()
	case tripMsg:
		if m.gotTrip(msg) {
			fetch := m.fetchFollow()
			return m, fetch
		}
	case boardMsg:
		m.gotBoard(msg)
	case routeMsg:
		m.gotRoute(msg)
	case followMsg:
		m.gotFollow(msg)
	case tea.PasteMsg:
		if m.screen == planning {
			m.typed(msg.Content)
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.screen {
	case planning:
		return m.planningKey(k)
	case journeys:
		return m.journeysKey(k)
	case stops:
		return m.stopsKey(k)
	case route:
		return m.routeKey(k)
	}
	return m.departuresKey(k)
}

// refresh fetches what is on screen again once refreshEvery has passed
// since the screen last asked.
func (m Model) refresh() (tea.Model, tea.Cmd) {
	next := m.tick()
	if m.loading() || !m.stale(m.triedAt) {
		return m, next
	}
	fetch := m.fetch()
	return m, tea.Batch(fetch, next)
}

// fetch asks iRail for what the screen on view shows.
func (m *Model) fetch() tea.Cmd {
	switch {
	case m.screen == departures:
		return m.fetchBoard()
	case m.screen == route:
		return m.fetchRoute()
	case m.screen == stops && m.left != nil:
		return m.fetchFollow()
	case m.screen == journeys || m.screen == stops:
		return m.fetchTrip()
	}
	return nil
}

func (m Model) planningKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := &m.fields[m.focus]
	switch k.String() {
	case "esc":
		if !m.hasTrip() {
			return m, tea.Quit
		}
		m.fields[0].pick(m.from, m.cfg.Lang)
		m.fields[1].pick(m.to, m.cfg.Lang)
		return m.toJourneys()
	case "up":
		m.choice = max(0, m.choice-1)
		return m, nil
	case "down":
		m.choice = max(0, min(len(m.suggested)-1, m.choice+1))
		return m, nil
	case "enter":
		return m.confirm(true)
	case "tab":
		return m.confirm(false)
	case "shift+tab":
		m.focus = 1 - m.focus
	case "backspace":
		f.backspace()
	case "ctrl+w":
		f.deleteWord()
	case "ctrl+u":
		f.clear()
	default:
		m.typed(k.Text)
		return m, nil
	}
	m.suggest()
	return m, nil
}

func (m *Model) typed(text string) {
	m.fields[m.focus].insert(strings.ReplaceAll(text, "\n", " "))
	m.note = ""
	m.suggest()
}

// suggest works out the stations the focused field can mean, best first.
// When nothing fits, they are the names a few typos away.
func (m *Model) suggest() {
	m.suggested, m.typos, m.choice = nil, false, 0
	f := m.fields[m.focus]
	if f.station != nil || strings.TrimSpace(f.text) == "" {
		return
	}
	m.suggested, _ = m.cfg.Stations.Lookup(f.text, m.cfg.Lang)
	if len(m.suggested) == 0 {
		m.suggested, m.typos = m.cfg.Stations.Suggest(f.text, m.cfg.Lang, 3), true
		return
	}
	m.suggested = m.suggested[:min(len(m.suggested), maxSuggestions)]
}

// confirm picks the highlighted suggestion and moves to the other field.
// With search, it looks up the journeys once both fields hold a station;
// without, it moves on even when there is nothing to pick.
func (m Model) confirm(search bool) (tea.Model, tea.Cmd) {
	f := &m.fields[m.focus]
	if f.station == nil {
		if len(m.suggested) > 0 {
			f.pick(m.suggested[min(m.choice, len(m.suggested)-1)], m.cfg.Lang)
		} else if search {
			return m, nil
		}
	}
	from, to := m.fields[0].station, m.fields[1].station
	if !search || from == nil || to == nil {
		m.focus = 1 - m.focus
		m.suggest()
		return m, nil
	}
	if from.ID == to.ID {
		m.note = "From and To are the same station; pick two different ones"
		return m, nil
	}
	return m.startTrip(*from, *to)
}

// startTrip shows a new trip and remembers it right away, in order, so
// that two quick swaps cannot save the wrong one.
func (m Model) startTrip(from, to stations.Station) (tea.Model, tea.Cmd) {
	m.setTrip(from, to)
	m.screen = journeys
	if m.cfg.Remember != nil {
		if err := m.cfg.Remember(from.ID, to.ID); err != nil {
			m.note = "could not remember this trip for next time: " + err.Error()
		}
	}
	fetch := m.fetchTrip()
	return m, fetch
}

// setTrip puts a new trip on hand, dropping the journeys and departures of
// the one before.
func (m *Model) setTrip(from, to stations.Station) {
	m.from, m.to = from, to
	m.trip, m.tripAt, m.selected = irail.Trip{}, time.Time{}, 0
	m.board, m.boardAt = irail.Liveboard{}, time.Time{}
	m.err, m.note = nil, ""
	m.fields[0].pick(from, m.cfg.Lang)
	m.fields[1].pick(to, m.cfg.Lang)
}

// The screens are switched through these, which leave no fetch, error or
// note of the screen before behind, and fetch only data that is not fresh.

func (m Model) toJourneys() (tea.Model, tea.Cmd) {
	m.screen, m.waiting, m.err, m.note, m.left, m.leftAt = journeys, 0, nil, "", nil, time.Time{}
	if !m.stale(m.tripAt) {
		return m, nil
	}
	fetch := m.fetchTrip()
	return m, fetch
}

func (m Model) toDepartures() (tea.Model, tea.Cmd) {
	m.screen, m.waiting, m.err, m.note = departures, 0, nil, ""
	if !m.stale(m.boardAt) {
		return m, nil
	}
	fetch := m.fetchBoard()
	return m, fetch
}

func (m Model) toPlanning() (tea.Model, tea.Cmd) {
	m.screen, m.waiting, m.err = planning, 0, nil
	m.focus = 1
	m.suggest()
	return m, nil
}

func (m Model) journeysKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.selected = max(0, m.selected-1)
	case "down", "j":
		m.selected = max(0, min(len(m.trip.Journeys)-1, m.selected+1))
	case "s":
		return m.startTrip(m.to, m.from)
	case "e", "/":
		return m.toPlanning()
	case "d":
		return m.toDepartures()
	case "enter", "right", "l":
		if len(m.trip.Journeys) > 0 {
			m.screen, m.scroll, m.note, m.left, m.leftAt = stops, 0, "", nil, time.Time{}
		}
	case "r":
		return m.fetchAgain()
	}
	return m, nil
}

func (m Model) stopsKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc", "left", "h", "backspace":
		return m.toJourneys()
	case "r":
		return m.fetchAgain()
	}
	return m.scrollKey(k), nil
}

func (m Model) departuresKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc", "d", "backspace":
		return m.toJourneys()
	case "e", "/":
		return m.toPlanning()
	case "r":
		return m.fetchAgain()
	case "up", "k":
		m.boardChoice = max(0, m.boardChoice-1)
	case "down", "j":
		m.boardChoice = max(0, min(len(m.board.Departures)-1, m.boardChoice+1))
	case "enter", "right", "l":
		if m.boardChoice < len(m.board.Departures) {
			return m.toRoute(m.board.Departures[m.boardChoice])
		}
	}
	return m, nil
}

// fetchAgain fetches what is on screen now, unless a fetch is under way.
func (m Model) fetchAgain() (tea.Model, tea.Cmd) {
	if m.loading() {
		return m, nil
	}
	fetch := m.fetch()
	return m, fetch
}

// begin starts a fetch the screen waits for and returns its id.
func (m *Model) begin() int {
	m.requests++
	m.waiting, m.triedAt = m.requests, m.cfg.Now()
	return m.waiting
}

func (m *Model) fetchTrip() tea.Cmd {
	id, source, fromID, toID, lang := m.begin(), m.cfg.Source, m.from.ID, m.to.ID, m.cfg.Lang
	return func() tea.Msg {
		trip, err := source.Journeys(context.Background(), fromID, toID, lang)
		return tripMsg{id: id, trip: trip, err: err}
	}
}

func (m *Model) fetchBoard() tea.Cmd {
	id, source, stationID, lang := m.begin(), m.cfg.Source, m.from.ID, m.cfg.Lang
	return func() tea.Msg {
		board, err := source.Liveboard(context.Background(), stationID, lang)
		return boardMsg{id: id, board: board, err: err}
	}
}

// gotTrip takes the answer the screen waits for. A failed refresh keeps
// the journeys already shown. It reports true when the journey on the
// stops screen has just left the list, so the screen starts to follow it.
func (m *Model) gotTrip(msg tripMsg) bool {
	if msg.id != m.waiting {
		return false
	}
	m.waiting, m.err = 0, msg.err
	if msg.err != nil {
		return false
	}
	var chosen *irail.Journey
	if m.selected < len(m.trip.Journeys) {
		chosen = &m.trip.Journeys[m.selected]
	}
	pinned := !m.keepChoice(msg.trip) && m.screen == stops && m.left == nil && chosen != nil
	if pinned {
		m.left = chosen
	}
	m.trip, m.tripAt = msg.trip, m.cfg.Now()
	return pinned
}

// shownJourney is the journey on the stops screen: the chosen one, or the
// one that has left the list while it was on screen.
func (m Model) shownJourney() irail.Journey {
	if m.left != nil {
		return *m.left
	}
	return m.trip.Journeys[m.selected]
}

// keepChoice finds the chosen journey in a refreshed list, by when it
// leaves and on which train, so the choice does not jump to another train
// when an earlier one has left. When it is gone, the choice keeps its row
// and keepChoice reports false.
func (m *Model) keepChoice(next irail.Trip) bool {
	if m.selected < len(m.trip.Journeys) {
		chosen := journeyKey(m.trip.Journeys[m.selected])
		for i, j := range next.Journeys {
			if journeyKey(j) == chosen {
				m.selected = i
				return true
			}
		}
	}
	m.selected = max(0, min(m.selected, len(next.Journeys)-1))
	return len(m.trip.Journeys) == 0
}

func journeyKey(j irail.Journey) string {
	return fmt.Sprint(j.Departure().Scheduled.Unix(), j.Legs[0].Train)
}

func (m *Model) gotBoard(msg boardMsg) {
	if msg.id != m.waiting {
		return
	}
	m.waiting, m.err = 0, msg.err
	if msg.err == nil {
		m.keepBoardChoice(msg.board)
		m.board, m.boardAt = msg.board, m.cfg.Now()
	}
}
