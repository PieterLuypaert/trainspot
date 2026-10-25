package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicReplacesTheFileAndLeavesNothingElse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "trainspot")
	path := filepath.Join(dir, "trip.json")

	for _, content := range []string{"first", "second"} {
		if err := WriteAtomic(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if got, err := os.ReadFile(path); err != nil || string(got) != "second" {
		t.Errorf("file holds %q, %v", got, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the folder holds %d entries, want only the file", len(entries))
	}
}
