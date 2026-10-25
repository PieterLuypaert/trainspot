package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"github.com/PieterLuypaert/trainspot/internal/files"
)

// keptTrips is how many recent trips the screen keeps.
const keptTrips = 5

// Trip is a trip as two station ids.
type Trip struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Trips are the recent trips, the last one first. The screen opens on the
// first and lists them all on the form.
type Trips []Trip

// With puts a trip first, once, and keeps the most recent keptTrips.
func (t Trips) With(trip Trip) Trips {
	rest := slices.DeleteFunc(slices.Clone(t), func(other Trip) bool { return other == trip })
	return append(Trips{trip}, rest...)[:min(keptTrips, len(rest)+1)]
}

type tripsFile struct {
	Recent Trips `json:"recent"`
	// From and To are how a single last trip was saved before.
	Trip
}

// LoadTrips reads the trips saved at path, none when nothing was saved
// yet; a file from before, with one trip, reads as that trip.
func LoadTrips(path string) (Trips, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f tripsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", path, err)
	}
	if len(f.Recent) == 0 && f.From != "" && f.To != "" {
		return Trips{f.Trip}, nil
	}
	return f.Recent, nil
}

func SaveTrips(path string, t Trips) error {
	data, err := json.Marshal(struct {
		Recent Trips `json:"recent"`
	}{t})
	if err != nil {
		return err
	}
	return files.WriteAtomic(path, data)
}
