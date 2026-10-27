package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/terminal"
	"tuios.dev/vt"
)

func cursorWrite(w *terminal.Window, text string) {
	w.LockIO()
	_, _ = w.Terminal.Write([]byte(text))
	w.UnlockIO()
	w.MarkContentDirty()
}

func TestSyncHoldIncludesGuestCursor(t *testing.T) {
	w := newTestWindow(t, "sync-cursor", 60, 20)
	m := newTestOS(w)
	m.Mode = TerminalMode
	cursorWrite(w, "\x1b[2J\x1b[HOLD FRAME\x1b[18;3H\x1b[6 q\x1b]12;#123456\x07")
	old := m.renderTerminal(w, true, true)
	pos, hidden, _ := w.GuestCursor()
	style, steady, col := w.CachedCursorStyle, w.CachedCursorSteady, w.CachedCursorColor
	cursorWrite(w, "\x1b[?2026h\x1b[5;10HX\x1b[2 q\x1b]12;#abcdef\x07")
	for i := 0; i < 3; i++ {
		w.InvalidateCache()
		got := m.renderTerminal(w, true, true)
		if got != old {
			t.Fatal("the body changed during an open synchronized update")
		}
		p, h, _ := w.GuestCursor()
		if p != pos || h != hidden || w.CachedCursorStyle != style || w.CachedCursorSteady != steady || !sameColor(col, w.CachedCursorColor) {
			t.Fatalf("half-frame cursor escaped: pos=%v hidden=%t style=%v steady=%t", p, h, w.CachedCursorStyle, w.CachedCursorSteady)
		}
	}
	cursorWrite(w, "\x1b[18;4H\x1b[?2026l")
	if out := m.renderTerminal(w, true, true); !strings.Contains(out, "X") {
		t.Fatal("completed frame never appeared")
	}
	p, _, _ := w.GuestCursor()
	if p.X != 3 || p.Y != 17 {
		t.Fatalf("completed cursor = %v", p)
	}
}

// Run the host-facing View, not just the VT accessor: a body-cache hit must
// not independently sample a newer cursor, even after 2026l has arrived.
func TestViewSyncCursorFrameConsistency(t *testing.T) {
	for _, fullscreen := range []bool{false, true} {
		for _, chunk := range []int{1, 8192} {
			t.Run(fmt.Sprintf("fullscreen=%t/chunk=%d", fullscreen, chunk), func(t *testing.T) {
				w, m := composedPane(t, "view-sync-cursor")
				m.Mode = TerminalMode
				if fullscreen {
					w.Resize(m.GetRenderWidth(), m.GetUsableHeight())
				}
				cursorWrite(w, "\x1b[H\x1b[2JOLD FRAME 中文🙂\x1b[18;3H\x1b[6 q\x1b]12;#123456\x07")
				old := m.View()
				if old.Cursor == nil {
					t.Fatal("fixture has no host cursor")
				}
				baseline := w.DisplayCursor()
				// The mode's CSI straddles an 8KiB parser boundary as well as
				// every byte boundary. BEL padding leaves the body unchanged.
				prefix := strings.Repeat("\x07", 8188) + "\x1b[?2026h"
				for start := 0; start < len(prefix); start += chunk {
					cursorWrite(w, prefix[start:min(start+chunk, len(prefix))])
				}
				cursorWrite(w, "\x1b[5;10HX\x1b[2 q\x1b]12;#abcdef\x07\x1b[?25l")
				for i := 0; i < 4; i++ {
					m.renderSkipped = i == 0
					if i > 0 {
						w.InvalidateCache()
					}
					got := m.View()
					if got.Cursor == nil || cursorState(got.Cursor) != cursorState(old.Cursor) {
						t.Fatalf("held body has another cursor: got=%+v want=%+v", got.Cursor, old.Cursor)
					}
					if w.DisplayCursor().Version != baseline.Version {
						t.Fatal("half-frame published a cursor version")
					}
					if strings.Contains(ansi.Strip(got.Content), "X") {
						t.Fatal("half-drawn body escaped")
					}
				}
				cursorWrite(w, "\x1b[18;4H\x1b[?25h\x1b[?2026l")
				// A skipped View must retain BOTH the old body and old cursor,
				// even though the parser now has a complete newer frame.
				m.renderSkipped = true
				if got := m.View(); cursorState(got.Cursor) != cursorState(old.Cursor) {
					t.Fatal("cached View sampled a newer cursor than its body")
				}
				m.renderSkipped = false
				w.InvalidateCache()
				got := m.View()
				if !strings.Contains(ansi.Strip(got.Content), "X") || got.Cursor == nil || got.Cursor.Position.X != old.Cursor.Position.X+1 {
					t.Fatalf("completed body/cursor never published: %+v", got.Cursor)
				}
			})
		}
	}
}

func TestFirstSyncFrameDrawsNeitherRealNorFakeCursor(t *testing.T) {
	w := newTestWindow(t, "first-sync-cursor", 60, 20)
	m := newTestOS(w)
	m.Mode = TerminalMode
	cursorWrite(w, "\x1b[?2026h\x1b[5;10HX")
	if out := m.renderTerminal(w, true, true); out != "" || countFakeCursors(out) != 0 {
		t.Fatalf("first half-frame drew content/a fake cursor: %q", out)
	}
	if c := m.getRealCursor(); c != nil {
		t.Fatalf("first half-frame drew a real cursor: %+v", c)
	}
	if c := m.getDisplayCursor(); c != nil {
		t.Fatalf("first half-frame committed a cursor: %+v", c)
	}
}

func TestSyncCursorHoldExpires(t *testing.T) {
	w := newTestWindow(t, "timeout-sync-cursor", 60, 20)
	m := newTestOS(w)
	m.Mode = TerminalMode
	cursorWrite(w, "\x1b[HOLD\x1b[18;3H")
	_ = m.renderTerminal(w, true, true)
	before := w.DisplayCursor()
	cursorWrite(w, "\x1b[?2026h\x1b[5;10HX")
	_ = m.renderTerminal(w, true, true)
	if w.DisplayCursor().Version != before.Version {
		t.Fatal("cursor published before the sync timeout")
	}
	time.Sleep(vt.SyncMaxHold + 10*time.Millisecond)
	w.MarkContentDirty()
	_ = m.renderTerminal(w, true, true)
	c := w.DisplayCursor()
	if c.Version <= before.Version || c.Position.Y != 4 {
		t.Fatalf("cursor frozen after sync timeout: %+v", c)
	}
}

func TestSyncCursorHoldSurvivesResizeAndFakeCursorFallback(t *testing.T) {
	w := newTestWindow(t, "resize-sync-cursor", 60, 20)
	m := newTestOS(w)
	m.Mode = TerminalMode
	cursorWrite(w, "\x1b[HOLD\x1b[18;3H")
	_ = m.renderTerminal(w, true, true)
	before := w.DisplayCursor()
	cursorWrite(w, "\x1b[?2026h\x1b[5;10HX")
	w.Resize(58, 19)
	// This normally takes the fake-cursor path. Sync must return the held
	// complete content before the cell loop can paint a live cursor into it.
	m.ShowScrollbackBrowser = true
	w.InvalidateCache()
	out := m.renderTerminal(w, true, true)
	if strings.Contains(out, "X") || countFakeCursors(out) != 0 {
		t.Fatal("resize/fake-cursor path painted the parser's intermediate state")
	}
	if w.DisplayCursor().Version != before.Version {
		t.Fatal("resize overwrote the held cursor")
	}
}
