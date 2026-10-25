package board

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/PieterLuypaert/trainspot/internal/irail"
)

func stop(station, hhmm string, delayMinutes int, platform string) irail.Stop {
	return irail.Stop{Station: station, Scheduled: at(hhmm), Delay: time.Duration(delayMinutes) * time.Minute, Platform: platform}
}

func ride(train, direction string, from, to irail.Stop) irail.Leg {
	return irail.Leg{Train: train, Direction: direction, Departure: from, Arrival: to}
}

func renderJourneys(list []irail.Journey, width int) string {
	return ansi.Strip(Journeys(list, JourneyOptions{From: "Gent-Sint-Pieters", To: "Aarlen", Width: width, Now: at("14:52"), Updated: at("14:51"), Location: brussels}))
}

// block returns the lines of the journey whose summary starts with prefix.
func block(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, b := range strings.Split(out, "\n\n") {
		if strings.HasPrefix(b, "  "+prefix) {
			return b
		}
	}
	t.Fatalf("no journey starting %q in\n%s", prefix, out)
	return ""
}

func TestJourneysSummariseEachWayToTravel(t *testing.T) {
	changed := stop("Gent-Sint-Pieters", "15:33", 5, "6")
	changed.PlatformChanged = true
	viaBrussels := irail.Journey{Legs: []irail.Leg{
		ride("IC 3614", "Leuven", changed, stop("Brussel-Zuid", "16:27", 0, "14")),
		ride("IC 2117", "Luxemburg", stop("Brussel-Zuid", "16:33", 0, "15"), stop("Aarlen", "19:42", 1, "2")),
	}}
	direct := irail.Journey{Legs: []irail.Leg{ride("IC 2114", "Luxemburg", stop("Gent-Sint-Pieters", "15:46", 0, "10"), stop("Aarlen", "19:10", 0, "2"))}}

	out := renderJourneys([]irail.Journey{viaBrussels, direct}, 100)

	if head := line(t, out, "→ Aarlen"); !strings.HasPrefix(head, "  Gent-Sint-Pieters → Aarlen") || !strings.HasSuffix(head, "Sun 25 Oct 14:52") {
		t.Errorf("header = %q", head)
	}
	b := block(t, out, "15:33")
	wants := []string{
		"15:33 +5 → 19:42 +1   4h05   1 change",
		"15:33 +5  Gent-Sint-Pieters  6 !  IC 3614 to Leuven",
		"16:27     Brussel-Zuid       14",
		"change 6 min",
		"16:33     Brussel-Zuid       15   IC 2117 to Luxemburg",
		"19:42 +1  Aarlen             2",
	}
	for _, want := range wants {
		if !strings.Contains(b, want) {
			t.Errorf("journey lacks %q:\n%s", want, b)
		}
	}
	if !strings.Contains(block(t, out, "15:46"), "15:46 → 19:10         3h24   direct") {
		t.Errorf("the direct journey's duration should line up with the one above:\n%s", out)
	}
	if !strings.Contains(out, "Live NMBS data via iRail, updated 14:51") {
		t.Errorf("source line should give iRail's time:\n%s", out)
	}
	if !strings.Contains(out, "+5 = 5 min late  ·  6 ! = platform changed") {
		t.Errorf("legend missing:\n%s", out)
	}
}

func TestJourneysWarnAboutChangesTheDelaysSqueeze(t *testing.T) {
	journey := func(arrivalDelay int) irail.Journey {
		return irail.Journey{Legs: []irail.Leg{
			ride("IC 1", "", stop("Gent-Sint-Pieters", "15:00", arrivalDelay, "1"), stop("Brussel-Zuid", "15:30", arrivalDelay, "2")),
			ride("IC 2", "", stop("Brussel-Zuid", "15:36", 0, "3"), stop("Aarlen", "18:30", 0, "4")),
		}}
	}
	cases := map[int][]string{
		2:  {"change 4 min instead of 6 min"},
		4:  {"change 2 min, tight"},
		10: {"change missed by 4 min", "1 change   change missed"},
	}
	for delay, wants := range cases {
		out := renderJourneys([]irail.Journey{journey(delay)}, 100)
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("delay %d: no %q in\n%s", delay, want, out)
			}
		}
	}
}

func TestJourneysNameLongChangesInHoursAndTheDayAtNight(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{
		ride("IC 522", "", stop("Gent-Sint-Pieters", "23:03", 0, "10"), irail.Stop{Station: "Luik-Guillemins", Scheduled: at("23:03").Add(138 * time.Minute)}),
		ride("IC 2426", "", irail.Stop{Station: "Luik-Guillemins", Scheduled: at("23:03").Add(337 * time.Minute)}, irail.Stop{Station: "Aarlen", Scheduled: at("23:03").Add(499 * time.Minute)}),
	}}
	tomorrow := irail.Journey{Legs: []irail.Leg{
		ride("IC 1", "", irail.Stop{Station: "Gent-Sint-Pieters", Scheduled: at("06:00").AddDate(0, 0, 1)}, irail.Stop{Station: "Aarlen", Scheduled: at("10:00").AddDate(0, 0, 1)}),
	}}

	out := renderJourneys([]irail.Journey{j, tomorrow}, 100)

	night := strings.Fields(strings.SplitN(block(t, out, "23:03"), "\n", 2)[0])
	if !strings.Contains(out, "change 3h19") || len(night) < 4 || strings.Join(night[:4], " ") != "23:03 → 07:22 8h19" {
		t.Errorf("long change or night journey:\n%s", out)
	}
	if !strings.Contains(out, "  06:00 → 10:00   4h00   direct   Mon 26 Oct") {
		t.Errorf("tomorrow's journey does not name its day at the end:\n%s", out)
	}
}

func TestJourneysShowCancelledTrains(t *testing.T) {
	cancelled := stop("Gent-Sint-Pieters", "16:07", 10, "9")
	cancelled.Cancelled = true
	j := irail.Journey{Legs: []irail.Leg{ride("IC 3616", "Leuven", cancelled, stop("Aarlen", "20:00", 0, "2"))}}

	out := renderJourneys([]irail.Journey{j}, 100)
	b := block(t, out, "16:07")

	if !strings.Contains(b, "16:07 → 20:00   cancelled") || !strings.Contains(b, "IC 3616 to Leuven  cancelled") {
		t.Errorf("cancelled journey:\n%s", b)
	}
	if strings.Contains(out, "+10") {
		t.Errorf("a cancelled train shows its delay:\n%s", out)
	}
}

func TestJourneysShowAWalkWithoutAChangeBeforeIt(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{
		ride("S2 3787", "Brussel-Zuid", stop("Mechelen", "15:00", 0, "3"), stop("Haren-Zuid", "15:20", 0, "2")),
		{Walking: true, Departure: stop("Haren-Zuid", "15:20", 0, ""), Arrival: stop("Haren", "15:30", 0, "")},
		ride("S4 1", "Aalst", stop("Haren", "15:36", 0, "1"), stop("Aarlen", "18:00", 0, "2")),
	}}

	b := block(t, renderJourneys([]irail.Journey{j}, 100), "15:00")

	if !strings.Contains(b, "15:20  Haren-Zuid  –  walk") {
		t.Errorf("walk line missing:\n%s", b)
	}
	if strings.Contains(b, "change 0 min") || !strings.Contains(b, "change 6 min") || !strings.Contains(b, "1 change") {
		t.Errorf("want only the change after the walk:\n%s", b)
	}
}

func TestJourneysCountAWalkAsPartOfTheChangeAfterIt(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{
		ride("S2 3787", "Brussel-Zuid", stop("Mechelen", "15:00", 0, "3"), stop("Haren-Zuid", "15:20", 4, "2")),
		{Walking: true, Departure: stop("Haren-Zuid", "15:20", 0, ""), Arrival: stop("Haren", "15:30", 0, "")},
		ride("S4 1", "Aalst", stop("Haren", "15:36", 0, "1"), stop("Aarlen", "18:00", 0, "2")),
	}}

	b := block(t, renderJourneys([]irail.Journey{j}, 100), "15:00")

	if !strings.Contains(b, "1 change") || strings.Contains(b, "missed") || !strings.Contains(b, "change 2 min, tight") {
		t.Errorf("the walk should leave one tight change of 2 min:\n%s", b)
	}
}

func TestJourneysFitNarrowTerminals(t *testing.T) {
	airport := irail.Journey{Legs: []irail.Leg{
		ride("IC 2215", "Brussels Airport - Zaventem", stop("Brussels Airport - Zaventem", "15:00", 12, "4"), stop("Aarlen", "18:00", 0, "2")),
	}}
	night := irail.Journey{Legs: []irail.Leg{
		ride("IC 522", "", stop("Gent-Sint-Pieters", "06:03", 12, "10"), irail.Stop{Station: "Luik-Guillemins", Scheduled: at("08:00").AddDate(0, 0, 1)}),
		ride("IC 2426", "", irail.Stop{Station: "Luik-Guillemins", Scheduled: at("07:00").AddDate(0, 0, 1), Delay: 7 * time.Minute}, irail.Stop{Station: "Aarlen", Scheduled: at("10:00").AddDate(0, 0, 1)}),
	}}
	night.Legs[0].Departure.Scheduled = night.Legs[0].Departure.Scheduled.AddDate(0, 0, 1)

	for _, width := range []int{50, 28} {
		out := renderJourneys([]irail.Journey{airport, night}, width)
		for _, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w > width {
				t.Errorf("width %d: line is %d wide: %q", width, w, l)
			}
		}
		if !strings.Contains(out, "…") {
			t.Errorf("width %d: nothing was shortened:\n%s", width, out)
		}
	}
}

func TestJourneysSaySoWhenThereAreNone(t *testing.T) {
	if out := renderJourneys(nil, 80); !strings.Contains(out, "No journeys found.") {
		t.Errorf("empty list =\n%s", out)
	}
}

func TestEachJourneyHasALegendForWhatItShows(t *testing.T) {
	changed := stop("Brussel-Zuid", "16:33", 0, "15")
	changed.PlatformChanged = true
	plain := irail.Journey{Legs: []irail.Leg{ride("IC 2114", "", stop("Gent-Sint-Pieters", "15:46", 0, "10"), stop("Aarlen", "19:10", 0, "2"))}}
	withChange := irail.Journey{Legs: []irail.Leg{ride("IC 2117", "", changed, stop("Aarlen", "19:42", 0, "2"))}}

	l := LayOutJourneys([]irail.Journey{plain, withChange}, JourneyOptions{Width: 100, Now: at("14:52"), Location: brussels})

	text := func(lines []string) string { return ansi.Strip(strings.Join(lines, "\n")) }
	if strings.Contains(text(l.Legends[0]), "platform changed") {
		t.Errorf("the first journey's legend explains a mark it does not show: %q", text(l.Legends[0]))
	}
	if !strings.Contains(text(l.Legends[1]), "platform changed") || !strings.Contains(text(l.Legend), "platform changed") {
		t.Errorf("legends = %q and %q", text(l.Legends[1]), text(l.Legend))
	}
}

func TestADayDoesNotPushAMissedChangeOffANarrowTerminal(t *testing.T) {
	missed := irail.Journey{Legs: []irail.Leg{
		ride("IC 1", "", stop("Gent-Sint-Pieters", "15:00", 10, "1"), stop("Brussel-Zuid", "15:30", 10, "2")),
		ride("IC 2", "", stop("Brussel-Zuid", "15:36", 0, "3"), stop("Aarlen", "18:30", 0, "4")),
	}}
	tomorrow := irail.Journey{Legs: []irail.Leg{
		ride("IC 3", "", irail.Stop{Station: "Gent-Sint-Pieters", Scheduled: at("06:00").AddDate(0, 0, 1)}, irail.Stop{Station: "Aarlen", Scheduled: at("10:00").AddDate(0, 0, 1)}),
	}}

	out := renderJourneys([]irail.Journey{missed, tomorrow}, 60)

	if !strings.Contains(block(t, out, "15:00"), "change missed") {
		t.Errorf("the missed change was cut:\n%s", out)
	}
}

func TestJourneyStopsListEveryStationOnTheWay(t *testing.T) {
	passed := irail.Call{Station: "Wetteren", Departure: at("15:25"), DepartureDelay: 3 * time.Minute, Passed: true}
	skipped := irail.Call{Station: "Aalst", Departure: at("15:44"), Cancelled: true}
	extra := irail.Call{Station: "Denderleeuw", Departure: at("15:52"), Extra: true}
	boarded := stop("Gent-Sint-Pieters", "15:13", 5, "6")
	boarded.Passed = true
	j := irail.Journey{Legs: []irail.Leg{
		{Train: "IC 3614", Direction: "Leuven", Departure: boarded, Calls: []irail.Call{passed, skipped, extra}, Arrival: stop("Brussel-Zuid", "16:07", 0, "14")},
		{Train: "IC 2117", Direction: "Luxemburg", Departure: stop("Brussel-Zuid", "16:13", 0, "15"),
			Calls: []irail.Call{{Station: "Brussel-Centraal", Departure: at("16:18")}}, Arrival: stop("Aarlen", "19:22", 0, "2")},
	}}

	raw, legend := JourneyStops(j, JourneyOptions{Width: 80, Now: at("15:30"), Location: brussels})
	lines := strings.Split(ansi.Strip(strings.Join(raw, "\n")), "\n")

	want := []string{
		"  IC 3614 to Leuven",
		"    15:13 +5  Gent-Sint-Pieters   6",
		"    15:25 +3  · Wetteren",
		"    15:44     · Aalst                 cancelled",
		"    15:52     · Denderleeuw           next stop  extra stop",
		"    16:07     Brussel-Zuid        14",
		"              change 6 min",
		"  IC 2117 to Luxemburg",
		"    16:13     Brussel-Zuid        15",
		"    16:18     · Brussel-Centraal",
		"    19:22     Aarlen              2",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("stops =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(raw[2], faint.Render("· Wetteren")) {
		t.Errorf("a passed station is not dimmed: %q", raw[2])
	}
	if text := ansi.Strip(strings.Join(legend, " ")); !strings.Contains(text, "min late") {
		t.Errorf("legend = %q", text)
	}
}

func TestJourneyStopsFitANarrowTerminal(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{{Train: "IC 2215", Direction: "Brussels Airport - Zaventem",
		Departure: stop("Gent-Sint-Pieters", "15:00", 0, "4"),
		Calls:     []irail.Call{{Station: "Brussels Airport - Zaventem", Departure: at("15:40"), DepartureDelay: 12 * time.Minute}},
		Arrival:   stop("Leuven", "16:00", 0, "2")}}}

	lines, _ := JourneyStops(j, JourneyOptions{Width: 36, Now: at("14:52"), Location: brussels})

	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 36 {
			t.Errorf("line is %d wide: %q", w, ansi.Strip(l))
		}
	}
}

func TestJourneyStopsSayWhereATrainNowEndsAndHideTheDelayOfASkippedStop(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{{Train: "IC 1", Departure: stop("Gent-Sint-Pieters", "15:00", 0, "4"),
		Calls: []irail.Call{
			{Station: "Wetteren", Departure: at("15:10"), DepartureDelay: 7 * time.Minute, Cancelled: true},
			{Station: "Aalst", Arrival: at("15:20"), ArrivalDelay: 2 * time.Minute, Departure: at("15:22"), EndsHere: true},
		},
		Arrival: irail.Stop{Station: "Brussel-Zuid", Scheduled: at("15:45"), Cancelled: true}}}}

	raw, legend := JourneyStops(j, JourneyOptions{Width: 80, Now: at("14:52"), Location: brussels})
	text := ansi.Strip(strings.Join(raw, "\n"))

	if !strings.Contains(text, "15:20 +2  · Aalst") || !strings.Contains(text, "train ends here") {
		t.Errorf("the stop where the train now ends:\n%s", text)
	}
	if strings.Contains(text, "+7") || strings.Contains(ansi.Strip(strings.Join(legend, " ")), "+7") {
		t.Errorf("a skipped stop shows or explains its delay:\n%s\n%s", text, ansi.Strip(strings.Join(legend, " ")))
	}
}

func TestTheJourneyListLeavesPassedStopsAlone(t *testing.T) {
	left := stop("Gent-Sint-Pieters", "15:13", 0, "9")
	left.Passed, left.PlatformChanged = true, true
	j := irail.Journey{Legs: []irail.Leg{ride("IC 3614", "Leuven", left, stop("Brussel-Zuid", "16:07", 0, "14"))}}

	out := Journeys([]irail.Journey{j}, JourneyOptions{From: "Gent-Sint-Pieters", To: "Brussel-Zuid", Width: 80, Now: at("15:20"), Location: brussels})

	if !strings.Contains(ansi.Strip(out), "9 !") || !strings.Contains(ansi.Strip(out), "9 ! = platform changed") {
		t.Errorf("the list hides the changed platform of a passed stop:\n%s", ansi.Strip(out))
	}
	if strings.Contains(out, faint.Render("Gent-Sint-Pieters")) {
		t.Errorf("the list dims a passed stop:\n%q", out)
	}
}

func TestRouteStopsShowWhereTheTrainIs(t *testing.T) {
	leaves := func(station, hhmm string, passed bool) irail.Call {
		return irail.Call{Station: station, Arrival: at(hhmm), Departure: at(hhmm), Passed: passed, Arrived: passed}
	}
	route := irail.Route{Train: "IC 2117", Stops: []irail.Call{
		leaves("Brussel-Zuid", "17:13", true),
		leaves("Leuven", "17:49", true),
		leaves("Ottignies", "18:18", false),
		leaves("Namen", "18:45", false),
		leaves("Aarlen", "21:22", false),
	}}
	atNamen := route
	atNamen.Stops = append([]irail.Call{}, route.Stops...)
	atNamen.Stops[2].Passed, atNamen.Stops[2].Arrived = true, true
	atNamen.Stops[3].Arrived = true

	text := func(r irail.Route) string {
		lines, _ := RouteStops(r, JourneyOptions{Width: 80, Now: at("18:00"), Location: brussels})
		return ansi.Strip(strings.Join(lines, "\n"))
	}

	between := text(route)
	if strings.Contains(between, "IC 2117") || !strings.HasPrefix(between, "    17:13  Brussel-Zuid") {
		t.Errorf("the route starts with the train's own line or not with its first station:\n%s", between)
	}
	if !regexp.MustCompile(`· Ottignies +next stop`).MatchString(between) || strings.Contains(between, "train is here") {
		t.Errorf("between Leuven and Ottignies:\n%s", between)
	}
	if standing := text(atNamen); !regexp.MustCompile(`· Namen +train is here`).MatchString(standing) || strings.Contains(standing, "next stop") {
		t.Errorf("standing in Namen:\n%s", standing)
	}
}

func TestPositionMarksSkipStopsTheTrainSkips(t *testing.T) {
	call := func(station, hhmm string, passed bool) irail.Call {
		return irail.Call{Station: station, Arrival: at(hhmm), Departure: at(hhmm), Passed: passed, Arrived: passed}
	}
	skipped := call("Ottignies", "18:18", false)
	skipped.Cancelled = true
	route := irail.Route{Train: "IC 1", Stops: []irail.Call{
		call("Brussel-Zuid", "17:13", true), call("Leuven", "17:49", true), skipped, call("Namen", "18:45", false), call("Aarlen", "21:22", false),
	}}

	lines, _ := RouteStops(route, JourneyOptions{Width: 80, Now: at("18:00"), Location: brussels})
	text := ansi.Strip(strings.Join(lines, "\n"))

	if !regexp.MustCompile(`· Namen +next stop`).MatchString(text) {
		t.Errorf("the stop after a skipped one is not the next stop:\n%s", text)
	}
}

func TestPositionMarksTheTrainAtTheBoardingStation(t *testing.T) {
	route := irail.Route{Train: "IC 1", Stops: []irail.Call{
		{Station: "Gent-Sint-Pieters", Departure: at("17:13"), Arrived: true},
		{Station: "Aalst", Departure: at("17:40")},
		{Station: "Brussel-Zuid", Arrival: at("18:07"), StartsHere: true},
	}}

	lines, _ := RouteStops(route, JourneyOptions{Width: 80, Now: at("17:10"), Location: brussels})
	text := ansi.Strip(strings.Join(lines, "\n"))

	if !regexp.MustCompile(`Gent-Sint-Pieters +– +train is here`).MatchString(text) {
		t.Errorf("the train standing at its first station is not marked:\n%s", text)
	}
	if !regexp.MustCompile(`Brussel-Zuid +– +cancelled`).MatchString(text) || strings.Contains(text, "next stop  cancelled") {
		t.Errorf("a cancelled last arrival:\n%s", text)
	}
}
