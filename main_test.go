package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/PieterLuypaert/trainspot/internal/irail"
	"github.com/PieterLuypaert/trainspot/internal/stations"
)

var testStations = map[string]string{
	"nl": `{"station":[
		{"id":"BE.NMBS.008892007","name":"Gent-Sint-Pieters","standardname":"Gent-Sint-Pieters"},
		{"id":"BE.NMBS.008893120","name":"Gent-Dampoort","standardname":"Gent-Dampoort"},
		{"id":"BE.NMBS.008881000","name":"Bergen","standardname":"Mons"},
		{"id":"BE.NMBS.008891009","name":"Brugge","standardname":"Brugge"}]}`,
	"fr": `{"station":[
		{"id":"BE.NMBS.008892007","name":"Gand-Saint-Pierre","standardname":"Gent-Sint-Pieters"},
		{"id":"BE.NMBS.008893120","name":"Gand-Dampoort","standardname":"Gent-Dampoort"},
		{"id":"BE.NMBS.008881000","name":"Mons","standardname":"Mons"},
		{"id":"BE.NMBS.008891009","name":"Bruges","standardname":"Brugge"}]}`,
}

type fakeIRail struct {
	liveboards   []url.Values
	liveboardErr int
	journeys     []url.Values
}

func (f *fakeIRail) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	switch r.URL.Path {
	case "/stations/":
		list, ok := testStations[query.Get("lang")]
		if !ok {
			list = testStations["nl"]
		}
		fmt.Fprint(w, list)
	case "/liveboard/":
		f.liveboards = append(f.liveboards, query)
		if f.liveboardErr != 0 {
			w.WriteHeader(f.liveboardErr)
			return
		}
		fmt.Fprint(w, `{"station":"x","timestamp":"1792936000","departures":{"departure":[
			{"station":"Brugge","time":"1792936320","delay":"0","canceled":"0","left":"0","isExtra":"0",
			 "platform":"6","platforminfo":{"name":"6","normal":"1"},"vehicleinfo":{"shortname":"IC 535"},"occupancy":{"name":"low"}}]}}`)
	case "/connections/":
		f.journeys = append(f.journeys, query)
		fmt.Fprint(w, `{"connection":[{
			"departure":{"station":"Gent-Sint-Pieters","time":"1792936320","delay":"120","canceled":"0","walking":"0","vehicle":"BE.NMBS.IC535","vehicleinfo":{"shortname":"IC 535"},"platform":"11","direction":{"name":"Oostende"}},
			"arrival":{"station":"Brugge","time":"1792937820","delay":"0","canceled":"0","walking":"0","platform":"4"}}]}`)
	default:
		http.NotFound(w, r)
	}
}

type result struct {
	code     int
	out, err string
	iRail    *fakeIRail
}

func runWith(t *testing.T, args []string, input string, interactive bool, iRail *fakeIRail) result {
	t.Helper()
	server := httptest.NewServer(iRail)
	t.Cleanup(server.Close)
	client := irail.NewClient("trainspot-test")
	client.BaseURL = server.URL

	o, err := parseArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	a := app{
		client:      client,
		cachePath:   filepath.Join(t.TempDir(), "stations.json"),
		now:         func() time.Time { return time.Unix(1792936000, 0) },
		in:          strings.NewReader(input),
		out:         &out,
		errOut:      &errOut,
		interactive: interactive,
		width:       100,
	}
	code := a.run(context.Background(), o)
	return result{code, ansi.Strip(out.String()), ansi.Strip(errOut.String()), iRail}
}

func TestShowsTheDeparturesFromAStation(t *testing.T) {
	r := runWith(t, []string{"gent", "sint", "pieters"}, "", true, &fakeIRail{})

	if r.code != 0 || !strings.Contains(r.out, "Departures from Gent-Sint-Pieters") || !strings.Contains(r.out, "Brugge") {
		t.Fatalf("code %d, out:\n%s\nerr:\n%s", r.code, r.out, r.err)
	}
	if got := r.iRail.liveboards[0]; got.Get("id") != "BE.NMBS.008892007" || got.Get("lang") != "nl" {
		t.Errorf("liveboard asked for %v", got)
	}
}

func TestShowsTheJourneysBetweenTwoStations(t *testing.T) {
	r := runWith(t, []string{"gent", "sint", "pieters", "--to", "brugge"}, "", true, &fakeIRail{})

	if r.code != 0 || len(r.iRail.liveboards) != 0 {
		t.Fatalf("code %d, liveboards %v, err:\n%s", r.code, r.iRail.liveboards, r.err)
	}
	for _, want := range []string{"Gent-Sint-Pieters → Brugge", "14:52 +2 → 15:17   23 min   direct", "IC 535 to Oostende"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("no %q in\n%s", want, r.out)
		}
	}
	if got := r.iRail.journeys[0]; got.Get("from") != "BE.NMBS.008892007" || got.Get("to") != "BE.NMBS.008891009" {
		t.Errorf("journeys asked for %v", got)
	}
}

func TestRefusesAJourneyToTheSameStation(t *testing.T) {
	r := runWith(t, []string{"bergen", "--to", "mons"}, "", true, &fakeIRail{})

	if r.code != 2 || !strings.Contains(r.err, "Bergen is both where the journey starts and ends") || len(r.iRail.journeys) != 0 {
		t.Errorf("code %d, err:\n%s", r.code, r.err)
	}
}

func TestNamesStationsInTheChosenLanguage(t *testing.T) {
	r := runWith(t, []string{"bergen", "--lang", "fr"}, "", true, &fakeIRail{})

	if r.code != 0 || !strings.Contains(r.out, "Departures from Mons") {
		t.Fatalf("code %d, out:\n%s\nerr:\n%s", r.code, r.out, r.err)
	}
	if got := r.iRail.liveboards[0].Get("lang"); got != "fr" {
		t.Errorf("liveboard lang = %q", got)
	}
}

func TestAsksWhichStationWhenANameFitsSeveral(t *testing.T) {
	r := runWith(t, []string{"gent"}, "9\nsint pieters\n2\n", true, &fakeIRail{})

	if r.code != 0 || !strings.Contains(r.out, "Departures from Gent-Sint-Pieters") {
		t.Fatalf("code %d, out:\n%s\nerr:\n%s", r.code, r.out, r.err)
	}
	for _, want := range []string{`  "gent" fits 2 stations:`, "   1  Gent-Dampoort\n", "   2  Gent-Sint-Pieters\n"} {
		if !strings.Contains(r.err, want) {
			t.Errorf("picker lacks %q:\n%s", want, r.err)
		}
	}
	if asked := strings.Count(r.err, "Which one?"); asked != 3 {
		t.Errorf("asked %d times, want 3 for two wrong answers and a right one", asked)
	}
}

func TestStopsWhenNoAnswerComes(t *testing.T) {
	r := runWith(t, []string{"gent"}, "", true, &fakeIRail{})

	if r.code != 2 || !strings.Contains(r.err, "no station chosen") || len(r.iRail.liveboards) != 0 {
		t.Errorf("code %d, err:\n%s", r.code, r.err)
	}
}

func TestListsTheChoicesWhenNobodyCanAnswer(t *testing.T) {
	r := runWith(t, []string{"gent"}, "", false, &fakeIRail{})

	if r.code != 2 || len(r.iRail.liveboards) != 0 {
		t.Fatalf("code %d, liveboards %v", r.code, r.iRail.liveboards)
	}
	want := `trainspot: "gent" fits 2 stations; type more of the name, such as:
  Gent-Dampoort
  Gent-Sint-Pieters
`
	if r.err != want {
		t.Errorf("err =\n%s\nwant\n%s", r.err, want)
	}
}

func TestSuggestsAStationForATypo(t *testing.T) {
	r := runWith(t, []string{"brugee"}, "", true, &fakeIRail{})

	if r.code != 2 || !strings.Contains(r.err, `no station is called "brugee"; did you mean Brugge?`) {
		t.Errorf("code %d, err:\n%s", r.code, r.err)
	}
}

func TestReportsTroubleAtIRail(t *testing.T) {
	r := runWith(t, []string{"brugge"}, "", true, &fakeIRail{liveboardErr: http.StatusServiceUnavailable})

	if r.code != 1 || !strings.Contains(r.err, "could not get the departures from Brugge") || !strings.Contains(r.err, "status.irail.be") {
		t.Errorf("code %d, err:\n%s", r.code, r.err)
	}
}

func TestPickerTellsLookAlikesApart(t *testing.T) {
	bergen := stations.Station{ID: "BE.NMBS.008881000", Names: map[string]string{"nl": "Bergen", "fr": "Mons"}, Standard: "Mons"}
	aachen := stations.Station{ID: "BE.NMBS.008015345", Names: map[string]string{"nl": "Aken Hbf"}, Standard: "Aachen Hbf"}

	if got := ansi.Strip(describe(bergen, "nl")); got != "  Mons" {
		t.Errorf("describe(Bergen) = %q", got)
	}
	if got := ansi.Strip(describe(aachen, "nl")); got != "  Aachen Hbf · abroad" {
		t.Errorf("describe(Aken) = %q", got)
	}
}

func TestCachePathFollowsXDG(t *testing.T) {
	cases := []struct{ xdg, home, want string }{
		{"/var/cache/me", "/Users/me", "/var/cache/me/trainspot/stations.json"},
		{"", "/Users/me", "/Users/me/.cache/trainspot/stations.json"},
		{"relative/cache", "/Users/me", "/Users/me/.cache/trainspot/stations.json"},
		{"", "", filepath.Join(os.TempDir(), "trainspot", "stations.json")},
	}
	for _, c := range cases {
		t.Setenv("XDG_CACHE_HOME", c.xdg)
		t.Setenv("HOME", c.home)
		if got := cachePath(); got != c.want {
			t.Errorf("XDG_CACHE_HOME=%q HOME=%q: cachePath() = %q, want %q", c.xdg, c.home, got, c.want)
		}
	}
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		args    []string
		want    options
		wantErr string
	}{
		{[]string{"gent"}, options{query: "gent", lang: "nl"}, ""},
		{[]string{"--lang", "fr", "liège", "guillemins"}, options{query: "liège guillemins", lang: "fr"}, ""},
		{[]string{"luik", "--no-color", "guillemins", "-lang=de"}, options{query: "luik guillemins", lang: "de", noColor: true}, ""},
		{[]string{"--lang", "fr", "--", "gent", "--lang", "de"}, options{query: "gent --lang de", lang: "fr"}, ""},
		{[]string{"gent", "--to", "brussel", "zuid", "--lang", "fr"}, options{query: "gent", to: "brussel zuid", lang: "fr"}, ""},
		{[]string{"--no-color", "luik", "-to=namen", "--", "-x"}, options{query: "luik", to: "namen -x", lang: "nl", noColor: true}, ""},
		{[]string{"gent", "--to"}, options{}, "--to needs the name of a station"},
		{[]string{"-h"}, options{lang: "nl", help: true}, ""},
		{[]string{"--version"}, options{lang: "nl", version: true}, ""},
		{[]string{"gent", "--lang", "es"}, options{}, `--lang takes nl, fr, en or de, not "es"`},
		{[]string{"--fast"}, options{}, "flag provided but not defined: -fast"},
	}
	for _, c := range cases {
		got, err := parseArgs(c.args)
		if c.wantErr != "" {
			if err == nil || err.Error() != c.wantErr {
				t.Errorf("parseArgs(%q) error = %v, want %q", c.args, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseArgs(%q) = %+v, %v, want %+v", c.args, got, err, c.want)
		}
	}
}
