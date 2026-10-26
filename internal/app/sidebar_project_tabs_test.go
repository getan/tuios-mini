package app

import (
	"strings"
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/gitstatus"
)

// TestProjectTabsShareOneLineWithTwoTargets pins the layout: Files and Git are
// two icons on one row, and each publishes its own click target. Two full-width
// rows spent two lines of the rail saying what two shapes say on one.
func TestProjectTabsShareOneLineWithTwoTargets(t *testing.T) {
	m := sidebarTestOS(t, 120, 40, "left")
	dir := t.TempDir()
	m.filesView = fileViewState{Show: 1, Dir: dir, Want: dir}
	if _, w := m.sidebarPanelLines(); w <= 0 {
		t.Fatal("rail did not draw")
	}

	var files, git *sidebarRowHit
	for i := range m.SidebarHits {
		switch m.SidebarHits[i].Kind {
		case sidebarRowProjectFiles:
			files = &m.SidebarHits[i]
		case sidebarRowProjectGit:
			git = &m.SidebarHits[i]
		}
	}
	if files == nil || git == nil {
		t.Fatalf("tab targets missing: files=%v git=%v", files, git)
	}
	if files.Y0 != git.Y0 || files.Y1 != git.Y1 {
		t.Fatalf("tabs are not on one line: files y=%d..%d git y=%d..%d",
			files.Y0, files.Y1, git.Y0, git.Y1)
	}
	if files.X0 >= files.X1 || git.X0 >= git.X1 {
		t.Fatalf("empty tab target: files=%+v git=%+v", files, git)
	}
	if files.X1 > git.X0 && git.X1 > files.X0 {
		t.Fatalf("tab targets overlap: files=%+v git=%+v", files, git)
	}

	// Each half switches to its own view, and neither needs a label to do it.
	if m.projectTab != 0 {
		t.Fatalf("fixture starts on tab %d", m.projectTab)
	}
	m.SidebarClick(git.X0, git.Y0, false)
	if m.projectTab != 1 {
		t.Fatalf("clicking the git icon left the tab at %d", m.projectTab)
	}
	m.SidebarClick(files.X0, files.Y0, false)
	if m.projectTab != 0 {
		t.Fatalf("clicking the files icon left the tab at %d", m.projectTab)
	}
}

// TestGitDecorationsRollUpToFolders: a folder whose contents changed must show
// it, with the most urgent status under it winning. A tree that only coloured
// the unchanged parent would hide every change inside a folded directory.
func TestGitDecorationsRollUpToFolders(t *testing.T) {
	v := gitStatusView{Status: gitstatus.Status{
		Root:   "/repo",
		Branch: "main",
		Staged: []gitstatus.Entry{
			{Path: "src/a/b.go", Letter: gitstatus.Modified},
		},
		Unstaged: []gitstatus.Entry{
			{Path: "src/deep/c.go", Letter: gitstatus.Deleted},
		},
		Untracked: []gitstatus.Entry{
			{Path: "docs/new.md", Letter: gitstatus.Untracked},
		},
		Conflicts: []gitstatus.Entry{
			{Path: "src/merge.go", Letter: gitstatus.Conflicted},
		},
	}}
	v.buildDecorations()

	if got := v.fileDeco["src/a/b.go"]; got != "git_modified" {
		t.Errorf("file decoration = %q", got)
	}
	if got := v.dirDeco["src/a"]; got != "git_modified" {
		t.Errorf("src/a = %q", got)
	}
	if got := v.dirDeco["docs"]; got != "git_untracked" {
		t.Errorf("docs = %q", got)
	}
	// The conflict wins over the deletion and the modification under src.
	if got := v.dirDeco["src"]; got != "git_conflict" {
		t.Errorf("src = %q, want the strongest child", got)
	}

	// A reading for another directory must not paint this one's rows.
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
	m.filesView.Dir = "/elsewhere"
	m.gitStatus = v
	if got := m.fileGitToken("src", true); got != "" {
		t.Errorf("stale reading painted a row: %q", got)
	}
}

// TestReviewVerbsDialTheHostSocket: a workspace owns its daemon, and LocalSocket
// names it. Review (ctrl+b v) used to dial the default socket instead and fail
// with "/tmp/tuios-501/tuios.sock: no such file" while the session was healthy.
func TestReviewVerbsDialTheHostSocket(t *testing.T) {
	m := NewOS(OSOptions{
		UserConfig:  config.DefaultConfig(),
		LocalSocket: "/nonexistent/golder-ws-default.sock",
	})
	_, err := m.inboxCaller()("review-diff", map[string]any{}, time.Second)
	if err == nil {
		t.Fatal("dialing a missing host socket succeeded")
	}
	if !strings.Contains(err.Error(), "golder-ws-default.sock") {
		t.Fatalf("review dialed the wrong socket: %v", err)
	}
}

// TestGitEntryClickRequestsAScopedReview: a tracked change opens the diff, not
// the file preview. Untracked paths are the only ones with nothing to diff.
func TestGitEntryClickRequestsAScopedReview(t *testing.T) {
	m, dir := previewOS(t)
	m.IsDaemonSession = true
	m.filesView = fileViewState{Show: 1, Dir: dir, Want: dir}
	origin := gitStatusOrigin{Session: "s", Window: "w1", Dir: dir}
	m.gitStatus = gitStatusView{gen: 1, asked: origin}
	m.ApplyGitStatus(GitStatusMsg{
		Gen: 1, Origin: origin,
		Status: gitstatus.Status{
			Root: dir, Branch: "main",
			Unstaged: []gitstatus.Entry{{Path: "a.go", Letter: gitstatus.Modified}},
		},
	})
	m.projectTab = 1
	rows := m.gitTabRows()
	found := false
	for _, row := range rows {
		if row.Kind != sidebarRowGitEntry || row.Name != "a.go" {
			continue
		}
		found = true
		if cmd := m.ActivateGitEntry(row.Index); cmd == nil {
			t.Fatal("a modified file opened nothing")
		}
	}
	if !found {
		t.Fatalf("modified entry missing from %+v", rows)
	}
	if m.docPreview.open {
		t.Fatal("a tracked change opened the file preview instead of the review")
	}
	if got := m.review.query.Path; got != "a.go" {
		t.Fatalf("review path = %q", got)
	}
	if m.review.query.Index == "" {
		t.Fatal("review request did not name the index side")
	}
}
