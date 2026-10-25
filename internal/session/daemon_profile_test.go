//go:build unix

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// productSockets are the files a standalone daemon opens beside its own socket
// and a local-only daemon must not. They are named rather than discovered so a
// new one has to be added here, with the profile decision that goes with it.
func productSockets(socketPath string) map[string]string {
	return map[string]string{
		"link":       LinkSocketPath(socketPath),
		"link-human": LinkHumanSocketPath(socketPath),
		"herdr":      HerdrSocketPath(socketPath),
	}
}

// TestLocalOnlyDaemonBindsOnlyItsOwnSocket is the socket half of the embedding
// boundary. The standalone daemon opens a link socket, a link-human socket and a
// herdr protocol socket next to its own, and an embedded workspace must find
// none of them: they are files in its host's directory that its host never asked
// for, and sockets nothing in the workspace speaks to.
func TestLocalOnlyDaemonBindsOnlyItsOwnSocket(t *testing.T) {
	d, socketPath := startTestDaemonWith(t, &DaemonConfig{Profile: ProfileLocalOnly})

	if d.linkListener != nil {
		t.Error("the local-only daemon bound the federation link socket")
	}
	if d.linkHumanListener != nil {
		t.Error("the local-only daemon bound the link-human socket")
	}
	if d.herdrListener != nil {
		t.Error("the local-only daemon bound the herdr protocol socket")
	}
	if d.transcriptWatcher != nil {
		t.Error("the local-only daemon created the transcript file watcher")
	}
	// The links are made on a client's attach, not at start, so the follow being
	// off is what keeps them from appearing later.
	if d.displayFollowing() {
		t.Error("the local-only daemon would follow an attached client's display")
	}
	if d.sshAgentFollowing() {
		t.Error("the local-only daemon would follow an attached client's ssh agent")
	}
	for what, path := range productSockets(socketPath) {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("the local-only daemon left the %s socket at %s", what, path)
		}
	}

	// One socket, and no symlink at all: the agent and display links the
	// standalone daemon keeps are symlinks beside the socket.
	entries, err := os.ReadDir(filepath.Dir(socketPath))
	if err != nil {
		t.Fatalf("read the socket directory: %v", err)
	}
	sockets := 0
	for _, e := range entries {
		if e.Type()&os.ModeSocket != 0 {
			sockets++
		}
		if e.Type()&os.ModeSymlink != 0 {
			t.Errorf("the local-only daemon left a link %q beside its socket", e.Name())
		}
		if strings.Contains(e.Name(), "agent-") || strings.Contains(e.Name(), "wayland-") {
			t.Errorf("the local-only daemon left a forwarding link %q beside its socket", e.Name())
		}
	}
	if sockets != 1 {
		t.Errorf("the local-only daemon opened %d sockets, want its own one", sockets)
	}

	// What it keeps still works: a session with a pane, created on the running
	// daemon exactly as an embedded host creates one.
	sess, err := d.manager.CreateSession("work", &SessionConfig{}, 80, 24)
	if err != nil {
		t.Fatalf("create a session on the local-only daemon: %v", err)
	}
	if _, err := sess.AddDaemonWindow("", nil); err != nil {
		t.Fatalf("add a pane on the local-only daemon: %v", err)
	}
}

// TestStandaloneDaemonKeepsItsProductSockets is the other half, and the reason
// the profile is a choice rather than the new default: the standalone product
// still binds the links its proxies and harnesses dial.
func TestStandaloneDaemonKeepsItsProductSockets(t *testing.T) {
	d, socketPath := startTestDaemonWith(t, &DaemonConfig{})

	if d.linkListener == nil {
		t.Error("the standalone daemon did not bind the federation link socket")
	}
	if d.linkHumanListener == nil {
		t.Error("the standalone daemon did not bind the link-human socket")
	}
	if d.transcriptWatcher == nil {
		t.Error("the standalone daemon did not create the transcript watcher")
	}
	for what, path := range productSockets(socketPath) {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the standalone daemon did not leave the %s socket at %s: %v", what, path, err)
		}
	}
}

// TestProfileNamesAreClosed keeps the profile from growing a value nothing
// reads: a daemon constructed with a profile this package does not define falls
// back to the standalone product, which is the wrong direction to be silent
// about in an embedding caller.
func TestProfileNamesAreClosed(t *testing.T) {
	if ProfileStandalone.LocalOnly() {
		t.Error("the zero profile must be the standalone product")
	}
	if !ProfileLocalOnly.LocalOnly() {
		t.Error("ProfileLocalOnly does not report itself as local-only")
	}
}
