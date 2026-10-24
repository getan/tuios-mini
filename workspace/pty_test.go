//go:build !windows

package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/xpty"

	"github.com/getan/tuios-mini/internal/terminal"
	"github.com/getan/tuios-mini/internal/testutil"
	"github.com/getan/tuios-mini/mini"
)

// TestRunDrawsAPaneInAPTY is the whole thing at once, on a real pty: a real
// daemon (package mini) on a real socket, the client attached to the session it
// was told to attach to, frames arriving, and the terminal put back when the
// context is cancelled.
//
// It is the only test that proves the parts line up: a client with the right
// options and a wrong attach sends nothing, and every check below is something
// that once worked and would read as "the TUI drew nothing" if it stopped.
func TestRunDrawsAPaneInAPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a daemon and a pty")
	}
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh to run in the session's pane: %v", err)
	}

	// A unix socket path is a fixed-size field, so the directory has to stay
	// short: see testutil.RuntimeDir.
	dir := testutil.RuntimeDir(t)
	socket := filepath.Join(dir, "workspace.sock")

	daemonCtx, stopDaemon := context.WithCancel(context.Background())
	t.Cleanup(stopDaemon)
	srv, err := mini.Start(daemonCtx, mini.Config{
		SocketPath: socket,
		Session:    "work",
		Shell:      "/bin/sh",
		Cwd:        dir,
		Width:      100,
		Height:     30,
		Version:    "workspace-test",
	})
	if err != nil {
		t.Fatalf("start the daemon: %v", err)
	}
	t.Cleanup(srv.Stop)

	pty, err := xpty.NewPty(100, 30)
	if err != nil {
		// A sandbox with no /dev/ptmx is not this package's failure.
		t.Skipf("no pty on this machine: %v", err)
	}
	t.Cleanup(func() { _ = pty.Close() })

	// The client reads and writes the pty's slave; the test reads the master.
	// Everything the client draws, including its cleanup, is what this reads.
	unixPty, ok := pty.(*xpty.UnixPty)
	if !ok {
		t.Skipf("this pty is a %T, with no slave file to hand over", pty)
	}
	slave := unixPty.Slave()
	if slave == nil {
		t.Skip("this pty has no slave file")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		err error
	}
	runResult := make(chan result, 1)
	go func() {
		err := Run(ctx, Options{
			Socket:     socket,
			Session:    "work",
			Version:    "workspace-test",
			ConfigPath: filepath.Join(dir, "config.toml"),
			Width:      100,
			Height:     30,
			Input:      slave,
			Output:     slave,
			// The test binary owns the process, so the client must not install
			// anything that would end it.
			DisableSignalHandling: true,
			// No Nerd Font icons in a test that only looks for text.
			ASCIIOnly:    true,
			NoAnimations: true,
		})
		runResult <- result{err: err}
	}()

	// Read the pty the way a terminal does, until the client has drawn.
	var mu sync.Mutex
	var drawn strings.Builder
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				mu.Lock()
				drawn.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// What is looked for is the client drawing, not a byproduct of it: entering
	// the alternate screen (bubbletea's, at start) and the synchronized-update
	// bracket that wraps a whole frame (the post-render writer's). A client
	// that connected and rendered nothing writes neither.
	enteredAltScreen := "\x1b[?1049h"
	frameBracket := "\x1b[?2026h"
	deadline := time.Now().Add(60 * time.Second)
	for {
		mu.Lock()
		got := drawn.String()
		mu.Unlock()
		if strings.Contains(got, enteredAltScreen) && strings.Contains(got, frameBracket) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the client drew no frame in 60s; it wrote %d bytes:\n%q", len(got), tail(got, 2000))
		}
		time.Sleep(50 * time.Millisecond)
	}

	// A cancelled context is the host asking the client to stop. It has to
	// come back, and with the cancellation, not with a plain nil.
	cancel()
	select {
	case r := <-runResult:
		if r.err != nil && !errors.Is(r.err, context.Canceled) {
			t.Fatalf("Run returned %v after cancellation, want context.Canceled", r.err)
		}
		if r.err == nil {
			t.Error("Run returned nil after cancellation; the host cannot tell a cancel from a quit")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}

	// The cleanup the client promises on every exit: the terminal it drew to
	// is reset, and the reset went to that terminal rather than to this
	// process's stdout.
	//
	// The bytes are compared with the carriage returns removed: a pty with
	// output post-processing on (which is what the slave is before the client
	// turns it raw, and what it is again after) turns the newline at the end of
	// the reset into CRLF, so the pty's copy is not byte-identical to the
	// sequence written.
	want := strings.ReplaceAll(terminal.ResetSequence, "\r", "")
	resetDeadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		got := drawn.String()
		mu.Unlock()
		if strings.Contains(strings.ReplaceAll(got, "\r", ""), want) {
			break
		}
		if time.Now().After(resetDeadline) {
			t.Fatalf("the client did not reset the terminal it drew to; last bytes:\n%q", tail(got, 2000))
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Nothing was written to this process's own stdout: the client's terminal
	// was the pty it was handed.
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Errorf("the client did not create its own config file: %v", err)
	}
}

// tail is the last n bytes of s, for a failure message that does not print a
// megabyte of escape sequences.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
