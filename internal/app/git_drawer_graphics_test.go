package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/review"
	"github.com/getan/tuios-mini/internal/terminal"
	"tuios.dev/vt"
)

func gitDrawerGraphicsOS(t *testing.T, kp *KittyPassthrough, id string) *OS {
	t.Helper()
	w := newTestWindow(t, id, 100, 30)
	w.Workspace = 1
	return &OS{
		Settings: config.Global, Width: 200, Height: 60, CurrentWorkspace: 1,
		Windows: []*terminal.Window{w}, KittyPassthrough: kp,
	}
}

// The normal compositor z-index cannot put text above Kitty graphics. Check
// the actual View graphics-flush path and host protocol, not only the popup's
// text. Failure modes: missing hide gate, a repeated flush showing the image,
// loss of resident data, or the image staying hidden after the drawer closes.
func TestGitDrawersHideAndRestoreBrowserPlacements(t *testing.T) {
	h := newOcclusionHarness(t)
	h.kp.inlineGraphics = false // native Kitty terminal, not the web CSS overlay
	m := gitDrawerGraphicsOS(t, h.kp, h.winID)
	m.flushGraphicsForView()
	if !h.host.has("a=p,i=") {
		t.Fatal("fixture did not place the browser image")
	}
	for _, kind := range []review.HistoryKind{
		review.HistoryCommits, review.HistoryGraph, review.HistoryBranches,
		review.HistoryTags, review.HistoryStashes, review.HistoryRemotes,
		review.HistoryFile, review.HistoryRepos, review.HistoryCommit,
		review.HistoryBranch, review.HistoryTag, review.HistoryStash, review.HistoryRemote,
	} {
		t.Run(string(kind), func(t *testing.T) {
			hidden := h.host.count("a=d,d=i,i=")
			m.gitDrawer = gitDrawerState{open: true, kind: kind, loading: true}
			m.flushGraphicsForView()
			if got := h.host.count("a=d,d=i,i="); got != hidden+1 {
				t.Fatalf("opening %s did not hide the browser image: deletes %d -> %d", kind, hidden, got)
			}
			shown := h.host.count("a=p,i=")
			m.gitDrawer.loading, m.gitDrawer.err = false, "read failed"
			m.flushGraphicsForView()
			if got := h.host.count("a=p,i="); got != shown {
				t.Fatal("an error/loading-state redraw put the browser over the drawer")
			}
			m.GitDrawerClose()
			m.flushGraphicsForView()
			if got := h.host.count("a=p,i="); got != shown+1 {
				t.Fatalf("closing %s did not restore the browser without a new frame", kind)
			}
		})
	}
	if h.host.has("a=d,d=I,") {
		t.Fatal("drawer destroyed resident image data instead of hiding the placement")
	}
}

// Browser video bypasses the normal placement list. Drive the same async
// frame wire as a live browser while the drawer opens, returns to its parent,
// and closes. The old UI gate did not hide it or stop new frames, although
// calling SetOverlayActive directly (the old tests) worked perfectly.
func TestGitDrawerBlocksLiveBrowserUntilLastOverlayCloses(t *testing.T) {
	withClientCaps(t, &HostCapabilities{
		KittyGraphics: true, TerminalName: "kitty", CellWidth: 10, CellHeight: 20,
	})
	rec := &recWriter{}
	kp := NewKittyPassthroughWithOptions(KittyPassthroughOptions{Output: rec, RemoteClient: true})
	const winID = "git-browser"
	t.Cleanup(func() { kp.OnWindowClose(winID) })
	// File transport reaches the same async browser-video path as shared
	// memory, without skipping this regression on macOS (no /dev/shm).
	path := filepath.Join(kittyTestTempDir(t), "browser.rgba")
	writeFrame := func(seed byte) {
		t.Helper()
		data := make([]byte, 400*300*4)
		for i := range data {
			data[i] = seed + byte(i)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := &vt.KittyCommand{
		Action: vt.KittyActionTransmitPlace, Medium: vt.KittyMediumFile,
		Format: vt.KittyFormatRGBA, ImageID: 1, Width: 400, Height: 300, FilePath: path,
	}
	raw := []byte("\x1b_Ga=T,t=f,i=1,f=32,s=400,v=300;" + b64(path) + "\x1b\\")
	send := func() { kp.ForwardCommand(cmd, raw, winID, 0, 0, 98, 28, 1, 1, 0, 0, 0, false, nil) }
	writeFrame(0)
	send()
	if !sendUntil(t, send, func() bool { return rec.has("a=T,i=") }, 2*time.Second) {
		t.Fatal("fixture did not start the browser stream")
	}
	m := gitDrawerGraphicsOS(t, kp, winID)
	m.flushGraphicsForView()
	hidden := rec.count("a=d,d=i,i=")
	parent := gitDrawerState{open: true, kind: review.HistoryCommits, rows: []string{"abc1234 subject"}}
	m.gitDrawer = gitDrawerState{open: true, kind: review.HistoryCommit, loading: true, parent: &parent}
	m.flushGraphicsForView()
	if got := rec.count("a=d,d=i,i="); got != hidden+1 {
		t.Fatalf("Git detail stayed behind live browser: deletes %d -> %d", hidden, got)
	}
	shown, streamed := rec.count("a=p,i="), rec.count("a=T,i=")
	for range 3 {
		writeFrame(55)
		send()
		m.flushGraphicsForView()
	}
	// Give the async frame writer time to expose any frame that escaped the
	// overlay gate; nothing may have been queued by the sends above.
	time.Sleep(50 * time.Millisecond)
	if rec.count("a=T,i=") != streamed || rec.count("a=p,i=") != shown {
		t.Fatal("a live browser frame painted over the open Git detail")
	}
	m.GitDrawerClose() // back to the list, not out of the overlay
	m.flushGraphicsForView()
	if !m.GitDrawerOpen() || rec.count("a=p,i=") != shown {
		t.Fatal("returning to the parent list exposed the browser")
	}
	m.ShowCommandPalette = true
	m.GitDrawerClose()
	m.flushGraphicsForView()
	if rec.count("a=p,i=") != shown {
		t.Fatal("closing Git restored the browser through the still-open command palette")
	}
	m.ShowCommandPalette = false
	m.flushGraphicsForView()
	if rec.count("a=p,i=") != shown+1 {
		t.Fatal("browser did not return immediately when the last popup closed")
	}
	writeFrame(200)
	if !sendUntil(t, send, func() bool { return rec.count("a=T,i=") > streamed }, 2*time.Second) {
		t.Fatal("browser stream did not resume after closing the popups")
	}
	rec.mu.Lock()
	protocol := rec.buf.String()
	rec.mu.Unlock()
	if strings.Contains(protocol, "a=d,d=I,") {
		t.Fatal("hiding the browser freed its image data")
	}
	t.Logf("Git/browser lifecycle: hide=%d restore=%d resumed_frames=%d", rec.count("a=d,d=i,i="), rec.count("a=p,i="), rec.count("a=T,i="))
}
