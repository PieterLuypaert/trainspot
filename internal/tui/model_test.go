package tui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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

func calls(from string, stations ...string) []irail.Call {
	var list []irail.Call
	for i, s := range stations {
		list = append(list, irail.Call{Station: s, Departure: at(from).Add(time.Duration(i*10) * time.Minute)})
	}
	return list
}

func sampleTrip() irail.Trip {
	return irail.Trip{Updated: at("16:36"), Journeys: []irail.Journey{
		{Legs: []irail.Leg{
			{Train: "IC 3615", Vehicle: "BE.NMBS.IC3615", Direction: "Leuven", Departure: stop("Gent-Sint-Pieters", "17:13", "9"),
				Calls: calls("17:20", "Wetteren", "Aalst", "Denderleeuw"), Arrival: stop("Brussel-Zuid", "18:07", "14")},
			{Train: "IC 2118", Vehicle: "BE.NMBS.IC2118", Direction: "Aarlen", Departure: stop("Brussel-Zuid", "18:13", "15"),
				Calls:   calls("18:20", "Brussel-Centraal", "Brussel-Noord", "Leuven", "Ottignies", "Gembloux", "Namen", "Ciney", "Marloie", "Libramont", "Marbehan"),
				Arrival: stop("Aarlen", "21:22", "2")},
		}},
		{Legs: []irail.Leg{
			{Train: "IC 2114", Direction: "Luxemburg", Departure: stop("Gent-Sint-Pieters", "18:46", "10"), Arrival: stop("Aarlen", "22:10", "2")},
		}},
	}}
}

type fakeSource struct {
	trips, boards, routes []string
	tripErr, boardErr     error
	// firstGone leaves the first journey out, as when it has left.
	firstGone bool
	// departures is how many departures the board lists; one when zero.
	departures int
	// trip, when set, is the answer for journeys instead of sampleTrip.
	trip *irail.Trip
	// board, when set, is the answer for every board.
	board     *irail.Liveboard
	routeErrs map[string]error
	// routeStops is how many stops of the route the answer keeps; all of
	// them when zero. aalstLate makes the train late at Aalst.
	routeStops int
	aalstLate  time.Duration
}

func (f *fakeSource) Journeys(_ context.Context, fromID, toID, _ string) (irail.Trip, error) {
	f.trips = append(f.trips, fromID+">"+toID)
	if f.tripErr != nil {
		return irail.Trip{}, f.tripErr
	}
	trip := sampleTrip()
	if f.trip != nil {
		trip = *f.trip
	}
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
	if f.board != nil {
		return *f.board, nil
	}
	board := irail.Liveboard{Station: "Gent-Sint-Pieters", Updated: at("16:36")}
	for i := range max(1, f.departures) {
		board.Departures = append(board.Departures, irail.Departure{
			Destination: "Oostende", Scheduled: at("16:49").Add(time.Duration(i) * time.Minute), Delay: 2 * time.Minute, Platform: "11",
			Train: fmt.Sprintf("IC %d", 535+i), Vehicle: fmt.Sprintf("BE.NMBS.IC%d", 535+i),
		})
	}
	return board, nil
}

// Route runs every train from Gent-Sint-Pieters by Wetteren and Aalst to
// Brussel-Zuid, standing in Wetteren: past the first station, at the second.
func (f *fakeSource) RouteThrough(_ context.Context, vehicleID, _ string, _ time.Time, _ string) (irail.Route, error) {
	f.routes = append(f.routes, vehicleID)
	if err := f.routeErrs[vehicleID]; err != nil {
		return irail.Route{}, err
	}
	call := func(station, hhmm string, passed, arrived bool) irail.Call {
		return irail.Call{Station: station, Arrival: at(hhmm), Departure: at(hhmm), Passed: passed, Arrived: arrived}
	}
	stops := []irail.Call{
		call("Gent-Sint-Pieters", "17:13", true, true),
		call("Wetteren", "17:20", false, true),
		call("Aalst", "17:30", false, false),
		call("Brussel-Zuid", "18:07", false, false),
	}
	stops[2].ArrivalDelay, stops[2].DepartureDelay = f.aalstLate, f.aalstLate
	if f.routeStops > 0 {
		stops = stops[:f.routeStops]
	}
	return irail.Route{Train: strings.TrimPrefix(vehicleID, "BE.NMBS."), Updated: at("16:40"), Stops: stops}, nil
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
	var recent Trips
	if from != "" {
		recent = Trips{{From: from, To: to}}
	}
	h.m = New(Config{
		Source: h.src, Stations: testStations, Lang: "nl", Location: brussels,
		Now:    func() time.Time { return h.now },
		Recent: recent,
		Remember: func(trips Trips) error {
			h.remembered = append(h.remembered, trips[0].From+">"+trips[0].To)
			return nil
		},
		TickEvery:   time.Nanosecond,
		SettleAfter: time.Nanosecond,
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
		"─────", "IC 3615 to Leuven", "Change at Brussel-Zuid · 6 min", "IC 2118 to Aarlen", "Live NMBS data via iRail, updated 16:36", "d departures")
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
	m := New(Config{Source: &fakeSource{}, Stations: testStations, Lang: "nl", Location: brussels, Now: func() time.Time { return at("16:36") }, Recent: Trips{{From: gentID, To: aarlenID}}, TickEvery: time.Nanosecond})
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

func TestKeepsTheLegendAndStatusInViewOnAFullBoard(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.departures = 30
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})

	h.press(letter('d'))

	h.wantOnScreen("+2 = 2 min late", "Live NMBS data via iRail, updated 16:36")
	if !regexp.MustCompile(`↓ \d+ more`).MatchString(h.screen()) {
		t.Errorf("a full board does not say there is more below:\n%s", h.screen())
	}
	if lines := strings.Split(h.screen(), "\n"); len(lines) != 24 {
		t.Errorf("%d lines, want 24", len(lines))
	}
}

func TestSaysWhenTheTripCannotBeRemembered(t *testing.T) {
	h := newHarness(t, "", "")
	h.m.cfg.Remember = func(Trips) error { return errors.New("disk full") }

	h.typeText("gent sint")
	h.press(enter)
	h.typeText("aarlen")
	h.press(enter)

	h.wantOnScreen("▸ 17:13 → 21:22", "could not remember this trip for next time: disk full")
}

func TestShowsEveryStopOfTheChosenJourney(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.press(enter)

	h.wantOnScreen("17:13 → 21:22 · Gent-Sint-Pieters → Aarlen", "4h09 · 1 change · leaves platform 9 · arrives platform 2",
		"IC 3615 to Leuven", "│  Wetteren", "│  Denderleeuw", "Change at Brussel-Zuid · 6 min · platform 14 → 15",
		"IC 2118 to Aarlen", "│  Marbehan", "●  Aarlen", "esc journeys")
	h.press(esc)
	h.wantOnScreen("▸ 17:13 → 21:22")
}

func TestScrollsThroughALongListOfStops(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 14})
	h.press(enter)
	moreBelow := regexp.MustCompile(`↓ \d+ more`)
	if !moreBelow.MatchString(h.screen()) {
		t.Errorf("a long list does not say there is more below:\n%s", h.screen())
	}
	h.wantOnScreen("│  Wetteren")

	h.press(down, down, down, down)
	h.wantOnScreen("↑ 4 more")
	if strings.Contains(h.screen(), "IC 3615 to Leuven") {
		t.Errorf("scrolling down kept the first lines:\n%s", h.screen())
	}

	for range 40 {
		h.press(down)
	}
	h.wantOnScreen("●  Aarlen", "Live NMBS data via iRail, updated 16:36")
	if moreBelow.MatchString(h.screen()) {
		t.Errorf("at the end of the list it still says there is more:\n%s", h.screen())
	}
}

func TestKeepsTheStopsOfAJourneyWhoseTrainHasLeft(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.press(enter)

	h.src.firstGone = true
	h.press(letter('r'))

	h.wantOnScreen("17:13 → 21:22 · Gent-Sint-Pieters → Aarlen", "IC 3615 to Leuven", "Following the train live")
	h.press(esc)
	h.wantOnScreen("▸ 18:46 → 22:10", "Live NMBS data via iRail, updated 16:36")
	if strings.Contains(h.screen(), "Following the train") {
		t.Errorf("the note outlives the stops screen:\n%s", h.screen())
	}
}

func TestScrollStaysWithinTheListWhenTheWindowGrows(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 14})
	h.press(enter)
	for range 40 {
		h.press(down)
	}

	h.send(tea.WindowSizeMsg{Width: 80, Height: 20})
	before := h.screen()
	h.press(tea.KeyPressMsg{Code: tea.KeyUp})

	if h.screen() == before {
		t.Errorf("up did nothing right after the window grew:\n%s", h.screen())
	}
}

func TestNoScrollingWhenTheStopsFitExactly(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.press(enter)
	main, tail, _ := h.m.scrollView()
	h.send(tea.WindowSizeMsg{Width: 90, Height: len(main) + len(tail) + 2})

	before := h.screen()
	h.press(down)

	if h.screen() != before || h.m.scroll != 0 {
		t.Errorf("down scrolled a list that fits (scroll %d):\n%s", h.m.scroll, h.screen())
	}
}

func TestATinyWindowKeepsTheStatusLine(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.press(enter)

	h.send(tea.WindowSizeMsg{Width: 60, Height: 6})

	h.wantOnScreen("Live NMBS data via iRail")
}

func TestPageDownMovesAWholePage(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 14})
	h.press(enter)
	_, _, shown := h.m.scrollView()

	h.press(tea.KeyPressMsg{Code: tea.KeyPgDown})

	h.wantOnScreen(fmt.Sprintf("↑ %d more", shown))
}

func TestTheStopsTitleNamesTheDayAndACancellation(t *testing.T) {
	trip := sampleTrip()
	j := &trip.Journeys[0]
	j.Legs[0].Departure.Scheduled = j.Legs[0].Departure.Scheduled.AddDate(0, 0, 1)
	j.Legs[1].Arrival.Cancelled = true
	m := New(Config{Source: &fakeSource{}, Stations: testStations, Lang: "nl", Location: brussels, Now: func() time.Time { return at("16:36") }, Recent: Trips{{From: gentID, To: aarlenID}}})
	m.trip, m.screen = trip, stops

	if title := strings.Split(ansi.Strip(m.View().Content), "\n")[0]; !strings.Contains(title, "Mon 26 Oct 17:13 → 21:22 cancelled") {
		t.Errorf("title = %q", title)
	}
}

func TestKeyHintsMakeWayOnANarrowTerminal(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	lastLine := func() string {
		lines := strings.Split(h.screen(), "\n")
		return lines[len(lines)-1]
	}

	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	if bottom := lastLine(); !strings.Contains(bottom, "enter stops") || !strings.Contains(bottom, "q quit") || strings.Contains(bottom, "refresh") {
		t.Errorf("80 columns: %q", bottom)
	}
	h.send(tea.WindowSizeMsg{Width: 120, Height: 24})
	if bottom := lastLine(); !strings.Contains(bottom, "r refresh") {
		t.Errorf("120 columns: %q", bottom)
	}
}

func TestChoosesADepartureToSeeItsTrainsRoute(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.departures = 3
	h.press(letter('d'))
	h.wantOnScreen("▸ 16:49", "enter route")

	h.press(down, enter)

	h.wantOnScreen("IC 536 to Oostende · from Gent-Sint-Pieters 16:50", "54 min · from Gent-Sint-Pieters to Brussel-Zuid · 2 stops on the way",
		"●  Gent-Sint-Pieters", "◆  Wetteren", "train is here", "│  Aalst", "●  Brussel-Zuid", "esc departures")
	if len(h.src.routes) != 1 || h.src.routes[0] != "BE.NMBS.IC536" {
		t.Errorf("routes asked for %v", h.src.routes)
	}
	h.press(esc)
	h.wantOnScreen("Departures from Gent-Sint-Pieters", "▸ 16:50")
}

func TestKeepsTheChosenDepartureInViewOnALongBoard(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.departures = 30
	h.send(tea.WindowSizeMsg{Width: 80, Height: 20})
	h.press(letter('d'))

	for range 25 {
		h.press(down)
	}

	h.wantOnScreen("▸ 17:14", "↑ ", "Live NMBS data via iRail")
	if lines := strings.Split(h.screen(), "\n"); len(lines) != 20 {
		t.Errorf("%d lines, want 20", len(lines))
	}
}

func TestRefreshesTheRouteEveryMinute(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.press(letter('d'), enter)
	asked := len(h.src.routes)

	h.now = h.now.Add(time.Minute)
	h.send(tickMsg(h.now))

	if len(h.src.routes) != asked+1 {
		t.Errorf("the route was not fetched again after a minute")
	}
}

func TestFollowsAJourneyOnceItsTrainHasLeftTheList(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.press(enter)

	h.src.firstGone = true
	h.press(letter('r'))

	if strings.Join(h.src.routes, ",") != "BE.NMBS.IC3615,BE.NMBS.IC2118" {
		t.Errorf("routes asked for %v, want both trains of the journey", h.src.routes)
	}
	h.wantOnScreen("17:13 → 21:22 · Gent-Sint-Pieters → Aarlen", "◆  Wetteren", "train is here", "Following the train live, updated 16:40")
}

func TestFollowsTheOtherTrainsWhenOneCannotBeFound(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.routeErrs = map[string]error{"BE.NMBS.IC2118": errors.New("not found")}
	h.press(enter)

	h.src.firstGone = true
	h.press(letter('r'))

	h.wantOnScreen("train is here", "Following the train live, updated 16:40")
}

func TestSaysSoWhenNoTrainOfTheJourneyCanBeFollowed(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.routeErrs = map[string]error{"BE.NMBS.IC3615": errors.New("not found")}
	h.press(enter)

	h.src.firstGone = true
	h.press(letter('r'))

	h.wantOnScreen("IC 3615 to Leuven", "could not refresh: not found")
}

func TestARouteOpensAtTheTopAfterScrollingTheStops(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 9})
	h.press(enter, down, down, down, down, down, esc)

	h.press(letter('d'), enter)

	h.wantOnScreen("●  Gent-Sint-Pieters")
	if h.m.scroll != 0 {
		t.Errorf("the route opened scrolled %d lines down", h.m.scroll)
	}
}

func TestAWideWindowPutsTheRouteLineBesideTheList(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.send(tea.WindowSizeMsg{Width: 130, Height: 30})

	var row string
	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, "▸ 17:13 → 21:22") {
			row = l
		}
	}
	if !strings.Contains(row, "IC 3615 to Leuven") {
		t.Errorf("the chosen journey's train is not beside it: %q", row)
	}
	h.wantOnScreen("│  Wetteren", "Change at Brussel-Zuid", "enter stops")
	if hasRule(h.screen()) {
		t.Errorf("a wide window still stacks the panes:\n%s", h.screen())
	}
}

// hasRule tells whether a rule runs across the inside of the frame.
func hasRule(screen string) bool {
	for _, l := range strings.Split(screen, "\n") {
		if strings.HasPrefix(l, "│") && strings.Contains(l, "──────────") {
			return true
		}
	}
	return false
}

func TestANarrowWindowPutsTheRouteLineUnderTheList(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 40})

	h.wantOnScreen("▸ 17:13 → 21:22", "│  Wetteren", "●  Aarlen")
	if !hasRule(h.screen()) {
		t.Errorf("no rule between the list and the route line:\n%s", h.screen())
	}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 16})
	h.wantOnScreen("more lines; enter shows all stops", "Live NMBS data via iRail")
}

func manyJourneys(n int) irail.Trip {
	trip := irail.Trip{Updated: at("16:36")}
	for i := range n {
		leaves := at("17:00").Add(time.Duration(i) * 30 * time.Minute)
		trip.Journeys = append(trip.Journeys, irail.Journey{Legs: []irail.Leg{{
			Train: fmt.Sprintf("IC %d", 100+i), Direction: "Aarlen",
			Departure: irail.Stop{Station: "Gent-Sint-Pieters", Scheduled: leaves, Platform: "9"},
			Arrival:   irail.Stop{Station: "Aarlen", Scheduled: leaves.Add(4 * time.Hour), Platform: "2"},
		}}})
	}
	return trip
}

func TestTheChosenJourneyStaysInViewInALongList(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	trip := manyJourneys(10)
	h.src.trip = &trip
	h.press(letter('r'))
	h.send(tea.WindowSizeMsg{Width: 130, Height: 12})

	for range 8 {
		h.press(down)
	}

	h.wantOnScreen("▸ 21:00", "IC 108 to Aarlen", "↑ ")
}

func TestAWideWindowKeepsAMissedChangeInTheList(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	trip := sampleTrip()
	trip.Journeys[0].Legs[0].Arrival.Delay = 12 * time.Minute
	h.src.trip = &trip
	h.press(letter('r'))

	h.send(tea.WindowSizeMsg{Width: 104, Height: 30})

	h.wantOnScreen("change missed")
}

func TestATinyJourneysScreenKeepsItsStatusLine(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)

	h.send(tea.WindowSizeMsg{Width: 80, Height: 8})

	h.wantOnScreen("Live NMBS data via iRail")
}

func twoRecentTrips(t *testing.T) *harness {
	h := newHarness(t, gentID, aarlenID)
	h.m.setRecent(Trips{{From: gentID, To: aarlenID}, {From: brusselID, To: gentID}})
	h.press(letter('e'))
	return h
}

func TestTheFormListsTheRecentTripsToPickByNumber(t *testing.T) {
	h := twoRecentTrips(t)
	h.wantOnScreen("Recent trips", "1  Gent-Sint-Pieters → Aarlen", "2  Brussel-Zuid → Gent-Sint-Pieters", "1-2 recent trip")

	h.press(letter('2'))

	h.wantOnScreen("Brussel-Zuid → Gent-Sint-Pieters")
	if last := h.remembered[len(h.remembered)-1]; last != brusselID+">"+gentID {
		t.Errorf("remembered %s, want the picked trip first", last)
	}
}

func TestANumberTypedIntoANameIsJustText(t *testing.T) {
	h := twoRecentTrips(t)

	h.press(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	h.typeText("a2")

	h.wantOnScreen("To    a2▌")
}

func TestTheFormShowsTheNextDeparturesFromTheChosenStation(t *testing.T) {
	h := newHarness(t, "", "")
	h.wantOnScreen("Pick where you leave from to see its next trains.")

	h.typeText("gent sint")
	h.press(enter)

	h.wantOnScreen("Next from Gent-Sint-Pieters", "Oostende", "IC 535")
	if len(h.src.boards) != 1 || h.src.boards[0] != gentID {
		t.Errorf("boards asked for %v", h.src.boards)
	}
}

func TestAWideFormHasTheDeparturesBesideItAndTheRecentTripsUnderIt(t *testing.T) {
	h := twoRecentTrips(t)
	h.send(tea.WindowSizeMsg{Width: 130, Height: 30})

	if from := screenRow(h, "From  Gent-Sint-Pieters"); !strings.Contains(from, "Next from Gent-Sint-Pieters") {
		t.Errorf("the departures are not beside the form: %q", from)
	}
	recent := strings.TrimPrefix(screenRow(h, "Recent trips"), "│")
	if divider := strings.Index(recent, "│"); divider == strings.LastIndex(recent, "│") || strings.Index(recent, "Recent trips") > divider {
		t.Errorf("the recent trips are not under the form, left of the divider: %q", recent)
	}
}

// screenRow is the last row of the screen that holds text.
func screenRow(h *harness, text string) string {
	var row string
	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, text) {
			row = l
		}
	}
	return row
}

func manyBrusselStations() *stations.Directory {
	var list []stations.Station
	for i, name := range []string{"Centraal", "Congres", "Kapellekerk", "Luxemburg", "Noord", "Schuman", "West", "Zuid"} {
		list = append(list, stations.Station{ID: fmt.Sprintf("BE.NMBS.0088100%02d", i), Names: map[string]string{"nl": "Brussel-" + name}, Standard: "Brussel-" + name})
	}
	return stations.NewDirectory(list)
}

func TestTheFormSuggestsEveryStationThatFits(t *testing.T) {
	h := newHarness(t, "", "")
	h.m.cfg.Stations = manyBrusselStations()

	h.typeText("brus")

	h.wantOnScreen("Brussel-Centraal", "Brussel-West", "Brussel-Zuid")
	if strings.Contains(h.screen(), "more; type more of the name") {
		t.Errorf("a window with room for all says some are left out:\n%s", h.screen())
	}
}

func TestAShrunkWindowKeepsTheChosenSuggestionInView(t *testing.T) {
	h := newHarness(t, "", "")
	h.m.cfg.Stations = manyBrusselStations()
	h.send(tea.WindowSizeMsg{Width: 80, Height: 40})
	h.typeText("brus")
	for range 7 {
		h.press(down)
	}

	h.send(tea.WindowSizeMsg{Width: 80, Height: 12})

	h.wantOnScreen("▸ Brussel-Noord")
	h.press(enter)
	h.wantOnScreen("From  Brussel-Noord")
}

func TestTheDeparturesUnderANarrowFormStayWhileItSuggests(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.press(letter('e'), tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	h.m.cfg.Stations = numberedBrusselStations(25)

	h.typeText("brus")

	h.wantOnScreen("more; type more of the name", "Next from Gent-Sint-Pieters", "IC 535", "Live NMBS data via iRail")
}

func TestLongRecentTripsStillTellApart(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	airport := "BE.NMBS.008819406"
	h.m.cfg.Stations = stations.NewDirectory([]stations.Station{
		{ID: airport, Names: map[string]string{"nl": "Brussels Airport - Zaventem"}, Standard: "Brussels Airport - Zaventem"},
		{ID: gentID, Names: map[string]string{"nl": "Gent-Sint-Pieters"}, Standard: "Gent-Sint-Pieters"},
		{ID: "BE.NMBS.008893120", Names: map[string]string{"nl": "Gent-Dampoort"}, Standard: "Gent-Dampoort"},
	})
	h.m.setRecent(Trips{{From: airport, To: gentID}, {From: airport, To: "BE.NMBS.008893120"}})

	h.send(tea.WindowSizeMsg{Width: 104, Height: 30})
	h.press(letter('e'))

	h.wantOnScreen("1  Brussels Airport - Zaventem", "→ Gent-Sint-Pieters", "→ Gent-Dampoort")
}

func TestTheRecentTripsMakeWayForSuggestions(t *testing.T) {
	h := twoRecentTrips(t)
	h.press(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})

	h.typeText("gen")
	if strings.Contains(h.screen(), "Recent trips") {
		t.Errorf("the recent trips show while numbers would be typed:\n%s", h.screen())
	}

	h.press(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	h.wantOnScreen("Recent trips", "1  Gent-Sint-Pieters → Aarlen")
}

func TestALongNameGoesWithoutNotesInANarrowForm(t *testing.T) {
	h := newHarness(t, "", "")
	h.m.cfg.Stations = stations.NewDirectory([]stations.Station{
		{ID: brusselID, Names: map[string]string{"nl": "Brussel-Zuid", "fr": "Bruxelles-Midi"}, Standard: "Brussel-Zuid/Bruxelles-Midi"},
		{ID: "BE.NMBS.008819406", Names: map[string]string{"nl": "Brussels Airport - Zaventem", "fr": "Bruxelles-Aéroport - Zaventem"}, Standard: "Brussels Airport - Zaventem/Bruxelles-Aéroport - Zaventem"},
	})
	h.send(tea.WindowSizeMsg{Width: 110, Height: 30})

	h.typeText("bru")

	h.wantOnScreen("Brussel-Zuid", "Bruxelles-Midi", "Brussels Airport - Zaventem")
	if strings.Contains(h.screen(), "Br…") || strings.Contains(h.screen(), "Bruxelles-Aéroport") {
		t.Errorf("a name too long for its notes shows a stub of them:\n%s", h.screen())
	}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 30})
	h.wantOnScreen("Bruxelles-Aéroport - Zaventem")
}

func numberedBrusselStations(n int) *stations.Directory {
	var list []stations.Station
	for i := range n {
		list = append(list, stations.Station{ID: fmt.Sprintf("BE.NMBS.0088200%02d", i), Names: map[string]string{"nl": fmt.Sprintf("Brussel-%02d", i)}, Standard: fmt.Sprintf("Brussel-%02d", i)})
	}
	return stations.NewDirectory(list)
}

func TestARefusedSearchSaysWhyBesideTheSuggestions(t *testing.T) {
	h := newHarness(t, "", "")
	h.m.cfg.Stations = numberedBrusselStations(25)
	h.send(tea.WindowSizeMsg{Width: 140, Height: 24})
	h.typeText("brus")
	h.press(enter)
	h.typeText("brus")

	h.press(enter)
	h.wantOnScreen("From and To are the same station")

	h.press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	h.wantOnScreen("From and To are the same station", "Brussel-00", "more; type more of the name")
}

func TestASmallWindowSaysHowManySuggestionsAreLeftOut(t *testing.T) {
	h := newHarness(t, "", "")
	h.m.cfg.Stations = manyBrusselStations()
	h.send(tea.WindowSizeMsg{Width: 80, Height: 12})
	h.typeText("brus")

	h.wantOnScreen("Brussel-Noord", "… 3 more; type more of the name", "To    station name")
	for range 10 {
		h.press(down)
	}
	h.press(enter)

	h.wantOnScreen("From  Brussel-Noord")
}

// sendOnly updates the model without running the commands it returns, as
// while their answers are on their way.
func (h *harness) sendOnly(msg tea.Msg) tea.Cmd {
	h.t.Helper()
	next, cmd := h.m.Update(msg)
	h.m = next.(Model)
	return cmd
}

func (h *harness) answer(cmds ...tea.Cmd) {
	h.t.Helper()
	for _, cmd := range cmds {
		for _, msg := range run(h.t, cmd) {
			if _, tick := msg.(tickMsg); !tick {
				h.send(msg)
			}
		}
	}
}

var shiftTab = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}

func TestANewFromStationNeverShowsTheOldStationsBoard(t *testing.T) {
	h := newHarness(t, "", "")
	h.typeText("gent sint")
	h.press(enter, shiftTab, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	h.typeText("brussel z")

	fetch := h.sendOnly(enter)

	h.wantOnScreen("Looking up departures from Brussel-Zuid…")
	if strings.Contains(h.screen(), "IC 535") {
		t.Errorf("Gent's board shows while Brussel-Zuid's is on its way:\n%s", h.screen())
	}
	h.answer(fetch)
	h.wantOnScreen("Next from Brussel-Zuid", "IC 535")
}

func TestTabbingWhileTheBoardIsOnItsWayAsksOnce(t *testing.T) {
	h := newHarness(t, "", "")
	h.typeText("gent sint")

	cmds := []tea.Cmd{h.sendOnly(enter)}
	for range 4 {
		cmds = append(cmds, h.sendOnly(tab))
	}
	h.answer(cmds...)

	if len(h.src.boards) != 1 {
		t.Errorf("asked for %d boards, want 1: %v", len(h.src.boards), h.src.boards)
	}
	h.wantOnScreen("Next from Gent-Sint-Pieters")
}

func TestTheSameStationNoteStillLooksUpTheNewFromStation(t *testing.T) {
	h := newHarness(t, "", "")
	h.typeText("brussel z")
	h.press(enter)
	h.typeText("gent sint")
	h.press(tab, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	h.typeText("gent sint")

	h.press(enter)

	h.wantOnScreen("From and To are the same station", "Next from Gent-Sint-Pieters")
	if got := strings.Join(h.src.boards, " "); got != brusselID+" "+gentID {
		t.Errorf("boards asked for %s", got)
	}
}

func TestAFailedRefreshBesideTheFormSaysSo(t *testing.T) {
	h := newHarness(t, "", "")
	h.typeText("gent sint")
	h.press(enter)
	h.wantOnScreen("Live NMBS data via iRail, updated 16:36")

	h.src.boardErr = errors.New("no network")
	h.now = h.now.Add(2 * time.Minute)
	h.send(tickMsg(h.now))

	h.wantOnScreen("could not refresh: no network", "IC 535")
}

func TestAFailedBoardBesideTheFormIsNotAskedForOnEveryKey(t *testing.T) {
	h := newHarness(t, "", "")
	h.src.boardErr = errors.New("no network")
	h.typeText("gent sint")

	h.press(enter, tab, tab)

	h.wantOnScreen("Could not get the departures: no network")
	if len(h.src.boards) != 1 {
		t.Errorf("asked for %d boards, want 1", len(h.src.boards))
	}
}

func TestTheBoardBesideTheFormExplainsOnlyWhatShows(t *testing.T) {
	h := newHarness(t, "", "")
	board := irail.Liveboard{Station: "Gent-Sint-Pieters", Updated: at("16:36")}
	for i := range 30 {
		board.Departures = append(board.Departures, irail.Departure{
			Destination: "Oostende", Scheduled: at("16:49").Add(time.Duration(i) * time.Minute), Platform: "11", Train: fmt.Sprintf("IC %d", 535+i),
		})
	}
	board.Departures[29].Delay = 7 * time.Minute
	h.src.board = &board
	h.typeText("gent sint")

	h.press(enter)

	h.wantOnScreen("IC 535", "more; a taller window shows them", "Live NMBS data via iRail")
	if strings.Contains(h.screen(), "min late") {
		t.Errorf("the legend explains a delay that does not show:\n%s", h.screen())
	}
}

func TestTheRecentTripHintMakesWayAndHidesWhileTyping(t *testing.T) {
	h := twoRecentTrips(t)

	h.send(tea.WindowSizeMsg{Width: 64, Height: 30})
	h.wantOnScreen("esc back")
	if strings.Contains(h.screen(), "recent trip ") {
		t.Errorf("the recent trip hint did not make way for esc:\n%s", h.screen())
	}

	h.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	h.press(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	h.typeText("gen")
	if strings.Contains(h.screen(), "1-2 recent trip") {
		t.Errorf("the hint offers numbers that would be typed:\n%s", h.screen())
	}
}

func TestARecentTripFromAStationToItselfIsLeftOut(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.m.setRecent(Trips{{From: gentID, To: gentID}, {From: brusselID, To: gentID}})
	h.press(letter('e'))

	h.wantOnScreen("1  Brussel-Zuid → Gent-Sint-Pieters", "1 recent trip")
	if strings.Contains(h.screen(), "Gent-Sint-Pieters → Gent-Sint-Pieters") {
		t.Errorf("a trip from a station to itself is listed:\n%s", h.screen())
	}
}

func TestAWideFormWrapsItsHint(t *testing.T) {
	h := newHarness(t, "", "")

	h.send(tea.WindowSizeMsg{Width: 110, Height: 30})

	h.wantOnScreen("Type a station's name", "brussels airport.")
}

func TestAWideBoardShowsTheChosenTrainsRouteBesideIt(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.departures = 3
	h.send(tea.WindowSizeMsg{Width: 140, Height: 30})

	h.press(letter('d'))

	var row string
	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, "▸ 16:49") {
			row = l
		}
	}
	if !besidePane(row) {
		t.Errorf("no pane beside the chosen departure: %q", row)
	}
	h.wantOnScreen("IC 535 to Oostende", "◆  Wetteren", "train is here")
	h.press(down, tea.KeyPressMsg{Code: tea.KeyUp})
	if strings.Join(h.src.routes, ",") != "BE.NMBS.IC535,BE.NMBS.IC536" {
		t.Errorf("routes asked for %v, want each train once", h.src.routes)
	}

	h.press(enter)
	if len(h.src.routes) != 2 {
		t.Errorf("opening the route fetched it again: %v", h.src.routes)
	}
	h.wantOnScreen("IC 535 to Oostende · from Gent-Sint-Pieters 16:49")
}

// besidePane tells whether a row has a divider between two panes, not
// just the frame's own borders.
func besidePane(row string) bool {
	return strings.Contains(strings.Trim(row, "│ "), " │ ")
}

func wideBoard(t *testing.T) *harness {
	h := newHarness(t, gentID, aarlenID)
	h.src.departures = 3
	h.send(tea.WindowSizeMsg{Width: 140, Height: 30})
	h.press(letter('d'))
	return h
}

func TestANarrowBoardHasNoPaneBesideIt(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.press(letter('d'))

	for _, l := range strings.Split(h.screen(), "\n") {
		if strings.Contains(l, "▸ 16:49") && besidePane(l) {
			t.Errorf("a narrow board has a pane beside it: %q", l)
		}
	}
}

func TestTheRouteBesideTheBoardIsAskedForOnceWhileOnItsWay(t *testing.T) {
	h := wideBoard(t)
	h.m.routes = map[string]knownRoute{}
	key := departureKey(h.m.board.Departures[0])

	first, second := h.sendOnly(settleMsg{key: key}), h.sendOnly(settleMsg{key: key, again: true})

	if second != nil {
		t.Error("a second fetch started while the first was on its way")
	}
	h.answer(first)
	h.wantOnScreen("◆  Wetteren")
}

func TestAFailedRefreshKeepsTheRouteBesideTheBoard(t *testing.T) {
	h := wideBoard(t)
	h.src.routeErrs = map[string]error{"BE.NMBS.IC535": errors.New("iRail is down")}

	h.press(letter('r'))

	h.wantOnScreen("◆  Wetteren", "could not refresh its route: iRail is down")
}

func TestRTriesAFailedRouteBesideTheBoardAgain(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.departures = 3
	h.src.routeErrs = map[string]error{"BE.NMBS.IC535": errors.New("iRail is down")}
	h.send(tea.WindowSizeMsg{Width: 140, Height: 30})
	h.press(letter('d'))
	h.wantOnScreen("Could not get its route: iRail is down")

	h.src.routeErrs = nil
	h.press(letter('r'))

	h.wantOnScreen("◆  Wetteren")
}

func TestTheRouteScreenRefreshesAMinuteAfterItsRouteCame(t *testing.T) {
	h := wideBoard(t)
	h.now = h.now.Add(10 * time.Second)
	h.press(down)
	h.press(tea.KeyPressMsg{Code: tea.KeyUp})
	h.now = h.now.Add(10 * time.Second)
	h.press(letter('r'), down)
	asked := len(h.src.routes)

	h.press(enter)
	h.now = h.now.Add(55 * time.Second)
	h.send(tickMsg(h.now))

	if len(h.src.routes) != asked+1 || h.src.routes[asked] != "BE.NMBS.IC536" {
		t.Errorf("routes asked for %v; want IC 536's again, a minute after it came", h.src.routes)
	}
}

func TestANarrowedWindowFetchesNoRouteThatWasWaitingToSettle(t *testing.T) {
	h := wideBoard(t)
	settle := h.sendOnly(down)

	h.send(tea.WindowSizeMsg{Width: 90, Height: 30})
	h.answer(settle)

	if strings.Join(h.src.routes, ",") != "BE.NMBS.IC535" {
		t.Errorf("routes asked for %v after the window narrowed", h.src.routes)
	}
}

func TestAPaneSaysWhenIRailHasNoRoute(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.routeStops = 1
	h.send(tea.WindowSizeMsg{Width: 140, Height: 30})

	h.press(letter('d'))

	h.wantOnScreen("iRail has no route for this train.")
}

func TestThePaneExplainsTheMarksOfItsRoute(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.aalstLate = 4 * time.Minute
	h.send(tea.WindowSizeMsg{Width: 140, Height: 30})

	h.press(letter('d'))

	h.wantOnScreen("+4 = 4 min late")
}

func TestTheRoutesOfTrainsThatLeftAreDropped(t *testing.T) {
	h := wideBoard(t)
	h.press(down)
	h.src.departures = 1

	h.press(letter('r'))

	if len(h.m.routes) != 1 {
		t.Errorf("kept %d routes for a board of one train", len(h.m.routes))
	}
	h.press(esc, letter('s'))
	if len(h.m.routes) != 0 {
		t.Errorf("a new trip kept %d routes of the old board", len(h.m.routes))
	}
}

// busyBoard has the long destination, train and crowd that make a board
// wide.
func busyBoard() *irail.Liveboard {
	board := &irail.Liveboard{Station: "Gent-Sint-Pieters", Updated: at("16:36")}
	for i := range 3 {
		board.Departures = append(board.Departures, irail.Departure{
			Destination: "Brussels Airport - Zaventem", Scheduled: at("16:49").Add(time.Duration(i) * time.Minute), Delay: 2 * time.Minute,
			Platform: "7", PlatformChanged: true, Train: fmt.Sprintf("IC %d", 2219+i), Vehicle: fmt.Sprintf("BE.NMBS.IC%d", 2219+i), Occupancy: "medium",
		})
	}
	return board
}

func TestABoardWithoutRoomForThePaneKeepsTheWholeWidth(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.board = busyBoard()
	h.send(tea.WindowSizeMsg{Width: 110, Height: 30})

	h.press(letter('d'))

	if row := screenRow(h, "▸ 16:49"); besidePane(row) || !strings.Contains(row, "medium") {
		t.Errorf("the board gave up a column for a pane: %q", row)
	}
	if len(h.src.routes) != 0 {
		t.Errorf("routes asked for without a pane to show them: %v", h.src.routes)
	}
}

func TestAWideBoardKeepsEveryColumnBesideThePane(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.board = busyBoard()
	h.send(tea.WindowSizeMsg{Width: 140, Height: 30})

	h.press(letter('d'))

	if row := screenRow(h, "▸ 16:49"); !besidePane(row) || !strings.Contains(row, "Brussels Airport - Zaventem") || !strings.Contains(row, "medium") {
		t.Errorf("the board beside the pane lost a column or its destination: %q", row)
	}
	h.wantOnScreen("IC 2219 to Brussels Airport - Zaventem")
}

func TestAnEmptyBoardKeepsTheWholeWidth(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.board = &irail.Liveboard{Station: "Gent-Sint-Pieters", Updated: at("16:36")}
	h.send(tea.WindowSizeMsg{Width: 140, Height: 20})

	h.press(letter('d'))

	if row := screenRow(h, "No departures"); besidePane(row) || !strings.Contains(row, "No departures are listed right now.") {
		t.Errorf("an empty board shares the window with a pane: %q", row)
	}
}

func TestTheBoardsLegendRunsUnderThePaneToo(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	board := &irail.Liveboard{Station: "Gent-Sint-Pieters", Updated: at("16:36")}
	for i := range 3 {
		board.Departures = append(board.Departures, irail.Departure{
			Destination: "Oostende", Scheduled: at("16:49").Add(time.Duration(i) * time.Minute), Delay: 12 * time.Minute,
			Platform: "11", PlatformChanged: true, Train: fmt.Sprintf("IC %d", 535+i), Vehicle: fmt.Sprintf("BE.NMBS.IC%d", 535+i),
		})
	}
	h.src.board = board
	h.send(tea.WindowSizeMsg{Width: 140, Height: 30})

	h.press(letter('d'))

	if row := screenRow(h, "+12 = 12 min late"); !strings.Contains(row, "platform changed") {
		t.Errorf("the legend wrapped at the board's width: %q", row)
	}
}

func TestTheBoardsLegendExplainsOnlyTheDeparturesInView(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	board := irail.Liveboard{Station: "Gent-Sint-Pieters", Updated: at("16:36")}
	for i := range 30 {
		board.Departures = append(board.Departures, irail.Departure{
			Destination: "Oostende", Scheduled: at("16:49").Add(time.Duration(i) * time.Minute), Platform: "11",
			Train: fmt.Sprintf("IC %d", 535+i), Vehicle: fmt.Sprintf("BE.NMBS.IC%d", 535+i),
		})
	}
	board.Departures[29].Delay = 7 * time.Minute
	h.src.board = &board

	h.press(letter('d'))
	if strings.Contains(h.screen(), "min late") {
		t.Errorf("the legend explains a delay that does not show:\n%s", h.screen())
	}

	for range 29 {
		h.press(down)
	}
	h.wantOnScreen("+7 = 7 min late")
}

func TestTheBoardsStatusLineStaysAsItsLegendChanges(t *testing.T) {
	for _, width := range []int{48, 49, 50} {
		h := newHarness(t, gentID, aarlenID)
		board := irail.Liveboard{Station: "Gent-Sint-Pieters", Updated: at("16:36")}
		for i := range 30 {
			d := irail.Departure{Destination: "Oostende", Scheduled: at("16:49").Add(time.Duration(i) * time.Minute), Platform: "11", Train: fmt.Sprintf("IC %d", 535+i)}
			switch {
			case i == 0:
				d.Delay, d.Platform, d.PlatformChanged = 2*time.Minute, "1", true
			case i >= 20:
				d.Delay, d.Platform, d.PlatformChanged = 15*time.Minute, "12", true
			}
			board.Departures = append(board.Departures, d)
		}
		h.src.board = &board
		h.send(tea.WindowSizeMsg{Width: width, Height: 20})
		h.press(letter('d'))

		for range 29 {
			h.press(down)
		}

		if !strings.Contains(h.screen(), "Live NMBS data via iRail") {
			t.Errorf("at %d columns the status line ran off the frame:\n%s", width, h.screen())
		}
	}
}

func TestANarrowBoardFetchesNoRouteUntilAsked(t *testing.T) {
	h := newHarness(t, gentID, aarlenID)
	h.src.departures = 3

	h.press(letter('d'), down, down)

	if len(h.src.routes) != 0 {
		t.Errorf("a narrow board fetched routes: %v", h.src.routes)
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

	h.typeText("brussel-")
	h.press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	h.wantOnScreen("To    ▌")
}

func TestTabMovesOnFromAnEmptyField(t *testing.T) {
	h := newHarness(t, "", "")

	h.press(tab)
	h.wantOnScreen("From  station name", "To    ▌")

	h.typeText("xyzzy")
	h.press(tab)
	h.wantOnScreen("From  ▌", "To    xyzzy")
}

func TestCtrlWWalksBackThroughAPickedName(t *testing.T) {
	f := field{}
	f.pick(stations.Station{ID: gentID, Names: map[string]string{"nl": "Gent-Sint-Pieters"}}, "nl")
	var steps []string
	for range 3 {
		f.deleteWord()
		steps = append(steps, f.text)
	}
	if strings.Join(steps, "|") != "Gent-Sint-|Gent-|" {
		t.Errorf("ctrl+w steps = %q", steps)
	}
}
