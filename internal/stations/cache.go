package stations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PieterLuypaert/trainspot/internal/irail"
)

// MaxAge is how long a saved station list is used before it is fetched again.
const MaxAge = 7 * 24 * time.Hour

const cacheVersion = 1

type cacheFile struct {
	Version  int       `json:"version"`
	Fetched  time.Time `json:"fetched"`
	Stations []Station `json:"stations"`
}

type Fetcher interface {
	Stations(ctx context.Context, lang string) ([]irail.Station, error)
}

// Load returns every station, from the list saved at path while it is
// younger than MaxAge, else fresh from iRail in all four languages. When
// iRail cannot be reached, an older saved list beats none; warn hears about
// that and about a list that could not be saved.
func Load(ctx context.Context, f Fetcher, path string, now time.Time, warn io.Writer) (*Directory, error) {
	saved, savedErr := readCache(path)
	// A list from the future was saved while the clock ran ahead; its age
	// says nothing, so it is refreshed.
	if savedErr == nil && !saved.Fetched.After(now) && now.Sub(saved.Fetched) < MaxAge {
		return NewDirectory(saved.Stations), nil
	}

	fresh, err := fetchAll(ctx, f)
	if err != nil {
		if savedErr == nil {
			fmt.Fprintf(warn, "trainspot: using the station list from %s, could not refresh it: %v\n", saved.Fetched.Format("2 Jan 2006"), err)
			return NewDirectory(saved.Stations), nil
		}
		return nil, fmt.Errorf("could not load the station list: %w", err)
	}

	if err := writeCache(path, cacheFile{Version: cacheVersion, Fetched: now, Stations: fresh}); err != nil {
		fmt.Fprintf(warn, "trainspot: could not save the station list, so it will be fetched again next time: %v\n", err)
	}
	return NewDirectory(fresh), nil
}

func fetchAll(ctx context.Context, f Fetcher) ([]Station, error) {
	var all []Station
	index := map[string]int{}
	for _, lang := range irail.Languages {
		list, err := f.Stations(ctx, lang)
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			i, known := index[s.ID]
			if !known {
				i = len(all)
				index[s.ID] = i
				all = append(all, Station{ID: s.ID, Names: map[string]string{}, Standard: s.Standard})
			}
			all[i].Names[lang] = s.Name
		}
	}
	if len(all) == 0 {
		return nil, errors.New("iRail sent an empty station list")
	}
	return all, nil
}

func readCache(path string) (cacheFile, error) {
	var c cacheFile
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("%s is damaged: %w", path, err)
	}
	if c.Version != cacheVersion || len(c.Stations) == 0 {
		return c, fmt.Errorf("%s holds an older or empty list", path)
	}
	return c, nil
}

// writeCache writes through a temporary file, so a crash never leaves a
// half-written list behind.
func writeCache(path string, c cacheFile) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "stations-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
