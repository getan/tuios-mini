package applist

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFrecencyRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	f := LoadFrecency(path)
	f.Note("nvim")
	f.Note("nvim")
	want := f.Boost("nvim")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded := LoadFrecency(path)
	if got := reloaded.Boost("nvim"); got != want {
		t.Fatalf("Boost after reload = %d, want %d", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("history mode = %o, want 600", perm)
	}
}

func TestFrecencySurvivesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := LoadFrecency(path)
	if got := f.Boost("anything"); got != 0 {
		t.Fatalf("Boost = %d from a corrupt history, want 0", got)
	}
	f.Note("gcc")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	if f.Boost("gcc") == 0 {
		t.Error("a corrupt history must not stop a new one being recorded")
	}
}

func TestFrecencyIgnoresNewerVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	body := fmt.Sprintf(`{"version":%d,"apps":{"gcc":{"n":9,"t":%d}}}`, frecencyVersion+1, time.Now().Unix())
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadFrecency(path).Boost("gcc"); got != 0 {
		t.Fatalf("Boost = %d from a future format, want it left alone", got)
	}
}

// TestFrecencyPrunes keeps the file from growing without bound while holding on
// to the records a person would notice losing.
func TestFrecencyPrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	f := LoadFrecency(path)

	f.mu.Lock()
	old := time.Now().Add(-90 * 24 * time.Hour).Unix()
	for i := range maxRecords + 100 {
		f.recs[fmt.Sprintf("stale-%d", i)] = record{Count: 1, Last: old}
	}
	f.mu.Unlock()

	f.Note("favourite")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded := LoadFrecency(path)
	reloaded.mu.Lock()
	got := len(reloaded.recs)
	reloaded.mu.Unlock()
	if got > maxRecords {
		t.Fatalf("history kept %d records, want at most %d", got, maxRecords)
	}
	if reloaded.Boost("favourite") == 0 {
		t.Error("pruning dropped the most recently used entry")
	}
}

// TestDefaultPathIsScoped is the seam an embedding host uses: the launch
// history is one file under the state directory, and a host that runs a client
// inside its own tree points it there rather than at the person's.
//
// Two scopes one after another hold two histories, which is the case that
// matters: a workspace launching a program must not teach the next workspace's
// launcher, nor the person's own, that it was run.
func TestDefaultPathIsScoped(t *testing.T) {
	userPath := DefaultPath()
	if userPath == "" {
		t.Fatal("DefaultPath is empty")
	}

	first, second := t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		path := filepath.Join(dir, "launcher.json")
		restore := ScopeDefaultPath(path)
		if got := DefaultPath(); got != path {
			t.Errorf("inside the scope DefaultPath = %q, want %q", got, path)
		}
		f := LoadFrecency(DefaultPath())
		f.Note(filepath.Base(dir))
		if err := f.Save(); err != nil {
			t.Fatalf("save into %s: %v", dir, err)
		}
		restore()
		// The same close twice, as a host that also defers it makes.
		restore()
		if got := DefaultPath(); got != userPath {
			t.Errorf("after the scope DefaultPath = %q, want the process's own %q", got, userPath)
		}
	}

	for _, dir := range []string{first, second} {
		f := LoadFrecency(filepath.Join(dir, "launcher.json"))
		if f.Boost(filepath.Base(dir)) == 0 {
			t.Errorf("the history in %s does not hold its own launch", dir)
		}
		other := first
		if dir == first {
			other = second
		}
		if f.Boost(filepath.Base(other)) != 0 {
			t.Errorf("the history in %s holds the other workspace's launch", dir)
		}
	}
}
