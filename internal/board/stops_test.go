package board

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/PieterLuypaert/trainspot/internal/irail"
)

func plainLines(lines []string) string {
	return ansi.Strip(strings.Join(lines, "\n"))
}

func twoTrainJourney() irail.Journey {
	boarded := stop("Gent-Sint-Pieters", "15:13", 5, "6")
	boarded.Passed = true
	return irail.Journey{Legs: []irail.Leg{
		{Train: "IC 3614", Direction: "Leuven", Departure: boarded, Calls: []irail.Call{
			{Station: "Wetteren", Arrival: at("15:24"), Departure: at("15:25"), DepartureDelay: 3 * time.Minute, Passed: true, Arrived: true},
			{Station: "Aalst", Arrival: at("15:43"), Departure: at("15:44"), Arrived: true},
			{Station: "Denderleeuw", Departure: at("15:52"), Extra: true},
		}, Arrival: stop("Brussel-Zuid", "16:07", 0, "14")},
		{Train: "IC 2117", Direction: "Luxemburg", Departure: stop("Brussel-Zuid", "16:13", 0, "15"),
			Calls: []irail.Call{{Station: "Brussel-Centraal", Departure: at("16:18")}}, Arrival: stop("Aarlen", "19:22", 0, "2")},
	}}
}

func TestJourneyStopsDrawARouteLine(t *testing.T) {
	lines, legend := JourneyStops(twoTrainJourney(), JourneyOptions{Width: 76, Now: at("15:30"), Location: brussels})

	want := []string{
		"  IC 3614 to Leuven                            49 min · 3 stops on the way",
		"  15:13 +5  ●  Gent-Sint-Pieters                                platform 6",
		"  15:25 +3  │  Wetteren",
		"  15:44     ◆  Aalst              train is here",
		"  15:52     │  Denderleeuw        extra stop",
		"  16:07     ●  Brussel-Zuid                                    platform 14",
		"",
		"  Change at Brussel-Zuid · 6 min · platform 14 → 15",
		"",
		"  IC 2117 to Luxemburg                            3h09 · 1 stop on the way",
		"  16:13     ●  Brussel-Zuid                                    platform 15",
		"  16:18     │  Brussel-Centraal",
		"  19:22     ●  Aarlen                                           platform 2",
	}
	if got := plainLines(lines); got != strings.Join(want, "\n") {
		t.Errorf("stops =\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	if !strings.Contains(lines[2], faint.Render("Wetteren")) || !strings.Contains(lines[1], faint.Render("Gent-Sint-Pieters")) {
		t.Errorf("passed stations are not dimmed:\n%q\n%q", lines[1], lines[2])
	}
	if text := plainLines(legend.Lines(80)); !strings.Contains(text, "min late") {
		t.Errorf("legend = %q", text)
	}
}

func TestTheChangeBlockSaysWhatTheDelaysLeave(t *testing.T) {
	change := func(arrivalDelay int, fromPlatform, toPlatform string) string {
		j := irail.Journey{Legs: []irail.Leg{
			ride("IC 1", "", stop("Gent-Sint-Pieters", "15:00", arrivalDelay, "1"), stop("Brussel-Zuid", "15:30", arrivalDelay, fromPlatform)),
			ride("IC 2", "", stop("Brussel-Zuid", "15:36", 0, toPlatform), stop("Aarlen", "18:30", 0, "4")),
		}}
		lines, _ := JourneyStops(j, JourneyOptions{Width: 80, Now: at("14:00"), Location: brussels})
		for _, l := range lines {
			if strings.Contains(ansi.Strip(l), "Change at") {
				return ansi.Strip(l)
			}
		}
		t.Fatalf("no change block in\n%s", plainLines(lines))
		return ""
	}

	cases := []struct {
		delay      int
		from, to   string
		wantChange string
	}{
		{0, "14", "15", "  Change at Brussel-Zuid · 6 min · platform 14 → 15"},
		{0, "14", "14", "  Change at Brussel-Zuid · 6 min · same platform 14"},
		{0, "", "15", "  Change at Brussel-Zuid · 6 min"},
		{2, "14", "15", "  Change at Brussel-Zuid · 4 min instead of 6 min · platform 14 → 15"},
		{4, "14", "15", "  Change at Brussel-Zuid · 2 min, tight · platform 14 → 15"},
		{10, "14", "15", "  Change at Brussel-Zuid · missed by 4 min · platform 14 → 15"},
	}
	for _, c := range cases {
		if got := change(c.delay, c.from, c.to); got != c.wantChange {
			t.Errorf("delay %d, %s → %s: %q, want %q", c.delay, c.from, c.to, got, c.wantChange)
		}
	}
}

func TestAWalkIsALineOfItsOwn(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{
		ride("S2 3787", "Brussel-Zuid", stop("Mechelen", "15:00", 0, "3"), stop("Haren-Zuid", "15:20", 0, "2")),
		{Walking: true, Departure: stop("Haren-Zuid", "15:20", 0, ""), Arrival: stop("Haren", "15:30", 0, "")},
		ride("S4 1", "Aalst", stop("Haren", "15:36", 0, "1"), stop("Aarlen", "18:00", 0, "2")),
	}}

	lines, _ := JourneyStops(j, JourneyOptions{Width: 80, Now: at("14:00"), Location: brussels})
	text := plainLines(lines)

	if !regexp.MustCompile(`  Walk to Haren +10 min`).MatchString(text) || !strings.Contains(text, "Change at Haren · 6 min") {
		t.Errorf("walk:\n%s", text)
	}
	if strings.Count(text, "Change at") != 1 {
		t.Errorf("want one change, after the walk:\n%s", text)
	}
}

func TestStopsSayWhereATrainEndsAndDropASkippedStopsDelay(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{{Train: "IC 1", Departure: stop("Gent-Sint-Pieters", "15:00", 0, "4"),
		Calls: []irail.Call{
			{Station: "Wetteren", Departure: at("15:10"), DepartureDelay: 7 * time.Minute, Cancelled: true},
			{Station: "Aalst", Arrival: at("15:20"), ArrivalDelay: 2 * time.Minute, Departure: at("15:22"), EndsHere: true},
		},
		Arrival: irail.Stop{Station: "Brussel-Zuid", Scheduled: at("15:45"), Cancelled: true}}}}

	lines, legend := JourneyStops(j, JourneyOptions{Width: 80, Now: at("14:52"), Location: brussels})
	text := plainLines(lines)

	if !regexp.MustCompile(`15:20 \+2  │  Aalst +train ends here`).MatchString(text) {
		t.Errorf("the stop where the train now ends:\n%s", text)
	}
	if !regexp.MustCompile(`Wetteren +cancelled`).MatchString(text) || strings.Contains(text, "+7") || strings.Contains(plainLines(legend.Lines(80)), "+7") {
		t.Errorf("a skipped stop:\n%s\n%s", text, plainLines(legend.Lines(80)))
	}
	if !regexp.MustCompile(`Brussel-Zuid +cancelled`).MatchString(text) || strings.Contains(text, "next stop") {
		t.Errorf("a cancelled arrival:\n%s", text)
	}
}

func route(stops ...irail.Call) irail.Route {
	return irail.Route{Train: "IC 2117", Stops: stops}
}

func runCall(station, hhmm string, passed, arrived bool) irail.Call {
	return irail.Call{Station: station, Arrival: at(hhmm), Departure: at(hhmm), Passed: passed, Arrived: arrived, Platform: "3"}
}

func TestRouteStopsShowWhereTheTrainIsAndMarkTheBoardStation(t *testing.T) {
	skipped := runCall("Ottignies", "18:18", false, false)
	skipped.Cancelled = true
	rt := route(
		runCall("Brussel-Zuid", "17:13", true, true),
		runCall("Leuven", "17:49", true, true),
		skipped,
		runCall("Namen", "18:45", false, false),
		runCall("Aarlen", "21:22", false, false),
	)

	lines, _ := RouteStops(rt, JourneyOptions{Width: 80, Now: at("18:00"), Location: brussels}, Mark{Station: "Namen", At: at("18:45")})
	text := plainLines(lines)

	if strings.Contains(text, "IC 2117") || !strings.HasPrefix(text, "  17:13  ●  Brussel-Zuid") {
		t.Errorf("a route starts with its first station, without the train's line:\n%s", text)
	}
	if !regexp.MustCompile(`18:45  ●  Namen +next stop +platform 3`).MatchString(text) {
		t.Errorf("the board's station after a skipped stop:\n%s", text)
	}
	if regexp.MustCompile(`Leuven +platform`).MatchString(text) {
		t.Errorf("a station on the way shows its platform:\n%s", text)
	}
}

func TestRouteStopsMarkTheTrainStandingAtItsFirstStation(t *testing.T) {
	rt := route(runCall("Gent-Sint-Pieters", "17:13", false, true), runCall("Aalst", "17:40", false, false), runCall("Brussel-Zuid", "18:07", false, false))

	lines, _ := RouteStops(rt, JourneyOptions{Width: 80, Now: at("17:10"), Location: brussels}, Mark{})

	if !regexp.MustCompile(`17:13  ◆  Gent-Sint-Pieters +train is here`).MatchString(plainLines(lines)) {
		t.Errorf("the train standing at its first station:\n%s", plainLines(lines))
	}
}

func TestSummaries(t *testing.T) {
	if got := ansi.Strip(JourneySummary(twoTrainJourney())); got != "4h04 · 1 change · leaves platform 6 · arrives platform 2" {
		t.Errorf("journey summary = %q", got)
	}
	rt := route(runCall("Rotterdam CS", "16:10", false, false), runCall("Breda", "16:42", false, false), runCall("Brussel-Zuid", "18:17", false, false))
	if got := ansi.Strip(RouteSummary(rt)); got != "2h07 · from Rotterdam CS to Brussel-Zuid · 1 stop on the way" {
		t.Errorf("route summary = %q", got)
	}
}

func TestTheRouteLineKeepsWhatMattersOnANarrowTerminal(t *testing.T) {
	airport := stop("Brussels Airport - Zaventem", "15:00", 0, "4")
	airport.PlatformChanged = true
	j := irail.Journey{Legs: []irail.Leg{{Train: "IC 2215", Direction: "Gent-Sint-Pieters", Departure: airport,
		Calls: []irail.Call{
			{Station: "Brussel-Noord", Arrival: at("15:15"), Departure: at("15:16"), Arrived: true},
			{Station: "Brussel-Centraal", Arrival: at("15:20"), ArrivalDelay: 12 * time.Minute, Departure: at("15:21"), EndsHere: true},
		},
		Arrival: stop("Gent-Sint-Pieters", "16:00", 0, "11")}}}

	for _, width := range []int{80, 56, 46} {
		lines, legend := JourneyStops(j, JourneyOptions{Width: width, Now: at("15:16"), Location: brussels})
		text := plainLines(lines)
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				t.Errorf("width %d: line is %d wide: %q", width, w, ansi.Strip(l))
			}
		}
		for _, want := range []string{"train is here", "train ends here"} {
			if !strings.Contains(text, want) {
				t.Errorf("width %d lost %q:\n%s", width, want, text)
			}
		}
		if !regexp.MustCompile(`Brussels A.* 4 !`).MatchString(text) || !regexp.MustCompile(`Gent-Sint.* (platform )?11`).MatchString(text) {
			t.Errorf("width %d lost a platform where you get on or off:\n%s", width, text)
		}
		if strings.Contains(text, "platform 4") != strings.Contains(text, "platform 11") {
			t.Errorf("width %d mixes the long and short platform:\n%s", width, text)
		}
		if !strings.Contains(plainLines(legend.Lines(width)), "4 ! = platform changed") {
			t.Errorf("width %d: legend = %q", width, plainLines(legend.Lines(80)))
		}
	}
}

func TestACancelledStopShowsNoDelayAndNoTrainMarker(t *testing.T) {
	gone := irail.Stop{Station: "Brussel-Zuid", Scheduled: at("17:13"), Delay: 9 * time.Minute, Cancelled: true, Arrived: true}
	j := irail.Journey{Legs: []irail.Leg{{Train: "IC 1", Departure: gone, Arrival: stop("Aarlen", "20:00", 0, "2")}}}

	lines, legend := JourneyStops(j, JourneyOptions{Width: 80, Now: at("17:00"), Location: brussels})
	text := plainLines(lines)

	if strings.Contains(text, "◆") || strings.Contains(text, "+9") || strings.Contains(plainLines(legend.Lines(80)), "+9") {
		t.Errorf("a cancelled stop shows the train or its delay:\n%s\n%s", text, plainLines(legend.Lines(80)))
	}
}

func TestATrainStandingAtTheBoardingStationIsThere(t *testing.T) {
	standing := stop("Gent-Sint-Pieters", "15:13", 0, "6")
	standing.Arrived = true
	j := irail.Journey{Legs: []irail.Leg{{Train: "IC 1", Departure: standing,
		Calls: []irail.Call{{Station: "Aalst", Departure: at("15:40")}}, Arrival: stop("Brussel-Zuid", "16:07", 0, "14")}}}

	lines, _ := JourneyStops(j, JourneyOptions{Width: 80, Now: at("15:10"), Location: brussels})
	text := plainLines(lines)

	if !regexp.MustCompile(`◆  Gent-Sint-Pieters +train is here`).MatchString(text) || strings.Contains(text, "next stop") {
		t.Errorf("a train standing at the platform:\n%s", text)
	}
}

func TestAJourneyThatStartsWithAWalkHasNoChangeBeforeItsTrain(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{
		{Walking: true, Departure: stop("Haren-Zuid", "15:20", 0, ""), Arrival: stop("Haren", "15:30", 0, "")},
		ride("S4 1", "Aalst", stop("Haren", "15:36", 0, "1"), stop("Aarlen", "18:00", 0, "2")),
	}}

	lines, _ := JourneyStops(j, JourneyOptions{Width: 80, Now: at("14:00"), Location: brussels})

	if text := plainLines(lines); strings.Contains(text, "Change at") || !strings.Contains(ansi.Strip(JourneySummary(j)), "direct") {
		t.Errorf("a walk before the first train counts as a change:\n%s\n%s", text, ansi.Strip(JourneySummary(j)))
	}
}

func TestAHeaderAndSummaryEndWhereTheTrainNowEnds(t *testing.T) {
	stops := []irail.Call{
		runCall("Brussel-Zuid", "17:13", false, false),
		{Station: "Leuven", Arrival: at("17:42"), Departure: at("17:49"), EndsHere: true},
		runCall("Namen", "18:45", false, false),
	}
	stops[2].Cancelled = true

	if got := ansi.Strip(RouteSummary(route(stops...))); !strings.HasPrefix(got, "29 min · from Brussel-Zuid to Leuven") {
		t.Errorf("summary = %q", got)
	}
	cancelled := irail.Leg{Train: "IC 1", Departure: stop("Brussel-Zuid", "17:13", 0, "1"), Arrival: irail.Stop{Station: "Namen", Scheduled: at("18:45"), Delay: 9 * time.Minute, Cancelled: true}}
	lines, _ := JourneyStops(irail.Journey{Legs: []irail.Leg{cancelled}}, JourneyOptions{Width: 80, Now: at("17:00"), Location: brussels})
	if header := ansi.Strip(lines[0]); !strings.HasSuffix(header, "cancelled") {
		t.Errorf("header of a leg whose arrival is cancelled = %q", header)
	}
}

func TestTheBoardStationIsMarkedOnlyAtItsOwnTime(t *testing.T) {
	rt := route(runCall("Oostende", "16:30", false, false), runCall("Gent-Sint-Pieters", "17:00", false, false), runCall("Brugge", "17:30", false, false),
		runCall("Gent-Sint-Pieters", "18:00", false, false), runCall("Aalst", "18:30", false, false))

	lines, _ := RouteStops(rt, JourneyOptions{Width: 80, Now: at("16:00"), Location: brussels}, Mark{Station: "Gent-Sint-Pieters", At: at("18:00")})
	text := plainLines(lines)

	if !regexp.MustCompile(`18:00  ●  Gent-Sint-Pieters +platform 3`).MatchString(text) || !strings.Contains(text, "17:00  │  Gent-Sint-Pieters") {
		t.Errorf("the station is marked at both visits, or not at its own:\n%s", text)
	}
}

func TestARouteWithoutTwoStopsDrawsNothing(t *testing.T) {
	if lines, legend := RouteStops(route(runCall("Gent-Sint-Pieters", "17:00", false, false)), JourneyOptions{Width: 80, Location: brussels}, Mark{}); lines != nil || legend.Lines(80) != nil {
		t.Errorf("got %q and %q", lines, legend.Lines(80))
	}
}
