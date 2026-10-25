package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/PieterLuypaert/trainspot/internal/irail"
	"github.com/PieterLuypaert/trainspot/internal/stations"
)

const (
	gentID    = "BE.NMBS.008892007"
	aarlenID  = "BE.NMBS.008866001"
	brusselID = "BE.NMBS.008814001"
)

var brussels, _ = time.LoadLocation("Europe/Brussels")

var testStations = stations.NewDirectory([]stations.Station{
	{ID: gentID, Names: map[string]string{"nl": "Gent-Sint-Pieters", "fr": "Gand-Saint-Pierre"}, Standard: "Gent-Sint-Pieters"},
	{ID: "BE.NMBS.008893120", Names: map[string]string{"nl": "Gent-Dampoort", "fr": "Gand-Dampoort"}, Standard: "Gent-Dampoort"},
	{ID: brusselID, Names: map[string]string{"nl": "Brussel-Zuid", "fr": "Bruxelles-Midi"}, Standard: "Brussel-Zuid/Bruxelles-Midi"},
	{ID: "BE.NMBS.008813003", Names: map[string]string{"nl": "Brussel-Centraal", "fr": "Bruxelles-Central"}, Standard: "Brussel-Centraal/Bruxelles-Central"},
	{ID: aarlenID, Names: map[string]string{"nl": "Aarlen", "fr": "Arlon"}, Standard: "Arlon"},
})

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-25 "+hhmm, brussels)
	if err != nil {
		panic(err)
	}
	return t
}

func stop(station, hhmm, platform string) irail.Stop {
	return irail.Stop{Station: station, Scheduled: at(hhmm), Platform: platform}
}

func sampleTrip() irail.Trip {
	return irail.Trip{Updated: at("16:36"), Journeys: []irail.Journey{
		{Legs: []irail.Leg{
			{Train: "IC 3615", Direction: "Leuven", Departure: stop("Gent-Sint-Pieters", "17:13", "9"), Arrival: stop("Brussel-Zuid", "18:07", "14")},
			{Train: "IC 2118", Direction: "Aarlen", Departure: stop("Brussel-Zuid", "18:13", "15"), Arrival: stop("Aarlen", "21:22", "2")},
		}},
		{Legs: []irail.Leg{
			{Train: "IC 2114", Direction: "Luxemburg", Departure: stop("Gent-Sint-Pieters", "18:46", "10"), Arrival: stop("Aarlen", "22:10", "2")},
		}},
	}}
}

type fakeSource struct {
	trips, boards     []string
	tripErr, boardErr error
	// firstGone leaves the first journey out, as when it has left.
	firstGone bool
}

func (f *fakeSource) Journeys(_ context.Context, fromID, toID, _ string) (irail.Trip, error) {
	f.trips = append(f.trips, fromID+">"+toID)
	if f.tripErr != nil {
		return irail.Trip{}, f.tripErr
	}
	trip := sampleTrip()
	if f.firstGone {
		trip.Journeys = trip.Journeys[1:]
	}
	return trip, nil
}

func (f *fakeSource) Liveboard(_ context.Context, stationID, _ string) (irail.Liveboard, error) {
	f.boards = append(f.boards, stationID)
	if f.boardErr != nil {
		return irail.Liveboard{}, f.boardErr
	}
	return irail.Liveboard{Station: "Gent-Sint-Pieters", Updated: at("16:36"), Departures: []irail.Departure{
		{Destination: "Oostende", Scheduled: at("16:49"), Platform: "11", Train: "IC 535"},
	}}, nil
}

type harness struct {
	t          *testing.T
	m          Model
	src        *fakeSource
	now        time.Time
	remembered []string
}

func newHarness(t *testing.T, from, to string) *harness {
	h := &harness{t: t, src: &fakeSource{}, now: at("16:36")}
	h.m = New(Config{
		Source: h.src, Stations: testStations, Lang: "nl", Location: brussels,
		Now:  func() time.Time { return h.now },
		From: from, To: to,
		Remember: func(fromID, toID string) error {
			h.remembered = append(h.remembered, fromID+">"+toID)
			return nil
		},
		TickEvery: time.Nanosecond,
	})
	h.send(tea.WindowSizeMsg{Width: 90, Height: 30})
	for _, msg := range run(t, h.m.Init()) {
		h.send(msg)
	}
	return h
}

// send updates the model and runs the commands it returns, feeding their
// messages back in. Ticks come back at once in tests and are dropped, so
// a test sends a tick itself when it wants one.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	next, cmd := h.m.Update(msg)
	h.m = next.(Model)
	for _, m := range run(h.t, cmd) {
		switch m.(type) {
		case tea.QuitMsg, tickMsg:
		default:
			h.send(m)
		}
	}
}

func run(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var msgs []tea.Msg
			for _, c := range batch {
				msgs = append(msgs, run(t, c)...)
			}
			return msgs
		}
		return []tea.Msg{msg}
	case <-time.After(5 * time.Second):
		t.Fatal("a command did not answer within 5 seconds")
		return nil
	}
}

func (h *harness) press(keys ...tea.KeyPressMsg) {
	h.t.Helper()
	for _, k := range keys {
		h.send(k)
	}
}

func (h *harness) typeText(text string) {
	h.t.Helper()
	for _, r := range text {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func (h *harness) screen() string {
	return ansi.Strip(h.m.View().Content)
}

func (h *harness) wantOnScreen(wants ...string) {
	h.t.Helper()
	screen := h.screen()
	for _, want := range wants {
		if !strings.Contains(screen, want) {
			h.t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
}

var (
	enter     = tea.KeyPressMsg{Code: tea.KeyEnter}
	tab       = tea.KeyPressMsg{Code: tea.KeyTab}
	down      = tea.KeyPressMsg{Code: tea.KeyDown}
	esc       = tea.KeyPressMsg{Code: tea.KeyEscape}
	backspace = tea.KeyPressMsg{Code: tea.KeyBackspace}
	ctrlC     = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
)

func letter(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestOpensOnTheFormWithoutALastTrip(t *testing.T) {
	h := newHarness(t, "", "")

	h.wantOnScreen("Plan a trip", "Sun 25 Oct 16:36", "From  ▌", "To    station name", "Type a station's name in any language", "enter search", "esc quit")
}

func TestSuggestsStationsAsYouType(t *testing.T) {
	h := newHarness(t, "", "")

	h.typeText("brus")
	h.wantOnScreen("From  brus▌", "▸ Brussel-Centraal  Bruxelles-Central", "Brussel-Zuid      Bruxelles-Midi")

	h.press(down)
	h.wantOnScreen("▸ Brussel-Zuid")
}

func TestSaysWhenANameFitsNoStation(t *testing.T) {
	h := newHarness(t, "", "")

	h.typeText("aarlem")
	h.wantOnScreen(`no station is called "aarlem"; did you mean:`, "▸ Aarlen")

	h.press(enter)
	h.wantOnScreen("From  Aarlen  Arlon")
}

func TestPlansATripAndShowsItsJourneys(t *testing.T) {
	h := newHarness(t, "", "")

	h.typeText("gent sint")
	h.press(enter)
	h.typeText("arlon")
	h.press(enter)

	h.wantOnScreen("Gent-Sint-Pieters → Aarlen", "▸ 17:13 → 21:22   4h09   1 change", "  18:46 → 22:10   3h24   direct",
		"─────", "IC 3615 to Leuven", "change 6 min", "IC 2118 to Aarlen", "Live NMBS data via iRail, updated 16:36", "d departures")
	if strings.Join(h.src.trips, ",") != gentID+">"+aarlenID || strings.Join(h.remembered, ",") != gentID+">"+aarlenID {
		t.Errorf("asked %v, remembered %v", h.src.trips, h.remembered)
	}
}

func TestChoosesAJourneyToSeeItsStops(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.press(down)

	h.wantOnScreen("▸ 18:46 → 22:10", "IC 2114 to Luxemburg")
	if strings.Contains(h.screen(), "IC 3615") {
		t.Errorf("the first journey's stops are still shown:\n%s", h.screen())
	}
}

func TestKeepsTheChosenJourneyWhenAnEarlierOneLeaves(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.press(down)

	h.src.firstGone = true
	h.press(letter('r'))

	h.wantOnScreen("▸ 18:46 → 22:10", "IC 2114 to Luxemburg")
}

func TestOpensOnTheLastTrip(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.wantOnScreen("Gent-Sint-Pieters → Aarlen", "▸ 17:13 → 21:22")
	if len(h.src.trips) != 1 {
		t.Errorf("asked for journeys %d times, want once", len(h.src.trips))
	}
}

func TestSwapsTheTrip(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.press(letter('s'))

	h.wantOnScreen("Aarlen → Gent-Sint-Pieters")
	if last := h.src.trips[len(h.src.trips)-1]; last != aarlenID+">"+gentID {
		t.Errorf("asked for %s after the swap", last)
	}
}

func TestShowsTheDeparturesFromWhereTheTripStarts(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.press(letter('d'))
	h.wantOnScreen("Departures from Gent-Sint-Pieters", "Oostende", "IC 535", "esc journeys")

	h.press(esc)
	h.wantOnScreen("Gent-Sint-Pieters → Aarlen", "▸ 17:13 → 21:22")
	if len(h.src.boards) != 1 || h.src.boards[0] != gentID {
		t.Errorf("departures asked for %v", h.src.boards)
	}
}

func TestEditsTheTripAndGoesBack(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.press(letter('e'))
	h.wantOnScreen("Plan a trip", "From  Gent-Sint-Pieters", "To    Aarlen  Arlon", "esc back")

	h.typeText("brussel z")
	h.wantOnScreen("To    brussel z▌", "▸ Brussel-Zuid")

	h.press(esc)
	h.wantOnScreen("Gent-Sint-Pieters → Aarlen")
}

func TestRefusesTheSameStationTwice(t *testing.T) {
	h := newHarness(t, "", "")

	h.typeText("aarlen")
	h.press(enter)
	h.typeText("arlon")
	h.press(enter)

	h.wantOnScreen("From and To are the same station")
	if len(h.src.trips) != 0 {
		t.Errorf("asked for journeys: %v", h.src.trips)
	}
}

func TestKeepsTheJourneysWhenARefreshFails(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.tripErr = errors.New("no network")

	h.now = h.now.Add(time.Minute)
	h.press(letter('r'))

	h.wantOnScreen("▸ 17:13 → 21:22", "could not refresh: no network")
}

func TestRefreshesEveryMinute(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	asked := len(h.src.trips)

	h.send(tickMsg(h.now))
	if len(h.src.trips) != asked {
		t.Fatalf("refreshed within the minute")
	}
	h.now = h.now.Add(time.Minute)
	h.send(tickMsg(h.now))
	if len(h.src.trips) != asked+1 {
		t.Errorf("did not refresh after a minute")
	}
}

func TestDropsAnAnswerForATripThatChanged(t *testing.T) {
	m := New(Config{Source: &fakeSource{}, Stations: testStations, Lang: "nl", Location: brussels, Now: func() time.Time { return at("16:36") }, From: gentID, To: aarlenID, TickEvery: time.Nanosecond})
	first := m.Init()

	next, _ := m.Update(letter('s'))
	for _, msg := range run(t, first) {
		if _, tick := msg.(tickMsg); !tick {
			next, _ = next.Update(msg)
		}
	}

	screen := ansi.Strip(next.(Model).View().Content)
	if !strings.Contains(screen, "Aarlen → Gent-Sint-Pieters") || !strings.Contains(screen, "Looking up journeys…") {
		t.Errorf("the answer for the trip before the swap was shown:\n%s", screen)
	}
}

func TestLeavingTheDeparturesBeforeTheyArriveDoesNotHang(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	next, boardFetch := h.m.Update(letter('d'))
	h.m = next.(Model)

	h.press(letter('e'), esc)
	for _, msg := range run(t, boardFetch) {
		h.send(msg)
	}

	if strings.Contains(h.screen(), "updating…") {
		t.Fatalf("the journeys wait for the departures' answer:\n%s", h.screen())
	}
	asked := len(h.src.trips)
	h.press(letter('r'))
	if len(h.src.trips) != asked+1 {
		t.Errorf("r did not fetch the journeys again")
	}
}

func TestReusesFreshDataWhenSwitchingScreens(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.press(letter('d'), esc, letter('d'), esc)

	if len(h.src.trips) != 1 || len(h.src.boards) != 1 {
		t.Errorf("asked for journeys %d and departures %d times within the minute, want once each", len(h.src.trips), len(h.src.boards))
	}
	h.now = h.now.Add(time.Minute)
	h.press(letter('d'))
	if len(h.src.boards) != 2 {
		t.Errorf("departures older than a minute were not fetched again")
	}
}

func TestLeavesNoNoteOrErrorBehindWhenSwitching(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.press(letter('e'), tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	h.typeText("gent sint")
	h.press(enter)
	h.wantOnScreen("From and To are the same station")
	h.press(esc)
	h.wantOnScreen("Live NMBS data via iRail, updated 16:36")

	h.src.boardErr = errors.New("no network")
	h.now = h.now.Add(time.Minute)
	h.press(letter('d'))
	h.wantOnScreen("Could not get the departures: no network")
	h.press(esc)
	if strings.Contains(h.screen(), "no network") {
		t.Errorf("the departures' error shows on the journeys:\n%s", h.screen())
	}
}

func TestFillsTheTerminalExactly(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	for _, size := range [][2]int{{90, 30}, {50, 12}, {30, 8}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(h.screen(), "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w != size[0] {
				t.Errorf("%dx%d: line is %d wide: %q", size[0], size[1], w, l)
			}
		}
	}
}

func TestQuits(t *testing.T) {
	for name, keys := range map[string][]tea.KeyPressMsg{
		"ctrl+c on the form":     {ctrlC},
		"esc on an empty form":   {esc},
		"q on the journeys list": {enter, letter('q')},
	} {
		h := newHarness(t, "", "")
		if name == "q on the journeys list" {
			h = newHarness(t, gentID, aarlenID)
			keys = keys[1:]
		}
		var cmd tea.Cmd
		for _, k := range keys {
			var next tea.Model
			next, cmd = h.m.Update(k)
			h.m = next.(Model)
		}
		if msgs := run(t, cmd); len(msgs) != 1 || msgs[0] != (tea.QuitMsg{}) {
			t.Errorf("%s: got %v, want quit", name, msgs)
		}
	}
}

func TestFieldEditing(t *testing.T) {
	h := newHarness(t, "", "")

	h.typeText("gent sint")
	h.press(backspace)
	h.wantOnScreen("From  gent sin▌")
	h.press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	h.wantOnScreen("From  gent ▌")
	h.send(tea.PasteMsg{Content: "dampoort\n"})
	h.wantOnScreen("From  gent dampoort ▌", "▸ Gent-Dampoort")
	h.press(tab)
	h.wantOnScreen("From  Gent-Dampoort", "To    ▌")
}
