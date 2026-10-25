package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adrg/xdg"

	"github.com/getan/tuios-mini/internal/app"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/debuglog"
	"github.com/getan/tuios-mini/internal/theme"
	"github.com/getan/tuios-mini/pkg/applist"
)

// These are the checks that a run leaves the person's tree alone and takes only
// what its host gave it: the state directory, the asset directories, the log
// stream and the process-wide tables that go with each. The whole-run proof is
// in pty_isolation_test.go, which needs a terminal; these run everywhere.

// TestStateDirIsDerivedFromTheConfigPathNotFromTheUser: a host that named only a
// config file still gets a private tree. The one answer this must never give is
// the user's state directory.
func TestStateDirIsDerivedFromTheConfigPathNotFromTheUser(t *testing.T) {
	dir := t.TempDir()
	opts := Options{ConfigPath: filepath.Join(dir, "config.toml")}
	got, err := opts.stateDir()
	if err != nil {
		t.Fatalf("stateDir: %v", err)
	}
	if want := filepath.Join(dir, "state"); got != want {
		t.Errorf("stateDir = %q, want %q", got, want)
	}
	userState := filepath.Join(xdg.StateHome, "tuios")
	if strings.HasPrefix(got, userState) {
		t.Errorf("the derived state directory %q is inside the user's own %q", got, userState)
	}

	named := filepath.Join(dir, "client")
	opts.StateDir = named
	if got, err := opts.stateDir(); err != nil || got != named {
		t.Errorf("stateDir with Options.StateDir = %q, %v; want %q", got, err, named)
	}
}

// TestRunScopesEverythingAndPutsItBack is the failure path a host hits most
// often — a daemon that is not there — and the contract on it: every scope is
// closed, the appearance globals and the log stream are as they were, and the
// runner's own flag is clear for the next run.
func TestRunScopesEverythingAndPutsItBack(t *testing.T) {
	fakeTerminal(t)
	opts := validOptions(t)
	opts.StateDir = filepath.Join(t.TempDir(), "client")

	beforeSettings := config.Global
	beforeLog := log.Writer()
	beforeTheme, beforeGlyphs := theme.CurrentThemeID(), theme.ActiveGlyphSetID()
	beforeLauncher := applist.DefaultPath()

	err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("Run attached to a socket nothing listens on")
	}

	if _, ok := config.ScopedConfigPath(); ok {
		t.Error("the config path scope was left open after Run returned")
	}
	if _, _, ok := theme.ScopedAssetDirs(); ok {
		t.Error("the theme asset scope was left open after Run returned")
	}
	if _, ok := app.ScopedClientState(); ok {
		t.Error("the client state scope was left open after Run returned")
	}
	if log.Writer() != beforeLog {
		t.Error("the log stream was left diverted after Run returned")
	}
	if !reflect.DeepEqual(config.Global, beforeSettings) {
		t.Error("the appearance globals were left changed after Run returned")
	}
	if got := theme.CurrentThemeID(); got != beforeTheme {
		t.Errorf("the theme in force after Run is %q, want %q", got, beforeTheme)
	}
	if got := theme.ActiveGlyphSetID(); got != beforeGlyphs {
		t.Errorf("the glyph set in force after Run is %q, want %q", got, beforeGlyphs)
	}
	if got := applist.DefaultPath(); got != beforeLauncher {
		t.Errorf("the launcher history path after Run is %q, want %q", got, beforeLauncher)
	}
	if running.Load() {
		t.Error("the runner flag was left set after Run returned")
	}

	// The run made its own tree and put what it keeps there.
	if _, err := os.Stat(opts.ConfigPath); err != nil {
		t.Errorf("the client did not create its own config: %v", err)
	}
	if _, err := os.Stat(opts.StateDir); err != nil {
		t.Errorf("the client did not create its own state directory: %v", err)
	}
}

// TestTwoRunsInARowShareNothing: a host that runs one project and then another
// is the case the scopes exist for. Each run reads its own config, keeps its
// own state, and neither sees the other's custom theme.
func TestTwoRunsInARowShareNothing(t *testing.T) {
	fakeTerminal(t)

	root := t.TempDir()
	first, second := filepath.Join(root, "one"), filepath.Join(root, "two")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		// A custom theme in each project's own tree, by the id of the other, so
		// a leaked registry would be visible as the wrong colours rather than as
		// a missing one.
		writeProjectTheme(t, dir, "shared")
	}

	for i, dir := range []string{first, second} {
		opts := Options{
			Socket:     filepath.Join(dir, "no-daemon.sock"),
			Session:    "work",
			ConfigPath: filepath.Join(dir, "config.toml"),
			StateDir:   filepath.Join(dir, "client"),
			Width:      100,
			Height:     30,
		}
		// The first run: the custom theme it wrote is visible inside the run and
		// gone after it.
		err := Run(context.Background(), opts)
		if err == nil {
			t.Fatalf("run %d attached to a socket nothing listens on", i)
		}
		if theme.Exists("shared") {
			t.Errorf("run %d left its custom theme in the process", i)
		}
		if _, err := os.Stat(filepath.Join(dir, "client", "themes", "shared.json")); err != nil {
			t.Errorf("run %d did not read the theme in its own tree: %v", i, err)
		}
	}
}

// TestRunWritesNothingIntoTheUsersTuiosTree is the audit in one check: seed a
// person's tuios directories with sentinel files, run a client that cannot
// attach, and require the tree to be byte for byte what it was.
//
// The sentinels are the files this client is known to read and write: its
// config, its sidebar, its launcher history, its custom theme and glyph set.
// A run that reached any of them fails here rather than on somebody's machine.
func TestRunWritesNothingIntoTheUsersTuiosTree(t *testing.T) {
	fakeTerminal(t)

	userConfig := filepath.Join(xdg.ConfigHome, "tuios")
	userState := filepath.Join(xdg.StateHome, "tuios")
	writeFile(t, filepath.Join(userConfig, "config.toml"), "[appearance]\ntheme = \"theirs\"\n")
	writeFile(t, filepath.Join(userConfig, "themes", "theirs.json"),
		`{"id":"theirs","fg":"#ffffff","bg":"#000000"}`)
	writeFile(t, filepath.Join(userConfig, "glyphs", "theirset.json"), `{"bullet":"T"}`)
	writeFile(t, filepath.Join(userState, "sidebar.json"), `{"order":["theirs"]}`)
	writeFile(t, filepath.Join(userState, "launcher.json"), `{"version":1,"apps":{"theirs":{"n":3,"t":1}}}`)

	before := snapshot(t, xdg.ConfigHome, xdg.StateHome, xdg.DataHome, xdg.CacheHome)

	opts := validOptions(t)
	opts.StateDir = filepath.Join(t.TempDir(), "client")
	if err := Run(context.Background(), opts); err == nil {
		t.Fatal("Run attached to a socket nothing listens on")
	}

	after := snapshot(t, xdg.ConfigHome, xdg.StateHome, xdg.DataHome, xdg.CacheHome)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("a run changed the person's own tuios tree:\n%s", diff(before, after))
	}
}

// TestDebugOnWritesTheRunsOwnLog: with Debug on, the client's log lines and the
// internal debug log go into the run's own tree — not into /tmp/tuios-debug.log,
// which is one file for every workspace on the machine — and the switch is not
// latched, so the host after the run is not left logging.
func TestDebugOnWritesTheRunsOwnLog(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "client")
	before := log.Writer()

	s := &scope{stateDir: stateDir}
	if err := s.divertDiagnostics(true); err != nil {
		t.Fatalf("divertDiagnostics: %v", err)
	}
	if log.Writer() == before {
		t.Error("the log stream was not diverted")
	}
	if !debuglog.Enabled() {
		t.Error("Debug on did not turn the internal debug log on")
	}
	want := filepath.Join(stateDir, "debug.log")
	if got := debuglog.DebugPath(); got != want {
		t.Errorf("the debug log is %q, want %q", got, want)
	}
	if got := debuglog.EventsLogPath(); got != filepath.Join(stateDir, "events.log") {
		t.Errorf("the event log is %q, want one in the run's own tree", got)
	}
	log.Print("a client line")
	debuglog.Internal{}.Printf("an internal line")
	s.close()
	// Closing twice is what a host that also defers it makes.
	s.close()

	if log.Writer() != before {
		t.Error("the log stream was not put back")
	}
	if debuglog.Enabled() {
		t.Error("the debug switch was left on after the run")
	}
	if got := debuglog.DebugPath(); got != debuglog.Path {
		t.Errorf("the debug log after the run is %q, want the process's own %q", got, debuglog.Path)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("the run's own debug log: %v", err)
	}
	if !strings.Contains(string(data), "a client line") || !strings.Contains(string(data), "an internal line") {
		t.Errorf("the run's debug log does not hold its lines:\n%s", data)
	}
}

// TestDebugOffKeepsTheFramesClean: with Debug off the process's log stream goes
// nowhere for the length of the run. An in-process daemon logging through it
// while a frame is drawn is what puts a line of text in the middle of an escape
// sequence, and the terminal then draws garbage after it.
func TestDebugOffKeepsTheFramesClean(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "client")
	before := log.Writer()

	s := &scope{stateDir: stateDir}
	if err := s.divertDiagnostics(false); err != nil {
		t.Fatalf("divertDiagnostics: %v", err)
	}
	if log.Writer() == before {
		t.Error("the log stream was not diverted")
	}
	if log.Writer() != io.Discard {
		t.Errorf("the log stream is %#v, want io.Discard", log.Writer())
	}
	if debuglog.Enabled() {
		t.Error("Debug off left the internal debug log on")
	}
	log.Print("a daemon line that must not reach a frame")
	s.close()

	if log.Writer() != before {
		t.Error("the log stream was not put back")
	}
	for _, name := range []string{"debug.log", "events.log"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !os.IsNotExist(err) {
			t.Errorf("a run with Debug off wrote %s: %v", name, err)
		}
	}
}

// writeProjectTheme puts a theme file where a workspace of that tree would read
// it.
func writeProjectTheme(t *testing.T, projectDir, id string) {
	t.Helper()
	dir := filepath.Join(projectDir, "client", "themes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"id":"` + id + `","fg":"#123456","bg":"#000000"}`
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeFile makes the directories a file needs and writes it.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// snapshot is every file under the given roots, by path and content digest. A
// root that does not exist contributes nothing.
func snapshot(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				out[path+"/"] = "dir"
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil //nolint:nilerr // a socket or a vanished file is not evidence
			}
			defer func() { _ = f.Close() }()
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				return nil //nolint:nilerr
			}
			out[path] = hex.EncodeToString(h.Sum(nil))
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return out
}

// diff is what a snapshot of two trees reports: the files that appeared,
// changed or went.
func diff(before, after map[string]string) string {
	var lines []string
	for path, sum := range after {
		switch prev, ok := before[path]; {
		case !ok:
			lines = append(lines, "  created "+path)
		case prev != sum:
			lines = append(lines, "  modified "+path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			lines = append(lines, "  removed "+path)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
