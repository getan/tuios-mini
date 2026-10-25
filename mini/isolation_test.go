//go:build unix

package mini_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getan/tuios-mini/mini"
)

// plantBrokenManifest writes a manifest a loader must report: the schema version
// is one no build here understands. It is how a test can see whether a directory
// was read at all — the daemon's own log carries the manifest path when it tries
// to load it, and says nothing when it never looks.
func plantBrokenManifest(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make harness dir: %v", err)
	}
	body := "schema_version = 99\nid = \"" + id + "\"\n\n[detect]\ncomm = [\"" + id + "-process\"]\n"
	if err := os.WriteFile(filepath.Join(dir, id+".toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the workspace log: %v", err)
	}
	return string(data)
}

// TestMiniIgnoresTheUserHarnessDirectory is the embedding default: a workspace
// reads the manifests compiled into the binary, and not the harness rules a
// standalone tuios on the same machine was configured with. The planted manifest
// cannot load, so the daemon's own log would name it if the directory were read.
func TestMiniIgnoresTheUserHarnessDirectory(t *testing.T) {
	userDir := t.TempDir()
	plantBrokenManifest(t, userDir, "planted-user")
	t.Setenv("TUIOS_HARNESS_DIR", userDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(userDir, "xdg"))

	dir := shortDir(t)
	logPath := filepath.Join(dir, "w.log")
	srv, err := mini.Start(context.Background(), mini.Config{
		SocketPath: filepath.Join(dir, "w.sock"), Session: "work", Shell: "/bin/sh", LogFile: logPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)

	got := readLog(t, logPath)
	if strings.Contains(got, "planted-user") {
		t.Fatalf("the workspace daemon read the user harness directory (TUIOS_HARNESS_DIR):\n%s", got)
	}
	if strings.Contains(got, "harness manifest") {
		t.Fatalf("the workspace daemon read a harness directory it was not given:\n%s", got)
	}
}

// TestMiniReadsItsOwnHarnessDirectory is the seam the default leaves open: a
// caller that wants harness rules points the daemon at a folder it owns, and
// that folder is the only one read. The same broken manifest is planted, so a
// directory that is read and one that is not are told apart by the log rather
// than by a claim.
func TestMiniReadsItsOwnHarnessDirectory(t *testing.T) {
	ownDir := t.TempDir()
	plantBrokenManifest(t, ownDir, "planted-own")
	userDir := t.TempDir()
	plantBrokenManifest(t, userDir, "planted-user")
	t.Setenv("TUIOS_HARNESS_DIR", userDir)

	dir := shortDir(t)
	logPath := filepath.Join(dir, "w.log")
	srv, err := mini.Start(context.Background(), mini.Config{
		SocketPath: filepath.Join(dir, "w.sock"), Session: "work", Shell: "/bin/sh", LogFile: logPath,
		HarnessDir: ownDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)

	got := readLog(t, logPath)
	if !strings.Contains(got, "planted-own") {
		t.Fatalf("the workspace daemon did not read its own harness directory %s:\n%s", ownDir, got)
	}
	if strings.Contains(got, "planted-user") {
		t.Fatalf("the workspace daemon also read the user harness directory:\n%s", got)
	}
}

// TestMiniBindsNoProductSocket is the socket side of the same boundary. An
// embedded workspace gets one socket, its pid file and its state directory. The
// standalone daemon's link sockets, herdr protocol socket and forwarding links
// are files its host never asked for.
func TestMiniBindsNoProductSocket(t *testing.T) {
	dir := shortDir(t)
	sock := filepath.Join(dir, "w.sock")
	srv, err := mini.Start(context.Background(), mini.Config{
		SocketPath: sock, Session: "work", Shell: "/bin/sh",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the workspace directory: %v", err)
	}
	sockets := 0
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.Type()&os.ModeSymlink != 0:
			t.Errorf("the workspace left a forwarding link %q beside its socket", name)
		case strings.Contains(name, "agent-") || strings.Contains(name, "wayland-"):
			t.Errorf("the workspace left a forwarding link %q beside its socket", name)
		case strings.HasSuffix(name, ".link"), strings.HasSuffix(name, ".link-human"), strings.HasSuffix(name, ".herdr"):
			t.Errorf("the workspace opened the standalone product socket %q", name)
		}
		if e.Type()&os.ModeSocket != 0 {
			sockets++
			if name != filepath.Base(sock) {
				t.Errorf("the workspace opened a second socket %q", name)
			}
		}
	}
	if sockets != 1 {
		t.Fatalf("the workspace opened %d sockets, want its own one", sockets)
	}
	if _, err := os.Stat(sock + ".pid"); err != nil {
		t.Errorf("the workspace did not write its pid file: %v", err)
	}
}

// TestMiniKeepsExplicitAgentReports covers the other half of the profile: what
// a local-only daemon keeps. A pane that reports its own agent state through the
// control socket is the workspace's whole agent surface, and it must work with
// the detector, the transcript watcher and the product services off.
func TestMiniKeepsExplicitAgentReports(t *testing.T) {
	_, c := startMini(t)

	list := call[windowList](t, c, "list-windows", map[string]any{"session": "work"})
	if len(list.Windows) == 0 {
		t.Fatal("the workspace session has no window")
	}
	window := list.Windows[0].WindowID

	type setResult struct {
		State   string `json:"state"`
		Applied bool   `json:"applied"`
		Reason  string `json:"reason"`
	}
	set := call[setResult](t, c, "set-agent-state", map[string]any{
		"session": "work", "window": window, "state": "needs_input", "message": "approve the plan",
	})
	if !set.Applied {
		t.Fatalf("the workspace refused an explicit agent report: state %q, reason %q", set.State, set.Reason)
	}
	if set.State != "needs_input" {
		t.Fatalf("the workspace recorded %q, want needs_input", set.State)
	}

	type getResult struct {
		State   string `json:"state"`
		Message string `json:"message"`
		Source  string `json:"source"`
	}
	got := call[getResult](t, c, "get-agent-state", map[string]any{"session": "work", "window": window})
	if got.State != "needs_input" || got.Message != "approve the plan" {
		t.Fatalf("the reported state did not survive: %+v", got)
	}
	if got.Source != "report" {
		t.Fatalf("the state is attributed to %q, want the pane's own report", got.Source)
	}
}

// TestMiniControlStillAnswers is the control surface the workspace is for: the
// socket answers about its sessions, windows and panes with the product
// services off.
func TestMiniControlStillAnswers(t *testing.T) {
	_, c := startMini(t)
	if _, err := c.Call(context.Background(), "list-sessions", nil); err != nil {
		t.Fatalf("list-sessions on the embedded daemon: %v", err)
	}
	sessions := call[struct {
		Sessions []struct {
			Name string `json:"name"`
		} `json:"sessions"`
	}](t, c, "list-sessions", nil)
	found := false
	for _, s := range sessions.Sessions {
		if s.Name == "work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the workspace session is not listed: %+v", sessions)
	}
}
