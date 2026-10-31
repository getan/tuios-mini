package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/overlay"
	"github.com/getan/tuios-mini/internal/review"
	"github.com/getan/tuios-mini/internal/theme"
)

// Deterministic input/render regression: wheel, click-away and returning from
// an in-flight detail were missing from the mouse-only Git path. A restored
// parent's generation must not be reused by a second detail's reply.
func TestGitDrawerMouseNavigationAndLateReplies(t *testing.T) {
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig(), Width: 100, Height: 35})
	t.Cleanup(m.Cleanup)
	origin := gitStatusOrigin{Session: "work", Window: "original"}
	m.gitDrawer = gitDrawerState{open: true, gen: 10, kind: review.HistoryCommits, title: "commits", origin: origin}
	for i := range 40 {
		m.gitDrawer.rows = append(m.gitDrawer.rows, fmt.Sprintf("abc%04d subject", i))
	}
	paint := func() overlay.Geometry {
		_, geo, rows := m.renderGitDrawer()
		m.OverlayZOrder = []string{"gitdrawer"}
		m.OverlayHits = []overlayPanelHit{{Kind: "gitdrawer", OriginX: 10, OriginY: 5, Geo: geo, Rows: rows}}
		return geo
	}
	geo := paint()
	for range 25 {
		if !m.OverlayMouseWheel(12, 7, false) {
			t.Fatal("Git drawer did not consume the wheel")
		}
	}
	if m.gitDrawer.selected != 25 || m.gitDrawer.scroll == 0 {
		t.Fatalf("wheel did not scroll: %#v", m.gitDrawer)
	}
	geo = paint()
	handled, cmd := m.OverlayMouseClick(12, 5+geo.BodyY, false)
	if !handled || cmd == nil || m.gitDrawer.kind != review.HistoryCommit {
		t.Fatal("click did not open the selected commit")
	}
	if m.gitDrawer.origin != origin {
		t.Fatal("detail lost its repository origin")
	}
	oldGen := m.gitDrawer.gen
	paint()
	m.OverlayMouseClick(0, 0, false)
	if !m.gitDrawer.open || m.gitDrawer.kind != review.HistoryCommits || m.gitDrawer.scroll == 0 {
		t.Fatal("click-away did not restore the scrolled parent list")
	}
	m.GitDrawerActivate()
	if m.gitDrawer.gen == oldGen {
		t.Fatal("a second detail reused the cancelled request's generation")
	}
	m.Update(gitHistoryMsg{Gen: oldGen, Rows: []string{"stale reply"}})
	if !m.gitDrawer.loading || len(m.gitDrawer.rows) != 0 {
		t.Fatal("a cancelled detail overwrote the newer one")
	}
	m.GitDrawerClose()
	paint()
	m.OverlayMouseClick(0, 0, false)
	if m.GitDrawerOpen() {
		t.Fatal("click-away did not close the list")
	}
}

func TestGitViewsDoNotAdvertiseKeyboardShortcuts(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewOS(OSOptions{UserConfig: cfg, KeybindRegistry: config.NewKeybindRegistry(cfg), Width: 100, Height: 35})
	t.Cleanup(m.Cleanup)
	m.SidebarFocused, m.projectTab = true, 1
	m.SidebarNav = []sidebarNavRow{{Kind: sidebarRowGitEntry}}
	lines, _ := m.sidebarFooter(sidebarVariantFull, 26, theme.UI(), -1, -1, func(sidebarRowKind) bool { return false })
	if len(lines) != 1 {
		t.Fatalf("Git footer still reserves %d lines for key hints", len(lines))
	}
	if _, hints := m.keybarHints(); len(hints) != 0 {
		t.Fatal("Git tab keybar still advertises shortcuts")
	}
	for _, kind := range []review.HistoryKind{review.HistoryCommits, review.HistoryCommit, review.HistoryBranches, review.HistoryRepos} {
		m.gitDrawer = gitDrawerState{open: true, kind: kind, title: string(kind), rows: []string{"example"}}
		content, _, _ := m.renderGitDrawer()
		text := ansi.Strip(content + m.keybarLine())
		for _, key := range []string{"j/k", "ctrl+c", "enter", "esc"} {
			if strings.Contains(text, key) {
				t.Errorf("%s advertises %q: %s", kind, key, text)
			}
		}
	}
}

func TestGitDrawerButtonsMatchRenderedTargets(t *testing.T) {
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig(), Width: 100, Height: 35})
	t.Cleanup(m.Cleanup)
	for _, state := range []gitDrawerState{
		{open: true, kind: review.HistoryCommits, loading: true},
		{open: true, kind: review.HistoryCommit, err: "error", parent: &gitDrawerState{open: true, kind: review.HistoryCommits}},
		{open: true, kind: review.HistoryCommit, rows: []string{"detail"}, parent: &gitDrawerState{open: true, kind: review.HistoryCommits}},
	} {
		m.gitDrawer = state
		content, geo, rows := m.renderGitDrawer()
		lines := strings.Split(ansi.Strip(content), "\n")
		m.OverlayZOrder = []string{"gitdrawer"}
		m.OverlayHits = []overlayPanelHit{{Kind: "gitdrawer", Geo: geo, Rows: rows}}
		for _, row := range rows {
			if row.Idx >= 0 {
				continue
			}
			label := "[close]"
			if row.Idx == gitDrawerBackRow {
				label = "[back]"
			}
			line := lines[row.Rect.Y0]
			at := strings.Index(line, label)
			if at < 0 {
				t.Fatalf("missing button %s: %q", label, line)
			}
			x := ansi.StringWidth(line[:at])
			if row.Rect.X0 != x || row.Rect.X1 != x+len(label) {
				t.Fatalf("%s target %v differs from the drawn button: %q", label, row.Rect, lines[row.Rect.Y0])
			}
			m.OverlayMouseMotion(x, row.Rect.Y0)
			if m.gitDrawer.selected != state.selected {
				t.Fatal("hovering a navigation button moved the selected history row")
			}
			m.gitDrawer = state
			m.OverlayMouseClick(x, row.Rect.Y0, false)
			if row.Idx == gitDrawerBackRow && (!m.gitDrawer.open || m.gitDrawer.kind != review.HistoryCommits) {
				t.Fatal("back did not restore the list")
			}
			if row.Idx == gitDrawerCloseRow && m.gitDrawer.open {
				t.Fatal("close did not dismiss the whole drawer")
			}
			m.gitDrawer = state
		}
	}
}
