package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/PieterLuypaert/trainspot/internal/board"
	"github.com/PieterLuypaert/trainspot/internal/stations"
)

// Under the form go the recent trips, each picked with its number, and
// beside it the next departures from the station in the From field.

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

// previewFrom starts fetching the board of the station in the From field
// when the one on hand is another station's or stale, unless that fetch is
// on its way or failed less than a minute ago; the refresh tries again.
// Update calls it after everything that happens on the form, so a new
// station is looked up whichever way it got into the field.
func (m *Model) previewFrom() tea.Cmd {
	from := m.fields[0].station
	switch {
	case from == nil, from.ID == m.previewID && !m.stale(m.previewAt):
		return nil
	case from.ID == m.previewAsked && (m.loading() || m.previewErr != nil && !m.stale(m.previewAskedAt)):
		return nil
	}
	return m.fetchPreview()
}

func (m *Model) fetchPreview() tea.Cmd {
	m.previewAsked, m.previewAskedAt = m.fields[0].station.ID, m.cfg.Now()
	return m.fetchLiveboard(m.previewAsked)
}

// gotPreview takes the board the form waits for. A failed refresh keeps
// the board already shown.
func (m *Model) gotPreview(msg boardMsg) {
	if msg.id != m.waiting {
		return
	}
	m.waiting, m.previewErr = 0, msg.err
	if msg.err == nil {
		m.preview, m.previewID, m.previewAt = msg.board, msg.station, m.cfg.Now()
	}
}

// previewFailed tells whether the last fetch of the board of the station in
// the From field failed, with none on its way.
func (m Model) previewFailed() bool {
	return m.previewErr != nil && !m.loading() && m.fields[0].station != nil && m.fields[0].station.ID == m.previewAsked
}

// recentLines list the recent trips under the form, numbered to pick,
// while a number picks one; as the user types a name, the suggestions
// take their place. A trip too long for the width takes two lines, so
// trips from the same station still tell apart.
func (m Model) recentLines(width int) []string {
	if !m.picksByNumber() {
		return nil
	}
	lines := []string{"", indent + bold.Render("Recent trips")}
	for i, t := range m.picks {
		number := indent + "  " + bold.Render(strconv.Itoa(i+1)) + "  "
		from, to := t.from.Name(m.cfg.Lang), t.to.Name(m.cfg.Lang)
		if line := number + from + faint.Render(" → ") + to; lipgloss.Width(line) <= width {
			lines = append(lines, line)
			continue
		}
		lines = append(lines, number+from, strings.Repeat(" ", lipgloss.Width(number))+faint.Render("→ ")+to)
	}
	return lines
}

// planningSide is the next departures from the station in the From field,
// in at most rows lines, for beside or under the form.
func (m Model) planningSide(width, rows int) []string {
	return append([]string{""}, m.previewLines(width, rows-1)...)
}

// previewLines are the next departures from the station in the From
// field, with their legend and when they came, or what stands in their
// place.
func (m Model) previewLines(width, rows int) []string {
	from := m.fields[0].station
	switch {
	case from == nil:
		return wrap("Pick where you leave from to see its next trains.", width, faint)
	case m.previewID != from.ID && m.previewFailed():
		return wrap("Could not get the departures: "+m.previewErr.Error(), width, alarm)
	case m.previewID != from.ID:
		return wrap("Looking up departures from "+from.Name(m.cfg.Lang)+"…", width, faint)
	}
	// The title, the blank line over the legend and the status line take
	// three rows.
	departures, legend := m.previewBoard(width, rows-3)
	lines := append([]string{indent + bold.Render("Next from "+from.Name(m.cfg.Lang))}, departures...)
	return append(append(append(lines, ""), legend...), m.previewStatus())
}

// previewBoard draws as many of the next departures as fit in rows, with
// the legend for those alone, so it explains only what shows.
func (m Model) previewBoard(width, rows int) (lines, legend []string) {
	o := board.Options{Width: width, Now: m.cfg.Now(), Location: m.cfg.Location}
	shown := m.preview
	lines, legend = board.DepartureLines(shown, o)
	if len(lines)+len(legend) <= rows || len(shown.Departures) <= 1 {
		return lines, legend
	}
	// The column titles and the line about the rest take two rows. Fewer
	// departures never need more legend than all of them.
	shown.Departures = shown.Departures[:max(1, rows-2-len(legend))]
	lines, legend = board.DepartureLines(shown, o)
	more := len(m.preview.Departures) - len(shown.Departures)
	return append(lines, indent+faint.Render(fmt.Sprintf("… %d more; a taller window shows them", more))), legend
}

// previewStatus says when the departures beside the form came, or that a
// refresh is on its way or failed while they stay on screen.
func (m Model) previewStatus() string {
	switch {
	case m.loading() && m.previewAsked == m.previewID:
		return indent + faint.Render("updating…")
	case m.previewFailed():
		return indent + alarm.Render("could not refresh: "+m.previewErr.Error())
	}
	return board.Footer(m.preview.Updated, m.cfg.Location)
}
