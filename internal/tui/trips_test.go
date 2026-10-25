package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTripsAreSavedAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trainspot", "trip.json")

	if got, err := LoadTrips(path); err != nil || len(got) != 0 {
		t.Fatalf("before saving: %+v, %v", got, err)
	}
	want := Trips{{From: gentID, To: aarlenID}, {From: brusselID, To: gentID}}
	if err := SaveTrips(path, want); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadTrips(path); err != nil || !slices.Equal(got, want) {
		t.Errorf("read back %+v, %v", got, err)
	}
}

func TestATripFileFromBeforeReadsAsOneTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trip.json")
	if err := os.WriteFile(path, []byte(`{"from":"`+gentID+`","to":"`+aarlenID+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadTrips(path)

	if err != nil || !slices.Equal(got, Trips{{From: gentID, To: aarlenID}}) {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestADamagedTripFileSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trip.json")
	if err := os.WriteFile(path, []byte("{half"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := LoadTrips(path); err == nil || !strings.Contains(err.Error(), "is damaged") || got != nil {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestWithPutsATripFirstOnceAndKeepsFive(t *testing.T) {
	trip := func(n int) Trip { return Trip{From: string(rune('a' + n)), To: "z"} }
	var trips Trips
	for n := range 7 {
		trips = trips.With(trip(n))
	}
	trips = trips.With(trip(4))

	want := Trips{trip(4), trip(6), trip(5), trip(3), trip(2)}
	if !slices.Equal(trips, want) {
		t.Errorf("trips = %+v, want %+v", trips, want)
	}
}
