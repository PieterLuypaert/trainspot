package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/PieterLuypaert/trainspot/internal/files"
)

// LastTrip is the trip the screen opens on next time, as two station ids.
type LastTrip struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// LoadLastTrip reads the trip saved at path, or an empty one when none
// was saved yet.
func LoadLastTrip(path string) (LastTrip, error) {
	var t LastTrip
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(data, &t); err != nil {
		return LastTrip{}, fmt.Errorf("%s is damaged: %w", path, err)
	}
	return t, nil
}

func SaveLastTrip(path string, t LastTrip) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return files.WriteAtomic(path, data)
}
