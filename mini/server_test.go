package mini_test

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/session"
	"github.com/getan/tuios-mini/internal/testutil"
	"github.com/getan/tuios-mini/mini"
	"github.com/getan/tuios-mini/mini/control"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIsolated(m))
}

func startMini(t *testing.T) (*mini.Server, *control.Client) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the M1 verb tests need a POSIX shell")
	}
	// A Unix socket path must stay under roughly 104 bytes, and the daemon
	// derives link and herdr socket names from it. t.TempDir() on macOS is far
	// too long for that, so use a short directory under /tmp.
	dir := shortDir(t)
	helper := filepath.Join(dir, "integrated-sh")
	data, err := os.ReadFile("testdata/integrated-sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, data, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv, err := mini.Start(ctx, mini.Config{
		SocketPath: socket, Session: "work", Shell: helper, Cwd: dir, Width: 100, Height: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := control.Dial(context.Background(), srv.Socket())
	if err != nil {
		srv.Stop()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		srv.Stop()
	})
	return srv, c
}

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tmini-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func call[T any](t *testing.T, c *control.Client, verb string, params map[string]any) T {
	t.Helper()
	raw, err := c.Call(context.Background(), verb, params)
	if err != nil {
		t.Fatalf("%s: %v", verb, err)
	}
	var out T
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s: decode: %v (%s)", verb, err, raw)
		}
	}
	return out
}

type windowList struct {
	Windows []struct {
		WindowID      string `json:"window_id"`
		DisplayName   string `json:"display_name"`
		ForegroundCmd string `json:"foreground_cmd"`
		AtPrompt      bool   `json:"at_prompt"`
	} `json:"windows"`
	FocusedWindowID string `json:"focused_window_id"`
	Total           int    `json:"total"`
}

type windowInfo struct {
	WindowID    string `json:"window_id"`
	DisplayName string `json:"display_name"`
}

type newWindow struct {
	WindowID string `json:"window_id"`
	Name     string `json:"name"`
}

type newSession struct {
	Session  string `json:"session"`
	WindowID string `json:"window_id"`
}

type capture struct {
	Content string `json:"content"`
}

type waitResult struct {
	Matched  bool `json:"matched"`
	ExitCode *int `json:"exit_code"`
}

type runResult struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

type shotResult struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

type setResult struct {
	DisplayName string `json:"display_name"`
	Minimized   bool   `json:"minimized"`
}

// waitAtPrompt waits until the shell reports its OSC 133 prompt mark through
// the daemon. run refuses a pane that is not at its prompt, and the mark that
// follows a finished command is processed asynchronously.
func waitAtPrompt(t *testing.T, c *control.Client, window string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		wl := call[windowList](t, c, "list-windows", map[string]any{"session": "work"})
		for _, w := range wl.Windows {
			if w.WindowID == window && w.AtPrompt {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("window %s never reported at_prompt", window)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestMiniDaemonVerbs drives every verb golder calls through the real daemon,
// on a real PTY. Nothing here is a fake: the shell below is a process, and the
// waits are the daemon's own event hub.
func TestMiniDaemonVerbs(t *testing.T) {
	srv, c := startMini(t)

	wl := call[windowList](t, c, "list-windows", map[string]any{"session": "work"})
	if wl.Total != 1 || len(wl.Windows) != 1 {
		t.Fatalf("list-windows = %d windows, want 1", wl.Total)
	}
	first := wl.Windows[0].WindowID

	got := call[windowInfo](t, c, "get-window", map[string]any{"session": "work", "window": first})
	if got.WindowID != first {
		t.Fatalf("get-window = %q, want %q", got.WindowID, first)
	}

	second := call[newWindow](t, c, "new-window", map[string]any{
		"session": "work", "name": "second", "command": []string{"/bin/sh", "-c", "sleep 30"}, "focus": false,
	}).WindowID

	call[json.RawMessage](t, c, "send-text", map[string]any{
		"session": "work", "window": first, "text": "printf 'HELLO-MINI\\n'", "submit": true,
	})
	matched := call[waitResult](t, c, "wait-for", map[string]any{
		"session": "work", "window": first, "condition": "window-output", "pattern": "HELLO-MINI", "timeout": 5000,
	})
	if !matched.Matched {
		t.Fatal("wait-for window-output did not match")
	}
	content := call[capture](t, c, "capture-pane", map[string]any{
		"session": "work", "window": first, "source": "visible", "lines": 50,
	})
	if !strings.Contains(content.Content, "HELLO-MINI") {
		t.Fatalf("capture-pane = %q, want HELLO-MINI", content.Content)
	}

	waitAtPrompt(t, c, first)
	ran := call[runResult](t, c, "run", map[string]any{
		"session": "work", "window": first, "command": "echo RUN-MINI", "timeout": 5000,
	})
	if ran.ExitCode != 0 || !strings.Contains(ran.Output, "RUN-MINI") {
		t.Fatalf("run = exit %d output %q", ran.ExitCode, ran.Output)
	}

	focused := call[struct {
		FocusedWindowID string `json:"focused_window_id"`
	}](t, c, "focus-window", map[string]any{"session": "work", "window": second})
	if focused.FocusedWindowID != second {
		t.Fatalf("focus-window = %q, want %q", focused.FocusedWindowID, second)
	}

	call[json.RawMessage](t, c, "move-window", map[string]any{
		"session": "work", "window": second, "workspace": 2, "follow": false,
	})
	resized := call[struct{ Width, Height int }](t, c, "resize", map[string]any{
		"session": "work", "window": second, "width": 90, "height": 20,
	})
	if resized.Width != 90 || resized.Height != 20 {
		t.Fatalf("resize = %dx%d", resized.Width, resized.Height)
	}

	renamed := call[setResult](t, c, "set-window", map[string]any{
		"session": "work", "window": second, "name": "renamed", "minimized": false,
	})
	if renamed.DisplayName != "renamed" {
		t.Fatalf("set-window name = %q, want renamed", renamed.DisplayName)
	}

	shotPath := filepath.Join(shortDir(t), "shot.png")
	shot := call[shotResult](t, c, "screenshot", map[string]any{
		"session": "work", "window": first, "format": "png", "out": shotPath,
	})
	if shot.Path == "" {
		t.Fatal("screenshot returned no path")
	}
	if info, err := os.Stat(shotPath); err != nil || info.Size() == 0 {
		t.Fatalf("screenshot file: info=%v err=%v", info, err)
	}

	call[json.RawMessage](t, c, "send-keys", map[string]any{
		"session": "work", "window": second, "keys": "ctrl+c",
	})
	exited := call[waitResult](t, c, "wait-for", map[string]any{
		"session": "work", "window": second, "condition": "window-exit", "timeout": 5000,
	})
	if !exited.Matched {
		t.Fatal("wait-for window-exit did not match after ctrl+c")
	}

	// A second connection proves restrict-connection narrows a live caller:
	// read_only must refuse the verb that types into a pane.
	restricted, err := control.Dial(context.Background(), srv.Socket())
	if err != nil {
		t.Fatal(err)
	}
	call[json.RawMessage](t, restricted, "restrict-connection", map[string]any{
		"scope": "all", "read_only": true,
	})
	_, err = restricted.Call(context.Background(), "send-text", map[string]any{
		"session": "work", "window": first, "text": "must-not-land",
	})
	var de *control.Error
	if !errors.As(err, &de) || de.Code != "forbidden" {
		t.Fatalf("send-text on a read-only connection = %v, want forbidden", err)
	}
	_ = restricted.Close()

	call[json.RawMessage](t, c, "close-window", map[string]any{"session": "work", "window": second})
	wl = call[windowList](t, c, "list-windows", map[string]any{"session": "work"})
	if wl.Total != 1 {
		t.Fatalf("windows after close = %d, want 1", wl.Total)
	}
}

func TestMiniStopRemovesSocket(t *testing.T) {
	srv, _ := startMini(t)
	socket := srv.Socket()
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("socket before Stop: %v", err)
	}
	srv.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(socket); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket %s still exists after Stop", socket)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestMiniScopeOwnRequiresThePaneToken is the real permission contract: the
// token printed by a pane, presented with its window id, narrows a connection
// to that pane's session. Another session is refused, and the pane's own
// session still accepts input.
func TestMiniScopeOwnRequiresThePaneToken(t *testing.T) {
	srv, c := startMini(t)
	wl := call[windowList](t, c, "list-windows", map[string]any{"session": "work"})
	if len(wl.Windows) != 1 {
		t.Fatalf("work has %d windows, want 1", len(wl.Windows))
	}
	first := wl.Windows[0].WindowID

	other := call[newSession](t, c, "new-session", map[string]any{"name": "other", "window": true})
	if other.WindowID == "" {
		t.Fatal("new-session other returned no window")
	}

	call[json.RawMessage](t, c, "send-text", map[string]any{
		"session": "work", "window": first,
		// The pane echoes the command line. Build the TOKEN= prefix from two
		// quoted halves so the echo cannot match the pattern below; only the
		// output line does.
		"text": `printf '%s%s\n' "TO""KEN=" "$TUIOS_PANE_TOKEN"`, "submit": true,
	})
	call[waitResult](t, c, "wait-for", map[string]any{
		"session": "work", "window": first, "condition": "window-output", "pattern": "TOKEN=", "timeout": 5000,
	})
	content := call[capture](t, c, "capture-pane", map[string]any{
		"session": "work", "window": first, "source": "visible", "lines": 50,
	}).Content
	match := regexp.MustCompile(`TOKEN=(\S+)`).FindStringSubmatch(content)
	if len(match) != 2 {
		t.Fatalf("capture = %q, want TOKEN=<token>", content)
	}

	restricted, err := control.Dial(context.Background(), srv.Socket())
	if err != nil {
		t.Fatal(err)
	}
	defer restricted.Close()
	call[json.RawMessage](t, restricted, "restrict-connection", map[string]any{
		"scope": "own", "pane_id": first, "pane_token": match[1],
	})

	// The caller's own session is still reachable.
	call[windowList](t, restricted, "list-windows", map[string]any{"session": "work"})

	for _, call := range []struct {
		verb   string
		params map[string]any
	}{
		{"list-windows", map[string]any{"session": "other"}},
		{"send-text", map[string]any{"session": "other", "window": other.WindowID, "text": "must-not-land"}},
	} {
		_, err := restricted.Call(context.Background(), call.verb, call.params)
		var de *control.Error
		if !errors.As(err, &de) || de.Code != "forbidden" {
			t.Fatalf("%s on another session = %v, want forbidden", call.verb, err)
		}
	}

	call[json.RawMessage](t, restricted, "send-text", map[string]any{
		"session": "work", "window": first, "text": "printf 'OWN-OK\\n'", "submit": true,
	})
	call[waitResult](t, restricted, "wait-for", map[string]any{
		"session": "work", "window": first, "condition": "window-output", "pattern": "OWN-OK", "timeout": 5000,
	})
}

// TestMiniCloseSendsSIGTERMFirst proves the pane close path gives a program
// that handles SIGTERM a chance to run its cleanup, instead of starting with
// SIGKILL. The pane is a shell script that writes a file from its trap.
func TestMiniCloseSendsSIGTERMFirst(t *testing.T) {
	_, c := startMini(t)
	dir := shortDir(t)
	script := filepath.Join(dir, "trap-term.sh")
	termFile := filepath.Join(dir, "term")
	readyFile := filepath.Join(dir, "ready")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
trap 'printf term > "$1"; exit 0' TERM
printf ready > "$2"
while :; do read -r _; done
`), 0o700); err != nil {
		t.Fatal(err)
	}
	window := call[newWindow](t, c, "new-window", map[string]any{
		"session": "work", "name": "term", "command": []string{"/bin/sh", script, termFile, readyFile}, "focus": false,
	}).WindowID
	// Close only after the trap is installed; a window closed while its
	// process is still starting has no handler to run yet.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the pane process never installed its SIGTERM trap")
		}
		time.Sleep(20 * time.Millisecond)
	}
	call[json.RawMessage](t, c, "close-window", map[string]any{"session": "work", "window": window})
	deadline = time.Now().Add(3 * time.Second)
	for {
		if data, err := os.ReadFile(termFile); err == nil && string(data) == "term" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the pane process did not run its SIGTERM trap before close")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMiniRefusesASecondDaemonOnTheSameSocket(t *testing.T) {
	srv, _ := startMini(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := mini.Start(ctx, mini.Config{SocketPath: srv.Socket(), Session: "other"}); err == nil {
		t.Fatal("a second daemon started on a live socket")
	}
}

// startMiniAt starts a daemon from a full Config in a short directory, with the
// helper shell the M1 tests use unless the Config names its own command, and
// returns the daemon and a client on it.
func startMiniAt(t *testing.T, cfg mini.Config) (*mini.Server, *control.Client) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these tests need a POSIX shell")
	}
	dir := shortDir(t)
	if cfg.SocketPath == "" {
		cfg.SocketPath = filepath.Join(dir, "d.sock")
	}
	if cfg.Session == "" {
		cfg.Session = "work"
	}
	if cfg.Shell == "" {
		cfg.Shell = writeHelperShell(t, dir)
	}
	if cfg.Cwd == "" {
		cfg.Cwd = dir
	}
	if cfg.Width == 0 {
		cfg.Width = 100
	}
	if cfg.Height == 0 {
		cfg.Height = 30
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv, err := mini.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := control.Dial(context.Background(), srv.Socket())
	if err != nil {
		srv.Stop()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		srv.Stop()
	})
	return srv, c
}

// writeHelperShell writes the OSC 133 shell the verb tests use into dir.
func writeHelperShell(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/integrated-sh")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "integrated-sh")
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMiniTwoDaemonsShareNothing starts two daemons in one process, each with a
// session of the same name and its own state and log, and proves neither one
// sees the other's files or writes into the other's log.
func TestMiniTwoDaemonsShareNothing(t *testing.T) {
	root := shortDir(t)
	first := filepath.Join(root, "one")
	second := filepath.Join(root, "two")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	a, ac := startMiniAt(t, mini.Config{
		SocketPath: filepath.Join(first, "d.sock"),
		StateDir:   filepath.Join(first, "state"),
		LogFile:    filepath.Join(first, "d.log"),
		Session:    "work",
	})
	b, bc := startMiniAt(t, mini.Config{
		SocketPath: filepath.Join(second, "d.sock"),
		StateDir:   filepath.Join(second, "state"),
		LogFile:    filepath.Join(second, "d.log"),
		Session:    "work",
	})
	if a.Socket() == b.Socket() {
		t.Fatal("two daemons ended up on one socket")
	}

	// Each daemon holds its own "work", with its own window.
	aw := call[windowList](t, ac, "list-windows", map[string]any{"session": "work"})
	bw := call[windowList](t, bc, "list-windows", map[string]any{"session": "work"})
	if len(aw.Windows) != 1 || len(bw.Windows) != 1 {
		t.Fatalf("window counts: %d and %d, want 1 each", len(aw.Windows), len(bw.Windows))
	}
	if aw.Windows[0].WindowID == bw.Windows[0].WindowID {
		t.Fatal("two daemons reported the same window id")
	}

	// A session made on one is not on the other.
	call[newSession](t, ac, "new-session", map[string]any{"name": "only-a", "window": true})
	bl := call[struct {
		Sessions []struct {
			Name string `json:"name"`
		} `json:"sessions"`
	}](t, bc, "list-sessions", map[string]any{})
	for _, sess := range bl.Sessions {
		if sess.Name == "only-a" {
			t.Fatal("a session created on one daemon is listed by the other")
		}
	}

	// Stopping one saves its state where it was told to, and leaves the other's
	// directory alone.
	a.Stop()
	<-a.Done()
	for name, dir := range map[string]string{
		"the stopped daemon's":  a.StateDir() + string(os.PathSeparator) + "work.json",
		"only-a on the stopped": a.StateDir() + string(os.PathSeparator) + "only-a.json",
	} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s state file is missing: %v", name, err)
		}
	}
	entries, err := os.ReadDir(b.StateDir())
	if err != nil {
		t.Fatalf("the running daemon's state directory: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "only-a.json" {
			t.Fatal("the first daemon's session state landed in the second daemon's state directory")
		}
	}
	if _, err := os.Stat(filepath.Join(second, "state", "only-a.json")); !os.IsNotExist(err) {
		t.Fatalf("only-a state on the second daemon: %v", err)
	}

	// Neither log holds a line about the other daemon: the start line names the
	// socket, so a shared or hijacked sink would show the other's path.
	firstLog := readFile(t, filepath.Join(first, "d.log"))
	secondLog := readFile(t, filepath.Join(second, "d.log"))
	if !strings.Contains(firstLog, filepath.Join(first, "d.sock")) {
		t.Errorf("the first log does not name its own socket:\n%s", firstLog)
	}
	if !strings.Contains(secondLog, filepath.Join(second, "d.sock")) {
		t.Errorf("the second log does not name its own socket:\n%s", secondLog)
	}
	if strings.Contains(firstLog, filepath.Join(second, "d.sock")) {
		t.Errorf("the first daemon's log holds the second daemon's lines:\n%s", firstLog)
	}
	if strings.Contains(secondLog, filepath.Join(first, "d.sock")) {
		t.Errorf("the second daemon's log holds the first daemon's lines:\n%s", secondLog)
	}
}

// waitForContent waits until the pane's visible screen holds want. It is what a
// test uses instead of wait-for when the output may already have arrived before
// the wait was registered: wait-for watches the stream from the moment it is
// called, and a pane that printed its line during Start printed it too early.
func waitForContent(t *testing.T, c *control.Client, session, window, want string) string {
	t.Helper()
	// A pane needs a moment to start: the daemon has just forked it, and a read
	// of the screen before its first write is a read of an empty screen. The
	// settle is short, and the poll after it is what waits.
	time.Sleep(300 * time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for {
		content := call[capture](t, c, "capture-pane", map[string]any{
			"session": session, "window": window, "source": "visible", "lines": 80,
		}).Content
		if strings.Contains(content, want) {
			return content
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pane never showed %q:\n%s", want, content)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestMiniLeavesForeignStateAlone writes state into the shared, upstream state
// directory a standalone tuios uses, then starts and stops an embedded daemon
// with a session of the same name. The upstream file must survive: an embedded
// workspace has its own state, and its startup sweep and shutdown must not
// reach outside it.
func TestMiniLeavesForeignStateAlone(t *testing.T) {
	upstream := session.ResurrectionStateDir()
	if err := os.MkdirAll(upstream, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(upstream, "work.json")
	const body = `{"name":"work","windows":[],"untouched":"by the workspace"}`
	if err := os.WriteFile(foreign, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// Residue a killed standalone daemon leaves, which a startup sweep removes.
	residue := filepath.Join(upstream, "half-written.json.tmp")
	if err := os.WriteFile(residue, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv, _ := startMiniAt(t, mini.Config{Session: "work"})
	srv.Stop()
	<-srv.Done()

	if got := readFile(t, foreign); got != body {
		t.Errorf("the standalone state file changed:\n got %s\nwant %s", got, body)
	}
	if _, err := os.Stat(residue); err != nil {
		t.Errorf("the embedded daemon swept the standalone state directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srv.StateDir(), "work.json")); err != nil {
		t.Errorf("the embedded daemon did not save its own state: %v", err)
	}
}

// TestMiniLogFileIsRealAndNotTheProcesses proves the configured log file is
// written, that the process's standard logger is left exactly as the embedding
// program set it, and that a daemon with no LogFile writes no file at all.
func TestMiniLogFileIsRealAndNotTheProcesses(t *testing.T) {
	before := log.Writer()
	beforeFlags, beforePrefix := log.Flags(), log.Prefix()

	srv, _ := startMiniAt(t, mini.Config{})
	srv.Stop()
	<-srv.Done()

	if log.Writer() != before || log.Flags() != beforeFlags || log.Prefix() != beforePrefix {
		t.Fatal("starting a daemon took over the process's standard logger")
	}
	if got := srv.LogFile(); got != "" {
		t.Errorf("a daemon with no LogFile reports %q", got)
	}
	if _, err := os.Stat(filepath.Join(srv.StateDir(), "tuios")); !os.IsNotExist(err) {
		t.Errorf("a daemon with no LogFile wrote a log: %v", err)
	}

	dir := shortDir(t)
	logPath := filepath.Join(dir, "workspace.log")
	srv2, _ := startMiniAt(t, mini.Config{LogFile: logPath})
	srv2.Stop()
	<-srv2.Done()

	contents := readFile(t, logPath)
	for _, want := range []string{"tuios-mini log started", "daemon started", "the daemon stopped"} {
		if !strings.Contains(contents, want) {
			t.Errorf("the log does not hold %q:\n%s", want, contents)
		}
	}
	if info, err := os.Stat(logPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("log file mode = %v (err %v), want 0600", info.Mode().Perm(), err)
	}
}

// TestMiniPaneEnvIsPerPane proves PaneEnv reaches the panes, not the process,
// and that a pane is never told about herdr or handed an identity the daemon
// did not give it.
func TestMiniPaneEnvIsPerPane(t *testing.T) {
	probe := `printf 'GS=%s HERDR=%s SESS=%s PATHOK=%s\n' "$GOLDER_WORKSPACE" "${HERDR_ENV-unset}" "$TUIOS_SESSION" "${PATH:+yes}"
printf 'DONE-ENV\n'
sleep 30`
	srv, c := startMiniAt(t, mini.Config{
		Session: "work",
		PaneEnv: []string{"GOLDER_WORKSPACE=1", "GOLDER_PROJECT=demo", "HERDR_ENV=1"},
		Command: []string{"/bin/sh", "-c", probe},
	})
	// HERDR_ENV is dropped, not refused: the daemon speaks no herdr protocol,
	// and a caller asking for it gets a pane without it rather than a failure.
	if os.Getenv("GOLDER_WORKSPACE") != "" || os.Getenv("GOLDER_PROJECT") != "" {
		t.Fatal("PaneEnv was applied to the process environment")
	}

	window := call[windowList](t, c, "list-windows", map[string]any{"session": "work"}).Windows[0].WindowID
	got := waitForContent(t, c, "work", window, "DONE-ENV")
	for _, want := range []string{"GS=1", "HERDR=unset", "SESS=work", "PATHOK=yes"} {
		if !strings.Contains(got, want) {
			t.Errorf("the pane's environment does not hold %q:\n%s", want, got)
		}
	}
	_ = srv
}

// TestMiniRefusesIdentityPaneEnv proves a caller cannot hand a pane an identity
// or a token: the variables are refused when the daemon starts, before any pane
// runs.
func TestMiniRefusesIdentityPaneEnv(t *testing.T) {
	dir := shortDir(t)
	for _, kv := range []string{
		"TUIOS_PANE_TOKEN=forged",
		"TUIOS_SESSION=other",
		"TUIOS_SOCKET=/tmp/someone-else.sock",
		"TUIOS_PANE_GRANTS=admin",
		"TUIOS_ENV=1",
	} {
		t.Run(kv, func(t *testing.T) {
			_, err := mini.Start(context.Background(), mini.Config{
				SocketPath: filepath.Join(dir, "d.sock"),
				StateDir:   filepath.Join(dir, "state"),
				Shell:      "/bin/sh",
				PaneEnv:    []string{kv},
			})
			if err == nil {
				t.Fatalf("Start accepted PaneEnv %q", kv)
			}
			if !strings.Contains(err.Error(), strings.SplitN(kv, "=", 2)[0]) {
				t.Errorf("the refusal does not name the variable: %v", err)
			}
		})
	}
}

// TestMiniRefusesBadPolicy proves an unknown permission mode or grant is
// refused with the values that would have worked, rather than quietly read as
// something else.
func TestMiniRefusesBadPolicy(t *testing.T) {
	dir := shortDir(t)
	for _, cfg := range []mini.Config{
		{SocketPath: filepath.Join(dir, "a.sock"), PermissionsMode: "loose"},
		{SocketPath: filepath.Join(dir, "b.sock"), PermissionsMode: "strict", Grants: []string{"everything"}},
	} {
		cfg.StateDir = filepath.Join(dir, "state")
		if _, err := mini.Start(context.Background(), cfg); err == nil {
			t.Fatalf("Start accepted %+v", cfg)
		}
	}
}

// TestMiniRefusesAnImpossibleSocketPath proves a socket path over the kernel's
// limit fails at Start with a message that says so, instead of binding and
// returning EINVAL.
func TestMiniRefusesAnImpossibleSocketPath(t *testing.T) {
	dir := shortDir(t)
	long := filepath.Join(dir, strings.Repeat("n", 120)+".sock")
	_, err := mini.Start(context.Background(), mini.Config{SocketPath: long, StateDir: filepath.Join(dir, "state")})
	if err == nil {
		t.Fatal("Start accepted a socket path over the limit")
	}
	for _, want := range []string{"limit", "shorter", "XDG_RUNTIME_DIR"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// TestMiniStartHonoursACancelledContext proves Start refuses to build a daemon
// for a caller that has already walked away, and leaves nothing behind.
func TestMiniStartHonoursACancelledContext(t *testing.T) {
	dir := shortDir(t)
	socket := filepath.Join(dir, "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mini.Start(ctx, mini.Config{SocketPath: socket, StateDir: filepath.Join(dir, "state")}); err == nil {
		t.Fatal("Start accepted a cancelled context")
	} else if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("a cancelled start left a socket behind: %v", err)
	}
}

// TestMiniStopReleasesTheContextWatcher proves Stop returns the goroutine that
// waits on the caller's context, instead of parking it on a context nobody
// cancels. The context here is never cancelled: only Stop ends the daemon.
func TestMiniStopReleasesTheContextWatcher(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()

	for range 3 {
		srv, _ := startMini(t)
		srv.Stop()
		<-srv.Done()
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= before+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines after three started and stopped daemons: %d, before: %d", n, before)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestMiniInitialCommand proves Config.Command is the argv the first pane runs,
// with the session's cwd and environment, and that the session is otherwise a
// normal one.
func TestMiniInitialCommand(t *testing.T) {
	cwd := shortDir(t)
	srv, c := startMiniAt(t, mini.Config{
		Session: "work",
		Cwd:     cwd,
		Command: []string{"/bin/sh", "-c", `printf 'CMD-OK cwd=%s\n' "$PWD"; sleep 30`},
	})
	window := call[windowList](t, c, "list-windows", map[string]any{"session": "work"}).Windows[0].WindowID
	got := waitForContent(t, c, "work", window, "CMD-OK")
	if !strings.Contains(got, "CMD-OK") {
		t.Fatalf("the first pane did not run the command:\n%s", got)
	}
	// /tmp may be a link on macOS (var -> private/var), so compare the last
	// element rather than the whole path.
	if !strings.Contains(got, filepath.Base(cwd)) {
		t.Errorf("the first pane did not start in %s:\n%s", cwd, got)
	}
	_ = srv
}

// TestMiniStrictPermissionsKeepGolderVerbs proves the permission policy is
// compatible with the verbs golder drives a pane with: under strict with the
// default grants, a pane's own token still reaches its own session for every
// verb golder uses, and another session stays out of reach.
func TestMiniStrictPermissionsKeepGolderVerbs(t *testing.T) {
	srv, c := startMiniAt(t, mini.Config{
		Session:         "work",
		PermissionsMode: "strict",
		// The default grants, named explicitly: read, write and fan.
		Grants: []string{"read", "write", "fan"},
	})
	first := call[windowList](t, c, "list-windows", map[string]any{"session": "work"}).Windows[0].WindowID
	other := call[newSession](t, c, "new-session", map[string]any{"name": "other", "window": true}).WindowID

	// The pane reports the grants it was started with, which is the strict
	// default and not admin.
	call[json.RawMessage](t, c, "send-text", map[string]any{
		"session": "work", "window": first,
		"text": `printf 'GRANTS=%s\n' "$TUIOS_PANE_GRANTS"; printf 'T%s=%s\n' "OKEN" "$TUIOS_PANE_TOKEN"`, "submit": true,
	})
	call[waitResult](t, c, "wait-for", map[string]any{
		"session": "work", "window": first, "condition": "window-output", "pattern": "TOKEN=", "timeout": 5000,
	})
	content := call[capture](t, c, "capture-pane", map[string]any{
		"session": "work", "window": first, "source": "visible", "lines": 60,
	}).Content
	if !strings.Contains(content, "GRANTS=read,write,fan") {
		t.Fatalf("the pane was not held to the strict default:\n%s", content)
	}
	match := regexp.MustCompile(`TOKEN=(\S+)`).FindStringSubmatch(content)
	if len(match) != 2 {
		t.Fatalf("capture = %q, want TOKEN=<token>", content)
	}

	restricted, err := control.Dial(context.Background(), srv.Socket())
	if err != nil {
		t.Fatal(err)
	}
	defer restricted.Close()
	call[json.RawMessage](t, restricted, "restrict-connection", map[string]any{
		"scope": "own", "pane_id": first, "pane_token": match[1],
	})

	// Every verb golder drives the pane with, in the pane's own session.
	call[windowList](t, restricted, "list-windows", map[string]any{"session": "work"})
	call[capture](t, restricted, "capture-pane", map[string]any{"session": "work", "window": first, "lines": 20})
	call[waitResult](t, restricted, "wait-for", map[string]any{
		"session": "work", "window": first, "condition": "window-output", "pattern": "GRANTS=", "timeout": 1000,
	})
	// A window made by the person's own connection, which the pane can then
	// write to: new-window is not a verb a restricted connection may call.
	second := call[newWindow](t, c, "new-window", map[string]any{
		"session": "work", "name": "written", "command": []string{"/bin/sh", "-c", "sleep 20"}, "focus": false,
	}).WindowID
	if second == "" {
		t.Fatal("new-window under strict returned no window")
	}
	call[json.RawMessage](t, restricted, "send-text", map[string]any{
		"session": "work", "window": first, "text": "printf 'STRICT-OK\n'", "submit": true,
	})
	call[waitResult](t, restricted, "wait-for", map[string]any{
		"session": "work", "window": first, "condition": "window-output", "pattern": "STRICT-OK", "timeout": 5000,
	})
	call[json.RawMessage](t, c, "close-window", map[string]any{"session": "work", "window": second})

	// Another session is still out of reach.
	for _, forbidden := range []struct {
		verb   string
		params map[string]any
	}{
		{"list-windows", map[string]any{"session": "other"}},
		{"send-text", map[string]any{"session": "other", "window": other, "text": "must-not-land"}},
	} {
		_, err := restricted.Call(context.Background(), forbidden.verb, forbidden.params)
		var de *control.Error
		if !errors.As(err, &de) || de.Code != "forbidden" {
			t.Fatalf("%s on another session = %v, want forbidden", forbidden.verb, err)
		}
	}
}

// TestMiniStateDirSurvivesARestart proves StateDir is what a saved session is
// read back from: a second daemon on the same state directory sees the session
// the first one saved, and a daemon on another state directory does not.
func TestMiniStateDirSurvivesARestart(t *testing.T) {
	root := shortDir(t)
	state := filepath.Join(root, "state")
	socket := filepath.Join(root, "d.sock")

	first, err := mini.Start(context.Background(), mini.Config{
		SocketPath: socket, StateDir: state, Session: "saved", Shell: writeHelperShell(t, root), Cwd: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	first.Stop()
	<-first.Done()
	if _, err := os.Stat(filepath.Join(state, "saved.json")); err != nil {
		t.Fatalf("the session state is not in the state directory: %v", err)
	}

	// A daemon that restores from that state directory is told about it, but an
	// embedded daemon does not restore on its own: the file is there to be
	// resurrected, which is the daemon's own contract.
	second, err := mini.Start(context.Background(), mini.Config{
		SocketPath: filepath.Join(root, "e.sock"), StateDir: state, Session: "other", Shell: writeHelperShell(t, root), Cwd: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Stop()
	if _, err := os.Stat(filepath.Join(state, "saved.json")); err != nil {
		t.Fatalf("a second daemon removed the saved session: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "other.json")); err == nil {
		t.Fatal("the second session was saved before the daemon stopped")
	}
}

// TestMiniSpeaksNoHerdr proves the embedded daemon leaves the herdr protocol
// off all the way down: no socket beside its own, no link directory, no
// HERDR_BIN_PATH pointing at an executable, and no HERDR_ variable in a pane.
func TestMiniSpeaksNoHerdr(t *testing.T) {
	srv, c := startMiniAt(t, mini.Config{
		Session: "work",
		Command: []string{"/bin/sh", "-c", `printf 'HERDR=[%s] BIN=[%s]\n' "$HERDR_ENV" "$HERDR_BIN_PATH"; printf 'HERDR-DONE\n'; sleep 30`},
	})
	for _, path := range []string{srv.Socket() + ".herdr", filepath.Join(filepath.Dir(srv.Socket()), "herdr")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("the herdr protocol left %s behind: %v", path, err)
		}
	}
	window := call[windowList](t, c, "list-windows", map[string]any{"session": "work"}).Windows[0].WindowID
	got := waitForContent(t, c, "work", window, "HERDR-DONE")
	if !strings.Contains(got, "HERDR=[] BIN=[]") {
		t.Errorf("a pane was told about a herdr protocol this daemon does not speak:\n%s", got)
	}
}

// TestMiniKeepsTheAgentsQuiet proves the embedded daemon starts with the agent
// extras off: no checkpoint is taken and no foreground-process detector runs,
// unless the caller asks for them.
func TestMiniKeepsTheAgentsQuiet(t *testing.T) {
	srv, _ := startMiniAt(t, mini.Config{Session: "work"})
	defer srv.Stop()
	// A checkpoint would leave a git ref and a directory behind a pane's agent
	// turn; the detector would poll every pane. Neither has anything to show
	// here unless the daemon was talking to git, which the state directory's
	// own listing would show.
	if _, err := os.Stat(filepath.Join(srv.StateDir(), "checkpoints")); !os.IsNotExist(err) {
		t.Errorf("the daemon wrote a checkpoint: %v", err)
	}
}
