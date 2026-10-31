//go:build !windows

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/xpty"
	"github.com/getan/tuios-mini/internal/testutil"
	"github.com/getan/tuios-mini/mini"
)

// Exercise actual CSI-u input, decoding, routing and daemon PTY delivery, not
// hand-made key messages. Repeat after detach/attach against the same daemon.
// The ready markers are computed by the shell, not found in echoed commands;
// the artifact is the exact UTF-8 read by the pane, not a screen rendering.
func TestIMECommitThroughPTYAfterReattach(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a daemon and real PTYs")
	}
	t.Setenv("TERM", "xterm-256color")
	dir := testutil.RuntimeDir(t)
	socket := filepath.Join(dir, "ime.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv, err := mini.Start(ctx, mini.Config{
		SocketPath: socket, Session: "ime", Shell: "/bin/sh", Cwd: dir,
		Width: 100, Height: 30, Version: "ime-regression",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	for cycle := range 2 {
		t.Run(fmt.Sprintf("attach-%d", cycle), func(t *testing.T) {
			pty, err := xpty.NewPty(100, 30)
			if err != nil {
				t.Skipf("no local PTY: %v", err)
			}
			defer pty.Close()
			unix, ok := pty.(*xpty.UnixPty)
			if !ok || unix.Slave() == nil {
				t.Skip("no Unix PTY slave")
			}
			var mu sync.Mutex
			var output strings.Builder
			go func() {
				buf := make([]byte, 32768)
				for {
					n, err := pty.Read(buf)
					mu.Lock()
					output.Write(buf[:n])
					mu.Unlock()
					if err != nil {
						return
					}
				}
			}()
			snapshot := func() string { mu.Lock(); defer mu.Unlock(); return output.String() }
			runCtx, stop := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() {
				done <- Run(runCtx, Options{
					Socket: socket, Session: "ime", Version: "ime-regression",
					ConfigPath: filepath.Join(dir, "config.toml"), StateDir: filepath.Join(dir, "client"),
					Input: unix.Slave(), Output: unix.Slave(), Width: 100, Height: 30,
					DisableSignalHandling: true, ASCIIOnly: true, NoAnimations: true,
				})
			}()
			defer func() {
				stop()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Error("client did not detach")
				}
			}()
			wait := func(what string, ready func() bool) {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for !ready() {
					if time.Now().After(deadline) || ctx.Err() != nil {
						t.Fatalf("waiting for %s; output: %q", what, snapshot())
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			typeBytes := func(s string) {
				t.Helper()
				if _, err := pty.Write([]byte(s)); err != nil {
					t.Fatal(err)
				}
			}
			wait("client frame", func() bool { return strings.Contains(snapshot(), "\x1b[?2026h") })
			for i, seq := range []string{
				"\x1b[32;1;20320:22909u",
				"\x1b[13;1;20320:22909u",
				"\x1b[13;2;20320:22909u",
			} {
				file := filepath.Join(dir, fmt.Sprintf("commit-%d-%d.txt", cycle, i))
				marker := 100 + cycle*10 + i
				// Like golder, the pane asks for basic Kitty disambiguation.
				typeBytes(fmt.Sprintf("printf '\\033[=1;1u'; echo IME-$((%d+1))-READY; IFS= read -r text; printf '%%s' \"$text\" > %q; printf '\\033[=0;1u'\r", marker, file))
				wait("pane reader", func() bool {
					return strings.Contains(snapshot(), fmt.Sprintf("IME-%d-READY", marker+1))
				})
				typeBytes(seq + "\r")
				wait("committed text artifact", func() bool { _, err := os.Stat(file); return err == nil })
				got, err := os.ReadFile(file)
				if err != nil || string(got) != "你好" {
					t.Fatalf("pane read %q (%v), want 你好; artifact %s", got, err, file)
				}
				t.Logf("commit %d: pane read %q (UTF-8 %x)", i, got, got)
			}
		})
	}
}
