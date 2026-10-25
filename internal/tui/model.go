// Package tui is the full-screen trip planner that plain trainspot opens:
// a form that suggests stations as you type, the journeys between them and
// the departures from where the trip starts, all kept up to date.
package tui

import (
	"context"
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
	fetchTimeout   = 20 * time.Second
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
	hasTrip  bool
	trip     irail.Trip
	board    irail.Liveboard
	selected int
	loading  bool
	fetched  time.Time
	err      error
	// note tells the user something that went wrong on the side, such as
	// a trip that could not be saved.
	note string
}

type tripMsg struct {
	fromID, toID string
	trip         irail.Trip
	err          error
}

type boardMsg struct {
	stationID string
	board     irail.Liveboard
	err       error
}

type tickMsg time.Time

type rememberedMsg struct{ err error }

func New(cfg Config) Model {
	m := Model{cfg: cfg, fields: [2]field{{label: "From"}, {label: "To"}}}
	from, hasFrom := cfg.Stations.ByID(cfg.From)
	to, hasTo := cfg.Stations.ByID(cfg.To)
	if hasFrom && hasTo && from.ID != to.ID {
		m.setTrip(from, to)
		m.screen, m.loading = journeys, true
	}
	return m
}

func (m Model) Init() tea.Cmd {
	if m.screen == journeys {
		return tea.Batch(m.fetchTrip(), tick())
	}
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
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

// refresh fetches what is on screen again once refreshEvery has passed.
func (m Model) refresh() (tea.Model, tea.Cmd) {
	if m.loading || m.cfg.Now().Sub(m.fetched) < refreshEvery {
		return m, tick()
	}
	switch m.screen {
	case journeys:
		m.loading = true
		return m, tea.Batch(m.fetchTrip(), tick())
	case departures:
		m.loading = true
		return m, tea.Batch(m.fetchBoard(), tick())
	}
	return m, tick()
}

func (m Model) planningKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := &m.fields[m.focus]
	switch k.String() {
	case "esc":
		if !m.hasTrip {
			return m, tea.Quit
		}
		m.fields[0].pick(m.from, m.cfg.Lang)
		m.fields[1].pick(m.to, m.cfg.Lang)
		m.screen = journeys
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
	m.screen, m.loading = journeys, true
	return m, tea.Batch(m.fetchTrip(), m.remember())
}

func (m *Model) setTrip(from, to stations.Station) {
	m.from, m.to, m.hasTrip = from, to, true
	m.trip, m.err, m.selected, m.note = irail.Trip{}, nil, 0, ""
	m.fields[0].pick(from, m.cfg.Lang)
	m.fields[1].pick(to, m.cfg.Lang)
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
		m.screen, m.focus, m.choice = planning, 1, 0
	case "d":
		m.screen, m.board, m.err, m.loading = departures, irail.Liveboard{}, nil, true
		return m, m.fetchBoard()
	case "r":
		return m.fetchAgain(m.fetchTrip)
	}
	return m, nil
}

func (m Model) departuresKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc", "d", "backspace":
		m.screen, m.err, m.loading = journeys, nil, true
		return m, m.fetchTrip()
	case "e", "/":
		m.screen, m.focus, m.choice = planning, 1, 0
	case "r":
		return m.fetchAgain(m.fetchBoard)
	}
	return m, nil
}

func (m Model) fetchAgain(fetch func() tea.Cmd) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	m.loading = true
	return m, fetch()
}

func (m Model) fetchTrip() tea.Cmd {
	source, fromID, toID, lang := m.cfg.Source, m.from.ID, m.to.ID, m.cfg.Lang
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		trip, err := source.Journeys(ctx, fromID, toID, lang)
		return tripMsg{fromID: fromID, toID: toID, trip: trip, err: err}
	}
}

func (m Model) fetchBoard() tea.Cmd {
	source, stationID, lang := m.cfg.Source, m.from.ID, m.cfg.Lang
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		board, err := source.Liveboard(ctx, stationID, lang)
		return boardMsg{stationID: stationID, board: board, err: err}
	}
}

func (m Model) remember() tea.Cmd {
	if m.cfg.Remember == nil {
		return nil
	}
	remember, fromID, toID := m.cfg.Remember, m.from.ID, m.to.ID
	return func() tea.Msg { return rememberedMsg{remember(fromID, toID)} }
}

// gotTrip keeps the journeys that answer the trip on screen; an answer for
// a trip the user has since changed is dropped. A failed refresh keeps the
// journeys already shown.
func (m *Model) gotTrip(msg tripMsg) {
	if !m.hasTrip || msg.fromID != m.from.ID || msg.toID != m.to.ID || m.screen == departures {
		return
	}
	m.loading, m.fetched, m.err = false, m.cfg.Now(), msg.err
	if msg.err == nil {
		m.trip = msg.trip
		m.selected = max(0, min(m.selected, len(m.trip.Journeys)-1))
	}
}

func (m *Model) gotBoard(msg boardMsg) {
	if msg.stationID != m.from.ID || m.screen != departures {
		return
	}
	m.loading, m.fetched, m.err = false, m.cfg.Now(), msg.err
	if msg.err == nil {
		m.board = msg.board
	}
}
