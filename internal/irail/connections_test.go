package irail

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"
)

func TestJourneysAsksForBothStationsByID(t *testing.T) {
	client, seen := serveFile(t, "testdata/connections-gent-sint-pieters-aarlen.json")

	if _, err := client.Journeys(context.Background(), "BE.NMBS.008892007", "BE.NMBS.008866001", "fr"); err != nil {
		t.Fatal(err)
	}

	if seen.URL.Path != "/connections/" {
		t.Errorf("path = %q", seen.URL.Path)
	}
	query := seen.URL.Query()
	for key, want := range map[string]string{"from": "BE.NMBS.008892007", "to": "BE.NMBS.008866001", "lang": "fr", "timesel": "departure", "format": "json"} {
		if got := query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestJourneysReadARecordedAnswer(t *testing.T) {
	client, _ := serveFile(t, "testdata/connections-gent-sint-pieters-aarlen.json")

	journeys, err := client.Journeys(context.Background(), "BE.NMBS.008892007", "BE.NMBS.008866001", "nl")
	if err != nil {
		t.Fatal(err)
	}

	if len(journeys) != 6 {
		t.Fatalf("got %d journeys", len(journeys))
	}
	first := journeys[0]
	if len(first.Legs) != 2 {
		t.Fatalf("first journey has %d legs", len(first.Legs))
	}
	toBrussels, toArlon := first.Legs[0], first.Legs[1]
	if toBrussels.Train != "IC 3614" || toBrussels.Direction != "Leuven" || toBrussels.Departure.Station != "Gent-Sint-Pieters" || toBrussels.Arrival.Station != "Brussel-Zuid" {
		t.Errorf("first leg = %+v", toBrussels)
	}
	if toBrussels.Departure.Delay != 5*time.Minute || toBrussels.Departure.Platform != "6" || !toBrussels.Departure.PlatformChanged {
		t.Errorf("first departure = %+v", toBrussels.Departure)
	}
	if toArlon.Train != "IC 2117" || toArlon.Departure.Platform != "15" || toArlon.Arrival.Station != "Aarlen" || toArlon.Arrival.Delay != time.Minute {
		t.Errorf("second leg = %+v", toArlon)
	}
	if !first.Arrival().Scheduled.Equal(time.Unix(1792956120, 0)) {
		t.Errorf("arrival = %v", first.Arrival().Scheduled)
	}
	if got := len(journeys[4].Legs); got != 3 {
		t.Errorf("fifth journey has %d legs, want 3 (via Luik-Guillemins and Namen)", got)
	}
}

func TestJourneysReadWalksAndCancelledTrains(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"connection":[{
			"departure":{"station":"Haren-Zuid","time":"1792940000","delay":"0","canceled":"0","walking":"0","vehicle":"BE.NMBS.S23787","vehicleinfo":{"shortname":"S2 3787"},"platform":"1","direction":{"name":"Brussel-Zuid"}},
			"vias":{"number":"1","via":[{
				"arrival":{"station":"Haren-Zuid","time":"1792940300","delay":"0","canceled":"0","walking":"0","platform":"2"},
				"departure":{"station":"Haren-Zuid","time":"1792940300","delay":"0","canceled":"0","walking":"1","vehicle":"BE.NMBS.WALK","vehicleinfo":{"shortname":"WALK"},"platform":"?"}}]},
			"arrival":{"station":"Haren","time":"1792940900","delay":"0","canceled":"1","walking":"1","platform":"?"}
		}]}`))
	})

	journeys, err := client.Journeys(context.Background(), "a", "b", "nl")
	if err != nil {
		t.Fatal(err)
	}

	walk := journeys[0].Legs[1]
	if !walk.Walking || walk.Train != "" || walk.Direction != "" || walk.Departure.Platform != "" {
		t.Errorf("walk = %+v", walk)
	}
	if !journeys[0].Cancelled() {
		t.Error("a journey with a cancelled stop is not cancelled")
	}
}

func at(base time.Time, minutes int) time.Time {
	return base.Add(time.Duration(minutes) * time.Minute)
}

func TestChangesCountTheDelays(t *testing.T) {
	base := time.Unix(1792940000, 0)
	leg := func(from, to string, dep, depDelay, arr, arrDelay int) Leg {
		return Leg{
			Train:     "IC 1",
			Departure: Stop{Station: from, Scheduled: at(base, dep), Delay: time.Duration(depDelay) * time.Minute},
			Arrival:   Stop{Station: to, Scheduled: at(base, arr), Delay: time.Duration(arrDelay) * time.Minute},
		}
	}
	cases := []struct {
		name          string
		journey       Journey
		planned, real time.Duration
		tight, missed bool
	}{
		{"comfortable", Journey{[]Leg{leg("A", "B", 0, 0, 30, 0), leg("B", "C", 36, 0, 60, 0)}}, 6 * time.Minute, 6 * time.Minute, false, false},
		{"tight", Journey{[]Leg{leg("A", "B", 0, 4, 30, 4), leg("B", "C", 36, 0, 60, 0)}}, 6 * time.Minute, 2 * time.Minute, true, false},
		{"missed", Journey{[]Leg{leg("A", "B", 0, 9, 30, 9), leg("B", "C", 36, 0, 60, 0)}}, 6 * time.Minute, -3 * time.Minute, false, true},
		{"next train late too", Journey{[]Leg{leg("A", "B", 0, 9, 30, 9), leg("B", "C", 36, 10, 60, 10)}}, 6 * time.Minute, 7 * time.Minute, false, false},
	}
	for _, c := range cases {
		changes := c.journey.Changes()
		if len(changes) != 1 {
			t.Fatalf("%s: %d changes", c.name, len(changes))
		}
		got := changes[0]
		if got.Planned != c.planned || got.Real != c.real || got.Tight() != c.tight || got.Missed() != c.missed {
			t.Errorf("%s: change = %+v, tight %v, missed %v", c.name, got, got.Tight(), got.Missed())
		}
	}
}

func TestAWalkBelongsToTheChangeAfterIt(t *testing.T) {
	base := time.Unix(1792940000, 0)
	train := func(from, to string, dep, arr, arrDelay int) Leg {
		return Leg{Train: "S2 1", Departure: Stop{Station: from, Scheduled: at(base, dep)}, Arrival: Stop{Station: to, Scheduled: at(base, arr), Delay: time.Duration(arrDelay) * time.Minute}}
	}
	walk := Leg{Walking: true, Departure: Stop{Station: "Haren-Zuid", Scheduled: at(base, 20)}, Arrival: Stop{Station: "Haren", Scheduled: at(base, 30)}}

	cases := []struct {
		name    string
		journey Journey
		want    []Change
	}{
		{"walk between trains", Journey{[]Leg{train("A", "Haren-Zuid", 0, 20, 4), walk, train("Haren", "C", 36, 60, 0)}},
			[]Change{{Planned: 6 * time.Minute, Real: 2 * time.Minute}}},
		{"walk to the destination", Journey{[]Leg{train("A", "Haren-Zuid", 0, 20, 4), walk}}, nil},
		{"walk from the start", Journey{[]Leg{walk, train("Haren", "C", 36, 60, 0)}},
			[]Change{{Planned: 6 * time.Minute, Real: 6 * time.Minute}}},
	}
	for _, c := range cases {
		if got := c.journey.Changes(); !slices.Equal(got, c.want) {
			t.Errorf("%s: Changes() = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestDurationCountsTheDelays(t *testing.T) {
	base := time.Unix(1792940000, 0)
	j := Journey{[]Leg{{
		Departure: Stop{Scheduled: base, Delay: 5 * time.Minute},
		Arrival:   Stop{Scheduled: at(base, 60), Delay: 2 * time.Minute},
	}}}

	if got := j.Duration(); got != 57*time.Minute {
		t.Errorf("Duration() = %v, want 57m", got)
	}
}
