package workspace

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/getan/tuios-mini/internal/app"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/debuglog"
	"github.com/getan/tuios-mini/internal/theme"
)

// clientStateDirName is the directory Run derives beside Options.ConfigPath
// when Options.StateDir names none. "state" rather than "tuios": nothing this
// client keeps may sit under a path the person's own client would look in.
const clientStateDirName = "state"

// scope is the process-wide state one run takes for itself, and the list of
// functions that puts it back.
//
// The client is built on globals — the config file the settings page saves, the
// theme registry and the theme in force, the glyph sets and the one selected,
// the directory the sidebar and the launcher write to, the standard library
// logger — because it was written for a process that is one client. A host that
// embeds it opens each of those onto its own tree before the client starts and
// closes them when it stops. The close is what makes the next run, and the
// host's own screens after it, see their own state rather than this one's, and
// it has to happen on every way out: a refusal, a failed attach, a cancel and a
// quit all reach it through the same defer.
type scope struct {
	restores []func()
	// stateDir is the tree this run owns: the UI state, the custom themes and
	// glyph sets, and the debug log inside it.
	stateDir string
	closed   bool
}

// add records one restore, to run when the scope is closed.
func (s *scope) add(restore func()) { s.restores = append(s.restores, restore) }

// close puts back everything the scope opened, newest first, and is safe to
// call more than once.
func (s *scope) close() {
	if s.closed {
		return
	}
	s.closed = true
	for i := len(s.restores) - 1; i >= 0; i-- {
		s.restores[i]()
	}
	s.restores = nil
}

// open scopes the process to the tree these options name and diverts the
// diagnostic logs into it. The scope is returned closed only on an error, so
// nothing it opened is left open by a half-finished setup.
func (o Options) open() (*scope, error) {
	stateDir, err := o.stateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: create the client state directory %s: %w", stateDir, err)
	}

	s := &scope{stateDir: stateDir}

	restoreConfig, err := config.ScopeConfigPath(o.ConfigPath)
	if err != nil {
		s.close()
		return nil, fmt.Errorf("workspace: %w", err)
	}
	s.add(restoreConfig)

	// Themes and glyph sets: the two directories a project's own custom assets
	// live in, and the process-wide registry and selections that go with them.
	// A workspace that left the registry as it found it would draw the next
	// project's custom theme, and one that did not scope the directories would
	// read the person's.
	restoreAssets, err := theme.ScopeAssets(
		filepath.Join(stateDir, "themes"),
		filepath.Join(stateDir, "glyphs"),
	)
	if err != nil {
		s.close()
		return nil, fmt.Errorf("workspace: %w", err)
	}
	s.add(restoreAssets)

	// Everything else the client keeps: the sidebar's preferences, the
	// launcher's history, saved layouts, tapes, a crash report.
	restoreUI, err := app.ScopeClientState(stateDir)
	if err != nil {
		s.close()
		return nil, fmt.Errorf("workspace: %w", err)
	}
	s.add(restoreUI)

	if err := s.divertDiagnostics(o.Debug); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// stateDir is the directory this run owns: Options.StateDir, or "state" beside
// Options.ConfigPath when the host named no directory of its own.
//
// A host that named only a config file still gets a private tree. The
// alternative — the user's state directory — is the one answer this must never
// give: the client would read the person's sidebar and write the person's
// launcher history, which is exactly what a host that embedded the client was
// trying to avoid.
func (o Options) stateDir() (string, error) {
	if o.StateDir != "" {
		abs, err := filepath.Abs(o.StateDir)
		if err != nil {
			return "", fmt.Errorf("workspace: resolve %s: %w", o.StateDir, err)
		}
		return abs, nil
	}
	cfg, err := filepath.Abs(o.ConfigPath)
	if err != nil {
		return "", fmt.Errorf("workspace: resolve %s: %w", o.ConfigPath, err)
	}
	return filepath.Join(filepath.Dir(cfg), clientStateDirName), nil
}

// divertDiagnostics sends the process's log stream and the diagnostic logs into
// the run's own tree, and records how to put them back.
//
// The standard library logger is the process's, and an in-process daemon logs
// through it: one log.Printf from the daemon while a frame is being drawn puts
// its line in the middle of an escape sequence, and the terminal draws garbage
// after it. So the stream is taken over for the length of the run — into the
// run's own debug log when Debug asked for one, and into nothing when it did
// not — and given back afterwards.
//
// Nothing is permanent. The previous writer is restored on the way out, the
// debug log's on/off switch is not latched by a run (see debuglog.Scope), and
// no run writes to /tmp/tuios-debug.log: two workspaces on one machine keep two
// logs, under the two directories their hosts gave them.
//
// What this costs is documented rather than hidden: log lines written by
// anything else in the host process while a client runs are discarded too when
// Debug is off, because the stream is one stream. A host that needs its own
// lines during a workspace run should log through its own logger, not through
// the standard one.
func (s *scope) divertDiagnostics(on bool) error {
	restoreDebug, err := debuglog.Scope(s.stateDir, on)
	if err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	s.add(restoreDebug)

	previous := log.Writer()
	restoreLog := func() { log.SetOutput(previous) }
	if !on {
		log.SetOutput(io.Discard)
		s.add(restoreLog)
		return nil
	}

	// The scope's own directory, made here as well as in open: a host that
	// asked for a debug log asked for the file to exist.
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		log.SetOutput(io.Discard)
		s.add(restoreLog)
		return nil
	}
	path := filepath.Join(s.stateDir, "debug.log")
	f, err := debuglog.Open(path)
	if err != nil {
		// The frames matter more than the log lines: a client that cannot open
		// its own log still must not print over itself.
		log.SetOutput(io.Discard)
		s.add(restoreLog)
		return nil
	}
	log.SetOutput(f)
	s.add(func() {
		restoreLog()
		_ = f.Close()
	})
	return nil
}
