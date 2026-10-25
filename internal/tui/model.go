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

// Source is where the screen gets its journeys and departures.
type Source interface {
	Journeys(ctx context.Context, fromID, toID, lang string) (irail.Trip, error)
	Liveboard(ctx context.Context, stationID, lang string) (irail.Liveboard, error)
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
	// choice is the highlighted suggestion under the focused field.
	choice int

	from, to stations.Station
	trip     irail.Trip
	board    irail.Liveboard
	// tripAt and boardAt are when the journeys and the departures on hand
	// were fetched, so that switching screens reuses them while fresh.
	tripAt, boardAt time.Time
	selected        int

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

type rememberedMsg struct{ err error }

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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m.refresh()
	case tripMsg:
		m.gotTrip(msg)
	case boardMsg:
		m.gotBoard(msg)
	case rememberedMsg:
		if msg.err != nil {
			m.note = "could not remember this trip for next time: " + msg.err.Error()
		}
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
	var fetch tea.Cmd
	switch m.screen {
	case journeys:
		fetch = m.fetchTrip()
	case departures:
		fetch = m.fetchBoard()
	}
	return m, tea.Batch(fetch, next)
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
	case "down":
		list, _ := m.suggestions()
		m.choice = max(0, min(len(list)-1, m.choice+1))
	case "enter":
		return m.confirm(true)
	case "tab":
		return m.confirm(false)
	case "shift+tab":
		m.focus, m.choice = 1-m.focus, 0
	case "backspace":
		f.backspace()
		m.choice = 0
	case "ctrl+w":
		f.deleteWord()
		m.choice = 0
	case "ctrl+u":
		f.clear()
		m.choice = 0
	default:
		m.typed(k.Text)
	}
	return m, nil
}

func (m *Model) typed(text string) {
	m.fields[m.focus].insert(strings.ReplaceAll(text, "\n", " "))
	m.note, m.choice = "", 0
}

// suggestions are the stations the focused field can mean, best first.
// When nothing fits, they are the names a few typos away, and typos is
// true.
func (m Model) suggestions() (list []stations.Station, typos bool) {
	f := m.fields[m.focus]
	if f.station != nil || strings.TrimSpace(f.text) == "" {
		return nil, false
	}
	list, _ = m.cfg.Stations.Lookup(f.text, m.cfg.Lang)
	if len(list) == 0 {
		return m.cfg.Stations.Suggest(f.text, m.cfg.Lang, 3), true
	}
	return list[:min(len(list), maxSuggestions)], false
}

// confirm picks the highlighted suggestion and moves to the other field.
// With search, it looks up the journeys once both fields hold a station.
func (m Model) confirm(search bool) (tea.Model, tea.Cmd) {
	f := &m.fields[m.focus]
	if f.station == nil {
		list, _ := m.suggestions()
		if len(list) == 0 {
			return m, nil
		}
		f.pick(list[min(m.choice, len(list)-1)], m.cfg.Lang)
	}
	m.choice = 0
	from, to := m.fields[0].station, m.fields[1].station
	if !search || from == nil || to == nil {
		m.focus = 1 - m.focus
		return m, nil
	}
	if from.ID == to.ID {
		m.note = "From and To are the same station; pick two different ones"
		return m, nil
	}
	return m.startTrip(*from, *to)
}

func (m Model) startTrip(from, to stations.Station) (tea.Model, tea.Cmd) {
	m.setTrip(from, to)
	m.screen = journeys
	fetch := m.fetchTrip()
	return m, tea.Batch(fetch, m.remember())
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
	m.screen, m.waiting, m.err, m.note = journeys, 0, nil, ""
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
	m.focus, m.choice = 1, 0
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
	case "r":
		return m.fetchAgain()
	}
	return m, nil
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
	}
	return m, nil
}

// fetchAgain fetches what is on screen now, unless a fetch is under way.
func (m Model) fetchAgain() (tea.Model, tea.Cmd) {
	if m.loading() {
		return m, nil
	}
	var fetch tea.Cmd
	if m.screen == departures {
		fetch = m.fetchBoard()
	} else {
		fetch = m.fetchTrip()
	}
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

func (m Model) remember() tea.Cmd {
	if m.cfg.Remember == nil {
		return nil
	}
	remember, fromID, toID := m.cfg.Remember, m.from.ID, m.to.ID
	return func() tea.Msg { return rememberedMsg{remember(fromID, toID)} }
}

// gotTrip takes the answer the screen waits for. A failed refresh keeps
// the journeys already shown.
func (m *Model) gotTrip(msg tripMsg) {
	if msg.id != m.waiting {
		return
	}
	m.waiting, m.err = 0, msg.err
	if msg.err == nil {
		m.keepChoice(msg.trip)
		m.trip, m.tripAt = msg.trip, m.cfg.Now()
	}
}

// keepChoice finds the chosen journey in a refreshed list, by when it
// leaves and on which train, so the choice does not jump to another train
// when an earlier one has left. When it is gone, the choice keeps its row.
func (m *Model) keepChoice(next irail.Trip) {
	if m.selected < len(m.trip.Journeys) {
		chosen := journeyKey(m.trip.Journeys[m.selected])
		for i, j := range next.Journeys {
			if journeyKey(j) == chosen {
				m.selected = i
				return
			}
		}
	}
	m.selected = max(0, min(m.selected, len(next.Journeys)-1))
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
		m.board, m.boardAt = msg.board, m.cfg.Now()
	}
}
