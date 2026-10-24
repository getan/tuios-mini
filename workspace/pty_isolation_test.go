//go:build !windows

package workspace

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/x/xpty"

	"github.com/getan/tuios-mini/internal/app"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/testutil"
	"github.com/getan/tuios-mini/internal/theme"
	"github.com/getan/tuios-mini/mini"
	"github.com/getan/tuios-mini/pkg/applist"
)

// TestARealClientKeepsItsStateInItsOwnTree is the isolation claim proved the
// only way that counts: a real daemon, a real pty, and the tree the client
// leaves behind.
//
// It is the test for what the audit found. The config file was scoped and
// nothing else was: a client still created and read the person's themes and
// glyphs directories, still read and wrote their sidebar and launcher history,
// still left the process on the workspace's theme and glyph set, and still kept
// the process's log stream when --debug was on. Everything below is checked
// against a seeded sentinel tree and against the process's own globals, so a
// path that stops being scoped fails here rather than on someone's machine.
func TestARealClientKeepsItsStateInItsOwnTree(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a daemon and a pty")
	}
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh to run in the session's pane: %v", err)
	}

	// The person's own files, each with something in it that the client must
	// neither read nor write.
	userConfig := filepath.Join(xdg.ConfigHome, "tuios")
	userState := filepath.Join(xdg.StateHome, "tuios")
	writeFile(t, filepath.Join(userConfig, "config.toml"), "[appearance]\nborder_style = \"double\"\n")
	writeFile(t, filepath.Join(userConfig, "themes", "theirs.json"), `{"id":"theirs","fg":"#ffffff","bg":"#000000"}`)
	writeFile(t, filepath.Join(userConfig, "glyphs", "theirset.json"), `{"bullet":"T"}`)
	writeFile(t, filepath.Join(userState, "sidebar.json"), `{"order":["theirs"],"socket":"elsewhere"}`)
	writeFile(t, filepath.Join(userState, "launcher.json"), `{"version":1,"apps":{"theirs":{"n":3,"t":1}}}`)

	beforeUser := snapshot(t, xdg.ConfigHome, xdg.StateHome, xdg.DataHome)
	beforeSettings := config.Global
	beforeTheme, beforeGlyphs := theme.CurrentThemeID(), theme.ActiveGlyphSetID()
	beforeLauncher := applist.DefaultPath()
	beforeLog := log.Writer()

	dir := testutil.RuntimeDir(t)
	socket := filepath.Join(dir, "workspace.sock")
	daemonState := filepath.Join(dir, "daemon")
	clientState := filepath.Join(daemonState, "client")

	// The client's own tree, with a theme and a glyph set of its own to read:
	// the two directories a scoped client used to create in the person's config
	// tree whether or not anything named them.
	writeFile(t, filepath.Join(clientState, "config.toml"),
		"[appearance]\ntheme = \"mine\"\nglyphs = \"myset\"\n")
	writeFile(t, filepath.Join(clientState, "themes", "mine.json"),
		`{"id":"mine","fg":"#123456","bg":"#010101"}`)
	writeFile(t, filepath.Join(clientState, "glyphs", "myset.json"), `{"bullet":"M"}`)

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
		StateDir:   daemonState,
	})
	if err != nil {
		t.Fatalf("start the daemon: %v", err)
	}
	t.Cleanup(srv.Stop)

	pty, err := xpty.NewPty(100, 30)
	if err != nil {
		t.Skipf("no pty on this machine: %v", err)
	}
	t.Cleanup(func() { _ = pty.Close() })
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

	runResult := make(chan error, 1)
	go func() {
		runResult <- Run(ctx, Options{
			Socket:                socket,
			Session:               "work",
			Version:               "workspace-test",
			ConfigPath:            filepath.Join(clientState, "config.toml"),
			StateDir:              clientState,
			Width:                 100,
			Height:                30,
			Input:                 slave,
			Output:                slave,
			DisableSignalHandling: true,
			ASCIIOnly:             true,
			NoAnimations:          true,
		})
	}()

	var mu sync.Mutex
	drawn := 0
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				mu.Lock()
				drawn += n
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// Wait until the client has drawn, which is when everything it scopes is in
	// force and the first frame has been written after it.
	deadline := time.Now().Add(60 * time.Second)
	for {
		mu.Lock()
		got := drawn
		mu.Unlock()
		if got > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the client drew nothing in 60s")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// What is in force while the client runs. These four calls read the scopes
	// under their own locks, so asking from another goroutine is safe and is the
	// only way to see the process from the inside of a run.
	if got, ok := app.ScopedClientState(); !ok || got != clientState {
		t.Errorf("while the client runs its state directory is %q (scoped: %v), want %q", got, ok, clientState)
	}
	if got, ok := config.ScopedConfigPath(); !ok || got != filepath.Join(clientState, "config.toml") {
		t.Errorf("while the client runs its config is %q (scoped: %v), want %q", got, ok, filepath.Join(clientState, "config.toml"))
	}
	if themes, glyphs, ok := theme.ScopedAssetDirs(); !ok ||
		themes != filepath.Join(clientState, "themes") ||
		glyphs != filepath.Join(clientState, "glyphs") {
		t.Errorf("while the client runs its assets are %q and %q (scoped: %v), want them under %q",
			themes, glyphs, ok, clientState)
	}
	// The one set the client's own config named is in force, and the person's
	// is not on the list at all. Both reads take the registry's lock, so asking
	// from this goroutine is safe.
	if got := theme.ActiveGlyphSetID(); got != "myset" {
		t.Errorf("while the client runs the glyph set is %q, want the one in its own tree", got)
	}
	for _, id := range theme.AvailableGlyphSets() {
		if id == "theirset" {
			t.Error("the person's glyph set is visible to the running client")
		}
	}
	if log.Writer() == beforeLog {
		t.Error("while the client runs the process's log stream is not diverted")
	}

	// A cancelled context is the host asking the client to stop. It has to come
	// back, and with the cancellation rather than a plain nil.
	cancel()
	select {
	case err := <-runResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v after cancellation, want context.Canceled", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
	srv.Stop()

	// Everything the run took is given back.
	if _, ok := app.ScopedClientState(); ok {
		t.Error("the client state scope was left open")
	}
	if _, ok := config.ScopedConfigPath(); ok {
		t.Error("the config scope was left open")
	}
	if _, _, ok := theme.ScopedAssetDirs(); ok {
		t.Error("the theme asset scope was left open")
	}
	if log.Writer() != beforeLog {
		t.Error("the log stream was not given back")
	}
	if got := theme.CurrentThemeID(); got != beforeTheme {
		t.Errorf("the theme in force after the run is %q, want %q", got, beforeTheme)
	}
	if got := theme.ActiveGlyphSetID(); got != beforeGlyphs {
		t.Errorf("the glyph set in force after the run is %q, want %q", got, beforeGlyphs)
	}
	if got := applist.DefaultPath(); got != beforeLauncher {
		t.Errorf("the launcher history after the run is %q, want %q", got, beforeLauncher)
	}
	if !reflect.DeepEqual(config.Global, beforeSettings) {
		t.Error("the appearance globals were left changed")
	}

	// The person's tree is exactly as it was, down to the bytes: no sidebar
	// written, no launcher history taught, no themes or glyphs directory made.
	afterUser := snapshot(t, xdg.ConfigHome, xdg.StateHome, xdg.DataHome)
	if !reflect.DeepEqual(beforeUser, afterUser) {
		t.Errorf("the client wrote the person's own tuios tree:\n%s", diff(beforeUser, afterUser))
	}

	// The client kept its own, under the daemon's tree.
	if _, err := os.Stat(filepath.Join(clientState, "config.toml")); err != nil {
		t.Errorf("the client did not create its own config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clientState, "themes")); err != nil {
		t.Errorf("the client did not read themes from its own tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clientState, "glyphs")); err != nil {
		t.Errorf("the client did not read glyph sets from its own tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clientState, "debug.log")); !os.IsNotExist(err) {
		t.Errorf("a run with Debug off wrote a debug log: %v", err)
	}
}
