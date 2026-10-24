package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// These are the checks that run before a single byte is dialled. Each one is a
// refusal: a client that started anyway would be hidden behind someone else's
// daemon, someone else's config file, or a pipe.

// validOptions is options that pass every check without needing a daemon. The
// socket points at a path nothing listens on: the tests that reach the dial
// want it to fail there.
func validOptions(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	return Options{
		Socket:     filepath.Join(dir, "no-daemon.sock"),
		Session:    "work",
		Version:    "test",
		ConfigPath: filepath.Join(dir, "config.toml"),
		Width:      100,
		Height:     30,
	}
}

// fakeTerminal answers the terminal checks affirmatively, so a test in a pipe
// can reach the code after them. It restores on cleanup; the variables are
// process-wide.
func fakeTerminal(t *testing.T) {
	t.Helper()
	prevIsTerminal, prevSize, prevTerm := isTerminalFile, sizeOfFile, termVariable
	isTerminalFile = func(*os.File) bool { return true }
	sizeOfFile = func(*os.File) (int, int, error) { return 100, 30, nil }
	termVariable = func(string) string { return "xterm-256color" }
	t.Cleanup(func() {
		isTerminalFile, sizeOfFile, termVariable = prevIsTerminal, prevSize, prevTerm
	})
}

// pipeTerminal answers the terminal checks negatively, as a pipe does.
func pipeTerminal(t *testing.T) {
	t.Helper()
	prevIsTerminal := isTerminalFile
	isTerminalFile = func(*os.File) bool { return false }
	t.Cleanup(func() { isTerminalFile = prevIsTerminal })
}

// TestRunRefusesIncompleteOptions: each missing field is named, because the
// caller has to fix it and the alternative is a client that quietly reached a
// daemon or a config file it was not meant to.
func TestRunRefusesIncompleteOptions(t *testing.T) {
	// The terminal checks come after these, so no seam is needed: nothing here
	// should get as far as looking at a terminal.
	for _, tc := range []struct {
		name  string
		opts  Options
		field string
	}{
		{"no socket", Options{ConfigPath: "/tmp/x/config.toml"}, "Options.Socket"},
		{"no config path", Options{Socket: "/tmp/x.sock"}, "Options.ConfigPath"},
		{"create with no name", Options{Socket: "/tmp/x.sock", ConfigPath: "/tmp/x/config.toml", CreateSession: true}, "Options.Session"},
		{"negative size", Options{Socket: "/tmp/x.sock", ConfigPath: "/tmp/x/config.toml", Width: -1}, "Options.Width"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Run(context.Background(), tc.opts)
			if err == nil {
				t.Fatal("Run accepted options that cannot work")
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("the refusal does not name %s: %v", tc.field, err)
			}
			if errors.Is(err, ErrAlreadyRunning) {
				t.Errorf("the refusal came from the runner lock, not the options: %v", err)
			}
		})
	}
}

// TestRunRefusesAnAlreadyCancelledContext: a host that cancels before the
// client starts gets the cancellation, not a client.
func TestRunRefusesAnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, validOptions(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run with a cancelled context = %v, want context.Canceled", err)
	}
}

// TestRunRefusesAPipe: a client that draws into a pipe or reads from one shows
// nobody anything and takes every keystroke nowhere. It is refused with a
// message that says what to pass instead.
func TestRunRefusesAPipe(t *testing.T) {
	pipeTerminal(t)
	opts := validOptions(t)
	err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("Run started against a pipe")
	}
	if !strings.Contains(err.Error(), "not a terminal") {
		t.Errorf("the refusal does not say the stream is not a terminal: %v", err)
	}
	if _, ok := config.ScopedConfigPath(); ok {
		t.Error("the config path was scoped before the terminal was checked, and left open by the refusal")
	}
}

// TestRunRefusesATerminalTooSmall: the check the standalone client makes, with
// the same floor.
func TestRunRefusesATerminalTooSmall(t *testing.T) {
	fakeTerminal(t)
	sizeOfFile = func(*os.File) (int, int, error) { return 20, 5, nil }
	err := Run(context.Background(), validOptions(t))
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("Run on a 20x5 terminal = %v, want a refusal", err)
	}
}

// TestRunRefusesADumbTerminal: TERM with no capabilities cannot render the
// interface, and starting anyway leaves a screen of nothing.
func TestRunRefusesADumbTerminal(t *testing.T) {
	fakeTerminal(t)
	termVariable = func(key string) string {
		if key == "TERM" {
			return "dumb"
		}
		return ""
	}
	err := Run(context.Background(), validOptions(t))
	if err == nil || !strings.Contains(err.Error(), "TERM") {
		t.Fatalf("Run with TERM=dumb = %v, want a refusal", err)
	}
}

// TestRunRefusesASecondConcurrentClient: the client is built on process-wide
// state, so two of them cannot run at once. The second call is told so.
func TestRunRefusesASecondConcurrentClient(t *testing.T) {
	fakeTerminal(t)
	if !running.CompareAndSwap(false, true) {
		t.Fatal("another client was already running; the tests share this flag")
	}
	defer running.Store(false)

	err := Run(context.Background(), validOptions(t))
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Run while another client is running = %v, want ErrAlreadyRunning", err)
	}
}

// TestRunRestoresEverythingItSet is the cleanup contract on a run that cannot
// attach: the config path scope, the appearance globals and the runner flag are
// all back as they were, and the client left no daemon connection behind.
//
// The socket here is a path nothing listens on, which is the most common way
// this fails in a host that got its daemon's lifetime wrong.
func TestRunRestoresEverythingItSet(t *testing.T) {
	fakeTerminal(t)
	opts := validOptions(t)

	before := config.Global
	if _, ok := config.ScopedConfigPath(); ok {
		t.Fatal("a config scope was open before the test started")
	}

	err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("Run attached to a socket nothing listens on")
	}
	if !strings.Contains(err.Error(), opts.Socket) {
		t.Errorf("the dial error does not name the socket %s: %v", opts.Socket, err)
	}

	if _, ok := config.ScopedConfigPath(); ok {
		t.Error("the config path scope was left open after Run returned")
	}
	if !reflect.DeepEqual(config.Global, before) {
		t.Error("the appearance globals were left changed after Run returned")
	}
	if running.Load() {
		t.Error("the runner flag was left set after Run returned")
	}
}

// TestRunReadsItsOwnConfigAndNotTheUsers is the reason this package scopes the
// config path at all: a host hands over a config file, and neither reading nor
// writing may fall back to the person's.
//
// The user's file here holds a setting the client must not pick up, and a
// second one that a save inside the client must not reach.
func TestRunReadsItsOwnConfigAndNotTheUsers(t *testing.T) {
	fakeTerminal(t)

	userPath, err := config.GetConfigPath()
	if err != nil {
		t.Fatalf("the user's config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatalf("create the user's config directory: %v", err)
	}
	const userConfig = "[appearance]\nborder_style = \"double\"\n"
	if err := os.WriteFile(userPath, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("write the user's config: %v", err)
	}

	opts := validOptions(t)
	// The client cannot attach (nothing listens), but the config is loaded
	// before the dial: that is the read this test is about.
	if err := Run(context.Background(), opts); err == nil {
		t.Fatal("Run attached to a socket nothing listens on")
	}

	scoped, statErr := os.Stat(opts.ConfigPath)
	if statErr != nil {
		t.Fatalf("the client did not create its own config at %s: %v", opts.ConfigPath, statErr)
	}
	if scoped.Size() == 0 {
		t.Errorf("the client's own config %s is empty", opts.ConfigPath)
	}

	after, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("read the user's config: %v", err)
	}
	if string(after) != userConfig {
		t.Errorf("the person's config changed during a workspace run:\nbefore:\n%s\nafter:\n%s", userConfig, after)
	}
}
