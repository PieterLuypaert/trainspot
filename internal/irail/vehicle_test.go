package irail

import (
	"context"
	"testing"
	"time"
)

func TestRouteAsksForTheTrainOnItsDay(t *testing.T) {
	client, seen := serveFile(t, "testdata/vehicle-ic-2117.json")
	brussels, _ := time.LoadLocation("Europe/Brussels")

	if _, err := client.Route(context.Background(), "BE.NMBS.IC2117", time.Date(2026, 10, 26, 0, 10, 0, 0, brussels), "fr"); err != nil {
		t.Fatal(err)
	}

	query := seen.URL.Query()
	for key, want := range map[string]string{"id": "BE.NMBS.IC2117", "date": "261026", "lang": "fr", "format": "json"} {
		if got := query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if seen.URL.Path != "/vehicle/" {
		t.Errorf("path = %q", seen.URL.Path)
	}
}

// The IC 2117 was recorded on 2026-10-25 between Leuven and Ottignies.
func recordedRoute(t *testing.T) Route {
	t.Helper()
	client, _ := serveFile(t, "testdata/vehicle-ic-2117.json")
	route, err := client.Route(context.Background(), "BE.NMBS.IC2117", time.Unix(1792947843, 0), "nl")
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func TestRouteReadsWhereTheTrainIs(t *testing.T) {
	route := recordedRoute(t)

	if route.Train != "IC 2117" || len(route.Stops) != 14 || !route.Updated.Equal(time.Unix(1792947843, 0)) {
		t.Fatalf("route = %s with %d stops at %v", route.Train, len(route.Stops), route.Updated)
	}
	leuven, ottignies := route.Stops[3], route.Stops[4]
	if !leuven.Passed || !leuven.Arrived || leuven.ArrivalDelay != 3*time.Minute || leuven.Platform != "6" {
		t.Errorf("Leuven = %+v", leuven)
	}
	if ottignies.Passed || ottignies.Arrived {
		t.Errorf("Ottignies = %+v", ottignies)
	}
	if last := route.Stops[13]; last.Station != "Luxemburg" || last.Platform != "" {
		t.Errorf("last stop = %+v", last)
	}
}

func TestFollowBringsALegUpToDate(t *testing.T) {
	route := recordedRoute(t)
	leg := Leg{Train: "IC 2117", Vehicle: "BE.NMBS.IC2117",
		Departure: Stop{Station: "Brussel-Zuid", Scheduled: time.Unix(1792944780, 0)},
		Arrival:   Stop{Station: "Aarlen", Scheduled: time.Unix(1792956120, 0)}}

	got := leg.Follow(route)

	if !got.Departure.Passed || got.Departure.Delay != 3*time.Minute || got.Departure.Platform != "15" {
		t.Errorf("departure = %+v", got.Departure)
	}
	if len(got.Calls) != 11 || got.Calls[0].Station != "Brussel-Centraal" || got.Calls[10].Station != "Marbehan" {
		t.Errorf("calls = %d, from %s to %s", len(got.Calls), got.Calls[0].Station, got.Calls[len(got.Calls)-1].Station)
	}
	if got.Arrival.Station != "Aarlen" || got.Arrival.Delay != time.Minute || got.Arrival.Passed {
		t.Errorf("arrival = %+v", got.Arrival)
	}
	if got.Train != "IC 2117" || got.Vehicle != "BE.NMBS.IC2117" {
		t.Errorf("Follow changed the train: %+v", got)
	}
}

func TestFollowLeavesALegOffTheRouteAlone(t *testing.T) {
	route := recordedRoute(t)
	leg := Leg{Departure: Stop{Station: "Gent-Sint-Pieters"}, Arrival: Stop{Station: "Aarlen"}}

	if got := leg.Follow(route); got.Departure != leg.Departure || got.Calls != nil || got.Arrival != leg.Arrival {
		t.Errorf("Follow changed a leg that is not on the route: %+v", got)
	}
}
