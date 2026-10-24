package mini_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuios/mini"
	"github.com/Gaurav-Gosain/tuios/mini/control"
)

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
