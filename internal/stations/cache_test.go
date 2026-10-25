package stations

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PieterLuypaert/trainspot/internal/irail"
)

type fakeFetcher struct {
	calls []string
	err   error
}

func (f *fakeFetcher) Stations(_ context.Context, lang string) ([]irail.Station, error) {
	f.calls = append(f.calls, lang)
	if f.err != nil {
		return nil, f.err
	}
	names := map[string]string{"nl": "Bergen", "fr": "Mons", "en": "Mons", "de": "Mons"}
	return []irail.Station{{ID: "BE.NMBS.008881000", Name: names[lang], Standard: "Mons"}}, nil
}

var now = time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)

func save(t *testing.T, path string, fetched time.Time) {
	t.Helper()
	stations := []Station{{ID: "BE.NMBS.008892007", Names: map[string]string{"nl": "Gent-Sint-Pieters"}, Standard: "Gent-Sint-Pieters"}}
	if err := writeCache(path, cacheFile{Version: cacheVersion, Fetched: fetched, Stations: stations}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFetchesAllFourLanguagesAndSavesThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trainspot", "stations.json")
	f := &fakeFetcher{}

	d, err := Load(context.Background(), f, path, now, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(f.calls, ",") != "nl,fr,en,de" {
		t.Errorf("fetched %v", f.calls)
	}
	if matches, sure := d.Lookup("mons", "nl"); !sure || matches[0].Name("nl") != "Bergen" {
		t.Errorf("merged names do not find Bergen as mons: %v", names(matches))
	}
	saved, err := readCache(path)
	if err != nil || !saved.Fetched.Equal(now) || saved.Stations[0].Names["fr"] != "Mons" {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

func TestLoadUsesASavedListForAWeek(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stations.json")
	save(t, path, now.Add(-MaxAge+time.Hour))
	f := &fakeFetcher{}

	d, err := Load(context.Background(), f, path, now, &bytes.Buffer{})

	if err != nil || len(f.calls) != 0 || d.Len() != 1 {
		t.Errorf("err %v, fetched %v, %d stations", err, f.calls, d.Len())
	}
}

func TestLoadRefreshesAnOldList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stations.json")
	save(t, path, now.Add(-MaxAge-time.Hour))
	f := &fakeFetcher{}

	d, err := Load(context.Background(), f, path, now, &bytes.Buffer{})

	if err != nil || len(f.calls) != 4 {
		t.Fatalf("err %v, fetched %v", err, f.calls)
	}
	if _, sure := d.Lookup("bergen", "nl"); !sure {
		t.Error("refreshed list does not have Bergen")
	}
}

func TestLoadRefreshesAListSavedWhileTheClockRanAhead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stations.json")
	save(t, path, now.AddDate(1, 0, 0))
	f := &fakeFetcher{}

	if _, err := Load(context.Background(), f, path, now, &bytes.Buffer{}); err != nil || len(f.calls) != 4 {
		t.Errorf("err %v, fetched %v", err, f.calls)
	}
}

func TestLoadFallsBackToAnOldListWhenOffline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stations.json")
	save(t, path, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	warn := &bytes.Buffer{}

	d, err := Load(context.Background(), &fakeFetcher{err: errors.New("no network")}, path, now, warn)

	if err != nil || d.Len() != 1 {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(warn.String(), "1 Sep 2026") || !strings.Contains(warn.String(), "no network") {
		t.Errorf("warning = %q", warn.String())
	}
}

func TestLoadExplainsWhenThereIsNoListAtAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stations.json")

	_, err := Load(context.Background(), &fakeFetcher{err: errors.New("no network")}, path, now, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "could not load the station list: no network") {
		t.Errorf("error = %v", err)
	}
}

func TestLoadReplacesADamagedList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stations.json")
	if err := os.WriteFile(path, []byte("{half a fi"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{}

	if _, err := Load(context.Background(), f, path, now, &bytes.Buffer{}); err != nil || len(f.calls) != 4 {
		t.Fatalf("err %v, fetched %v", err, f.calls)
	}
	if _, err := readCache(path); err != nil {
		t.Errorf("list was not replaced: %v", err)
	}
}

func TestLoadWarnsWhenTheListCannotBeSaved(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	warn := &bytes.Buffer{}

	_, err := Load(context.Background(), &fakeFetcher{}, filepath.Join(blocker, "stations.json"), now, warn)

	if err != nil || !strings.Contains(warn.String(), "could not save the station list") {
		t.Errorf("err %v, warning %q", err, warn.String())
	}
}
