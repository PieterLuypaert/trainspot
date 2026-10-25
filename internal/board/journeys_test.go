package board

import (
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
	return ansi.Strip(Journeys(list, JourneyOptions{From: "Gent-Sint-Pieters", To: "Aarlen", Width: width, Now: at("14:52"), Location: brussels}))
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
	if !strings.Contains(block(t, out, "15:46"), "15:46 → 19:10   3h24   direct") {
		t.Errorf("direct journey:\n%s", out)
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
		2:  {"change 4 min instead of 6"},
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

func TestJourneysShowCancelledTrains(t *testing.T) {
	cancelled := stop("Gent-Sint-Pieters", "16:07", 0, "9")
	cancelled.Cancelled = true
	j := irail.Journey{Legs: []irail.Leg{ride("IC 3616", "Leuven", cancelled, stop("Aarlen", "20:00", 0, "2"))}}

	b := block(t, renderJourneys([]irail.Journey{j}, 100), "16:07")

	if !strings.Contains(b, "16:07 → 20:00   cancelled") || !strings.Contains(b, "IC 3616 to Leuven  cancelled") {
		t.Errorf("cancelled journey:\n%s", b)
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
	if strings.Contains(b, "change 0 min") || !strings.Contains(b, "change 6 min") {
		t.Errorf("want only the change after the walk:\n%s", b)
	}
}

func TestJourneysFitNarrowTerminals(t *testing.T) {
	j := irail.Journey{Legs: []irail.Leg{
		ride("IC 2215", "Brussels Airport - Zaventem", stop("Brussels Airport - Zaventem", "15:00", 12, "4"), stop("Aarlen", "18:00", 0, "2")),
	}}

	out := renderJourneys([]irail.Journey{j}, 50)

	for _, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 50 {
			t.Errorf("line is %d wide: %q", w, l)
		}
	}
	if !strings.Contains(out, "…") {
		t.Errorf("nothing was shortened:\n%s", out)
	}
}

func TestJourneysSaySoWhenThereAreNone(t *testing.T) {
	if out := renderJourneys(nil, 80); !strings.Contains(out, "No journeys found.") {
		t.Errorf("empty list =\n%s", out)
	}
}
