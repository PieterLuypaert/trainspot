package board

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/PieterLuypaert/trainspot/internal/irail"
)

var brussels, _ = time.LoadLocation("Europe/Brussels")

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-25 "+hhmm, brussels)
	if err != nil {
		panic(err)
	}
	return t
}

func sampleBoard() irail.Liveboard {
	return irail.Liveboard{
		Station: "Bergen",
		Updated: at("14:52"),
		Departures: []irail.Departure{
			{Destination: "Brugge", Scheduled: at("14:47"), Platform: "6", PlatformChanged: true, Train: "L 13097", Occupancy: "low"},
			{Destination: "Oostende", Scheduled: at("14:49"), Delay: 3 * time.Minute, Platform: "11", Train: "IC 535", Occupancy: "medium"},
			{Destination: "Antwerpen-Centraal", Scheduled: at("14:53"), Delay: 9 * time.Minute, Platform: "4", Train: "IC 1814", Occupancy: "high"},
			{Destination: "Kortrijk", Scheduled: at("15:08"), Platform: "8", Cancelled: true, Train: "L 885"},
			{Destination: "Lokeren", Scheduled: at("14:45"), Platform: "5", Departed: true, Train: "S53 664"},
			{Destination: "Brussels Airport - Zaventem", Scheduled: at("15:15"), Train: "IC 2215", Extra: true},
		},
	}
}

func render(b irail.Liveboard, width int) string {
	return ansi.Strip(Render(b, Options{Station: "Bergen", LocalName: "Mons", Width: width, Now: at("14:52"), Location: brussels}))
}

func line(t *testing.T, out, containing string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, containing) {
			return l
		}
	}
	t.Fatalf("no line with %q in\n%s", containing, out)
	return ""
}

func TestRenderNamesTheStationAndTheTime(t *testing.T) {
	out := render(sampleBoard(), 100)

	head := line(t, out, "Departures from")
	if !strings.Contains(head, "Departures from Bergen (Mons)") || !strings.HasSuffix(head, "Sun 25 Oct 14:52") {
		t.Errorf("header = %q", head)
	}
	if !strings.Contains(out, "Live NMBS data via iRail, updated 14:52") {
		t.Errorf("no source line in\n%s", out)
	}
}

func TestRenderSpellsOutEveryDepartureState(t *testing.T) {
	out := render(sampleBoard(), 100)

	cases := map[string][]string{
		"Brugge":             {"14:47", "on time", "6 !", "L 13097", "low"},
		"Oostende":           {"14:49", "14:52 +3", "11", "medium"},
		"Antwerpen-Centraal": {"14:53", "15:02 +9", "IC 1814", "high"},
		"Kortrijk":           {"15:08", "cancelled", "L 885"},
		"Lokeren":            {"14:45", "departed"},
		"Brussels Airport":   {"15:15", "–", "IC 2215 extra"},
	}
	for destination, wants := range cases {
		row := line(t, out, destination)
		for _, want := range wants {
			if !strings.Contains(row, want) {
				t.Errorf("row %q lacks %q", row, want)
			}
		}
	}
}

func TestRenderExplainsOnlyTheMarksOnTheBoard(t *testing.T) {
	out := render(sampleBoard(), 100)
	if !strings.Contains(out, "+3 = 3 min late  ·  6 ! = platform changed") {
		t.Errorf("legend missing in\n%s", out)
	}

	calm := irail.Liveboard{Updated: at("14:52"), Departures: []irail.Departure{
		{Destination: "Gent-Sint-Pieters", Scheduled: at("15:00"), Platform: "3", Train: "IC 1"},
	}}
	if out := render(calm, 100); strings.Contains(out, "late") || strings.Contains(out, "platform changed") {
		t.Errorf("legend without anything to explain in\n%s", out)
	}
}

func TestRenderExplainsOnlyMarksThatAreDrawn(t *testing.T) {
	b := irail.Liveboard{Updated: at("14:52"), Departures: []irail.Departure{
		{Destination: "Lokeren", Scheduled: at("14:45"), Delay: 3 * time.Minute, Platform: "5", PlatformChanged: true, Departed: true, Train: "S53 664"},
		{Destination: "Kortrijk", Scheduled: at("15:08"), Delay: 6 * time.Minute, Platform: "8", Cancelled: true, Train: "L 885"},
		{Destination: "Genk", Scheduled: at("15:10"), Delay: 9 * time.Minute, Platform: "10", Train: "IC 1514"},
	}}

	out := render(b, 100)

	if !strings.Contains(out, "+9 = 9 min late") || strings.Contains(out, "+3 = ") || strings.Contains(out, "+6 = ") {
		t.Errorf("legend should explain the +9 on the board, not departed or cancelled trains:\n%s", out)
	}
	if strings.Contains(out, "platform changed") {
		t.Errorf("legend explains a platform mark that a departed train does not show:\n%s", out)
	}
}

func TestRenderLeavesNoWhitespaceOnlyLines(t *testing.T) {
	calm := irail.Liveboard{Updated: at("14:52"), Departures: []irail.Departure{
		{Destination: "Gent-Sint-Pieters", Scheduled: at("15:00"), Platform: "3", Train: "IC 1", Occupancy: "low"},
	}}
	for _, b := range []irail.Liveboard{calm, {Updated: at("02:10")}} {
		if out := render(b, 80); strings.Contains(out, "\n  \n") || strings.Contains(out, "\n\n\n") {
			t.Errorf("stray blank lines in\n%q", out)
		}
	}
}

func TestRenderLeavesOutTheCrowdColumnWithoutEstimates(t *testing.T) {
	b := irail.Liveboard{Updated: at("14:52"), Departures: []irail.Departure{
		{Destination: "Gent-Sint-Pieters", Scheduled: at("15:00"), Platform: "3", Train: "IC 1"},
	}}

	out := render(b, 100)

	if heading := line(t, out, "Platform"); strings.Contains(heading, "Crowd") || !strings.Contains(heading, "Train") {
		t.Errorf("heading = %q, want Train but no Crowd", heading)
	}
	if row := line(t, out, "IC 1"); strings.HasSuffix(row, " ") {
		t.Errorf("row ends in spaces: %q", row)
	}
}

func TestRenderShowsAnEarlyTrainAsOnTime(t *testing.T) {
	b := irail.Liveboard{Updated: at("14:52"), Departures: []irail.Departure{
		{Destination: "Leuven", Scheduled: at("15:13"), Delay: -time.Minute, Platform: "9", Train: "IC 3613"},
	}}

	out := render(b, 100)

	if row := line(t, out, "Leuven"); !strings.Contains(row, "on time") || strings.Contains(row, "+") {
		t.Errorf("early train row = %q", row)
	}
}

func TestRenderFitsNarrowTerminals(t *testing.T) {
	cases := []struct {
		width   int
		missing []string
	}{
		{100, nil},
		{70, []string{"Crowd"}},
		{52, []string{"Crowd", "Train"}},
	}
	for _, c := range cases {
		out := render(sampleBoard(), c.width)
		for _, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w > c.width {
				t.Errorf("width %d: line is %d wide: %q", c.width, w, l)
			}
		}
		heading := line(t, out, "Platform")
		for _, title := range c.missing {
			if strings.Contains(heading, title) {
				t.Errorf("width %d still shows %s: %q", c.width, title, heading)
			}
		}
	}
	if out := render(sampleBoard(), 52); !strings.Contains(out, "…") {
		t.Errorf("long destinations are not shortened at width 52:\n%s", out)
	}
}

func TestRenderFitsALongStationNameInTheHeader(t *testing.T) {
	cases := map[int]string{
		90: "Departures from Luik-Guillemins (Liège-Guillemins)",
		60: "Departures from Luik-Guillemins (Liège-Guillemins)",
		48: "Departures from Luik-Guillemins",
		24: "Departures from Lui…",
	}
	for width, want := range cases {
		o := Options{Station: "Luik-Guillemins", LocalName: "Liège-Guillemins", Width: width, Now: at("14:52"), Location: brussels}
		head := strings.Split(ansi.Strip(Render(sampleBoard(), o)), "\n")[0]
		if !strings.HasPrefix(head, "  "+want) || ansi.StringWidth(head) > width {
			t.Errorf("width %d: header %q, want it to start with %q and fit", width, head, want)
		}
		if hasClock := strings.HasSuffix(head, "14:52"); hasClock != (width == 90) {
			t.Errorf("width %d: header %q shows the clock: %v", width, head, hasClock)
		}
	}
}

func TestRenderSaysSoWhenNothingLeaves(t *testing.T) {
	out := render(irail.Liveboard{Updated: at("02:10")}, 80)

	if !strings.Contains(out, "No departures are listed right now.") {
		t.Errorf("empty board =\n%s", out)
	}
}
