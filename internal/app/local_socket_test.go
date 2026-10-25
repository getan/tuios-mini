package app

import (
	"path/filepath"
	"testing"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/session"
	"github.com/getan/tuios-mini/internal/testutil"
)

// A workspace embeds its daemon and passes it a private socket. Rename used to
// call session.DialVerbClient(), which resolves the host's default socket
// instead: in a workspace that is a different path (on macOS often
// /tmp/tuios-<uid>/tuios.sock) where no daemon listens, so the editor failed
// with "failed to connect to daemon" while the running session was healthy.
func TestWorkspaceRenameUsesTheHostsDaemonSocket(t *testing.T) {
	dir := testutil.RuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	socket := filepath.Join(dir, "workspace.sock")

	d := session.NewDaemon(&session.DaemonConfig{
		Version: "test", SocketPath: socket, StateDir: t.TempDir(), DisableAutoRestore: true,
	})
	if err := d.Start(); err != nil {
		t.Fatalf("daemon start: %v", err)
	}
	t.Cleanup(d.Stop)

	client := session.NewTUIClient()
	if err := client.ConnectSocket(socket, "test", 80, 24, nil); err != nil {
		t.Fatalf("connect to the workspace daemon: %v", err)
	}
	if _, err := client.AttachSession("work", true, 80, 24); err != nil {
		t.Fatalf("attach: %v", err)
	}
	client.StartReadLoop()
	t.Cleanup(func() { _ = client.Close() })

	model := NewOS(OSOptions{
		UserConfig:      config.DefaultConfig(),
		IsDaemonSession: true,
		DaemonClient:    client,
		LocalSocket:     socket,
		SessionName:     "work",
		Width:           80,
		Height:          24,
	})
	model.BeginRenameSession("work")
	model.RenameBuffer = "renamed"
	cmd := model.CommitRename()
	if cmd == nil {
		t.Fatal("rename produced no command")
	}
	msg, ok := cmd().(RenameAppliedMsg)
	if !ok || msg.Err != nil {
		t.Fatalf("rename = %#v, want success", msg)
	}

	list, err := client.RefreshSessionList()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	for _, info := range list {
		if info.Name == "renamed" {
			return
		}
	}
	t.Fatalf("sessions = %+v, want renamed", list)
}
