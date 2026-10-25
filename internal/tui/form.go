package tui

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/PieterLuypaert/trainspot/internal/board"
	"github.com/PieterLuypaert/trainspot/internal/stations"
)

// Beside the form go the recent trips, each picked with its number, and
// the next departures from the station in the From field.

// recentTrip is a recent trip with its stations found.
type recentTrip struct{ from, to stations.Station }

// setRecent puts the recent trips on hand, with the stations of those that
// can be picked: both stations still known, and not one and the same.
func (m *Model) setRecent(trips Trips) {
	m.recent, m.picks = trips, nil
	for _, t := range trips {
		from, hasFrom := m.cfg.Stations.ByID(t.From)
		to, hasTo := m.cfg.Stations.ByID(t.To)
		if hasFrom && hasTo && from.ID != to.ID {
			m.picks = append(m.picks, recentTrip{from: from, to: to})
		}
	}
}

// picksByNumber tells whether a number key picks a recent trip: only while
// the focused field holds no typed text, so a station name with digits can
// still be typed.
func (m Model) picksByNumber() bool {
	f := m.fields[m.focus]
	return len(m.picks) > 0 && (f.text == "" || f.station != nil)
}

// recentKey reads a key as the number of a recent trip.
func (m Model) recentKey(text string) (int, bool) {
	if !m.picksByNumber() {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > len(m.picks) {
		return 0, false
	}
	return n, true
}

func (m Model) pickRecent(n int) (tea.Model, tea.Cmd) {
	t := m.picks[n-1]
	return m.startTrip(t.from, t.to)
}

// previewFrom fetches the board of the station in the From field, unless
// the one on hand is that station's and fresh.
func (m Model) previewFrom() (tea.Model, tea.Cmd) {
	from := m.fields[0].station
	if from == nil || (m.previewID == from.ID && !m.stale(m.previewAt)) {
		return m, nil
	}
	fetch := m.fetchPreview()
	return m, fetch
}

func (m *Model) fetchPreview() tea.Cmd {
	id, source, stationID, lang := m.begin(), m.cfg.Source, m.fields[0].station.ID, m.cfg.Lang
	m.previewID = stationID
	return func() tea.Msg {
		board, err := source.Liveboard(context.Background(), stationID, lang)
		return previewMsg{id: id, board: board, err: err}
	}
}

func (m *Model) gotPreview(msg previewMsg) {
	if msg.id != m.waiting {
		return
	}
	m.waiting, m.err = 0, msg.err
	if msg.err == nil {
		m.preview, m.previewAt = msg.board, m.cfg.Now()
	}
}

// planningSide lists the recent trips, numbered to pick, and the next
// departures from the station in the From field, in at most rows lines.
func (m Model) planningSide(width, rows int) []string {
	lines := []string{""}
	if len(m.picks) > 0 {
		lines = append(lines, indent+bold.Render("Recent trips"))
		for i, t := range m.picks {
			lines = append(lines, indent+"  "+bold.Render(strconv.Itoa(i+1))+"  "+t.from.Name(m.cfg.Lang)+faint.Render(" → ")+t.to.Name(m.cfg.Lang))
		}
		lines = append(lines, "")
	}
	return append(lines, m.previewLines(width, rows-len(lines))...)
}

// previewLines are the next departures from the station in the From
// field, or what stands in their place.
func (m Model) previewLines(width, rows int) []string {
	from := m.fields[0].station
	switch {
	case from == nil:
		return []string{indent + faint.Render("Pick where you leave from to see its next trains.")}
	case m.previewID != from.ID || m.preview.Updated.IsZero():
		if m.err != nil {
			return []string{indent + alarm.Render("Could not get the departures: "+m.err.Error())}
		}
		return []string{indent + faint.Render("Looking up departures from "+from.Name(m.cfg.Lang)+"…")}
	}
	departures, _ := board.DepartureLines(m.preview, board.Options{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location})
	title := indent + bold.Render("Next from "+from.Name(m.cfg.Lang))
	return append([]string{title}, cut(departures, rows-1, "a taller window shows them")...)
}
