package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastTripIsSavedAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trainspot", "trip.json")

	if got, err := LoadLastTrip(path); err != nil || got != (LastTrip{}) {
		t.Fatalf("before saving: %+v, %v", got, err)
	}
	want := LastTrip{From: gentID, To: aarlenID}
	if err := SaveLastTrip(path, want); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadLastTrip(path); err != nil || got != want {
		t.Errorf("read back %+v, %v", got, err)
	}
}

func TestADamagedLastTripSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trip.json")
	if err := os.WriteFile(path, []byte("{half"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadLastTrip(path)

	if err == nil || !strings.Contains(err.Error(), "is damaged") || got != (LastTrip{}) {
		t.Errorf("got %+v, %v", got, err)
	}
}
