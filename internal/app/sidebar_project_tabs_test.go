package app

import (
	"strings"
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/gitstatus"
	"github.com/getan/tuios-mini/internal/theme"
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

// TestFileDecorationMatchesNestedListing: git reports "cmd/test.go"; the row
// inside cmd/ says "test.go". Comparing the two by name left every nested file
// plain while its folder wore the roll-up — the folder changed and the file did
// not.
func TestFileDecorationMatchesNestedListing(t *testing.T) {
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
	m.filesView.Dir = "/repo/cmd"
	m.gitStatus = gitStatusView{
		Dir: "/repo/cmd",
		Status: gitstatus.Status{
			Root:     "/repo",
			Unstaged: []gitstatus.Entry{{Path: "cmd/test.go", Letter: gitstatus.Modified}},
		},
	}
	m.gitStatus.buildDecorations()
	if got := m.fileGitToken("test.go", false); got != "git_modified" {
		t.Fatalf("nested file decoration = %q, want git_modified", got)
	}
	// A name that is not the changed one stays plain.
	if got := m.fileGitToken("other.go", false); got != "" {
		t.Errorf("unrelated file painted: %q", got)
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
			Unstaged:  []gitstatus.Entry{{Path: "a.go", Letter: gitstatus.Modified}},
			Untracked: []gitstatus.Entry{{Path: "new.txt", Letter: gitstatus.Untracked}},
		},
	})
	m.projectTab = 1
	rows := m.gitTabRows()
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Kind != sidebarRowGitEntry {
			continue
		}
		seen[row.Name] = true
		if cmd := m.ActivateGitEntry(row.Index); cmd == nil {
			t.Fatalf("%s opened nothing", row.Name)
		}
	}
	for _, name := range []string{"a.go", "new.txt"} {
		if !seen[name] {
			t.Fatalf("%s missing from %+v", name, rows)
		}
	}
	// The last activation (untracked) must be a review of that path, not a
	// preview of the file.
	if m.review.query.Path != "new.txt" || m.review.query.Index != "" {
		t.Fatalf("untracked review query = %+v", m.review.query)
	}
	if m.docPreview.open {
		t.Fatal("a tracked or untracked change opened the file preview instead of the review")
	}
	// And a tracked row names the index side it came from.
	for _, row := range rows {
		if row.Kind == sidebarRowGitEntry && row.Name == "a.go" {
			m.ActivateGitEntry(row.Index)
		}
	}
	if m.review.query.Path != "a.go" || m.review.query.Index == "" {
		t.Fatalf("tracked review query = %+v", m.review.query)
	}
}

// TestDocPreviewHighlightsOnFirstOpen: a fresh buffer starts at Version 0,
// equal to the zero highlight generation, so the old gate skipped the first
// pass and a file stayed plain until it was edited.
func TestDocPreviewHighlightsOnFirstOpen(t *testing.T) {
	m, dir := previewOS(t)
	writeFile(t, dir, "sample.go", "package main\n\nfunc main() {}\n")
	openPreview(t, m, dir, "sample.go", false)
	m.renderDocBody(80, 10, theme.UI())
	if !m.docPreview.hlDone {
		t.Fatal("the first rendered frame skipped syntax highlighting")
	}
	spans := 0
	for _, line := range m.docPreview.hl {
		spans += len(line)
	}
	if spans == 0 {
		t.Fatalf("Go source produced no spans: %+v", m.docPreview.hl)
	}
	if m.docPreview.hlVer != m.docPreview.buf.Version() {
		t.Errorf("highlight cache version = %d, buffer = %d", m.docPreview.hlVer, m.docPreview.buf.Version())
	}
}
