package app

import (
	"testing"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/review"
	"github.com/getan/tuios-mini/internal/terminal"
)

func TestGitDrawerMoveAndRepositoryChoice(t *testing.T) {
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
	m.CurrentWorkspace = 1
	m.Windows = []*terminal.Window{{ID: "w1", Workspace: 1}, {ID: "w2", Workspace: 1}}
	m.FocusedWindow = 0
	m.gitDrawer = gitDrawerState{
		open: true, kind: review.HistoryRepos,
		rows: []string{"/repo/one\tw1\tone", "/repo/two\tw2\ttwo"},
	}

	m.GitDrawerMove(1)
	if m.gitDrawer.selected != 1 {
		t.Fatalf("selected = %d", m.gitDrawer.selected)
	}
	m.GitDrawerEdge(false)
	if m.gitDrawer.selected != 0 {
		t.Fatalf("home selected = %d", m.gitDrawer.selected)
	}
	m.GitDrawerMove(1)
	m.GitDrawerActivate()
	if m.gitDrawer.open {
		t.Fatal("the repository choice left the drawer open")
	}
	if m.FocusedWindow != 1 {
		t.Fatalf("focused window = %d, want the pane showing the chosen repo", m.FocusedWindow)
	}
}

func TestGitDrawerVisibleRowsStayInTheWindow(t *testing.T) {
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
	m.gitDrawer = gitDrawerState{open: true, rows: make([]string, 50)}
	m.GitDrawerEdge(true)
	if m.gitDrawer.selected != 49 {
		t.Fatalf("selected = %d", m.gitDrawer.selected)
	}
	if m.gitDrawer.scroll+gitDrawerVisibleRows() <= 49 {
		t.Fatalf("scroll %d did not follow the selection", m.gitDrawer.scroll)
	}
}

func TestGitDrawerCommitOpensALazyDetail(t *testing.T) {
	m := NewOS(OSOptions{UserConfig: config.DefaultConfig()})
	m.gitStatus.Origin.Session = "session"
	m.gitDrawer = gitDrawerState{
		open: true, kind: review.HistoryCommits,
		rows: []string{"abc123 2026-10-30 Author subject"},
	}
	m.GitDrawerActivate()
	if m.gitDrawer.kind != review.HistoryCommit || !m.gitDrawer.loading {
		t.Fatalf("drawer = %#v, want a loading commit detail", m.gitDrawer)
	}
	if len(m.gitDrawer.rows) != 0 {
		t.Fatalf("commit rows were fetched before the detail opened: %#v", m.gitDrawer.rows)
	}
	if m.gitDrawer.parent == nil || m.gitDrawer.parent.kind != review.HistoryCommits {
		t.Fatal("the commits list was not kept as the detail's parent")
	}
	m.GitDrawerClose()
	if m.gitDrawer.kind != review.HistoryCommits || len(m.gitDrawer.rows) != 1 {
		t.Fatalf("closing the detail lost the commits list: %#v", m.gitDrawer)
	}
}

func TestGitDrawerDetailKinds(t *testing.T) {
	tests := []struct {
		name string
		kind review.HistoryKind
		row  string
		want review.HistoryKind
		ref  string
	}{
		{"commit", review.HistoryCommits, "abc123 2026-10-30 Author subject", review.HistoryCommit, "abc123"},
		{"file", review.HistoryFile, "def456 2026-10-30 Author subject", review.HistoryCommit, "def456"},
		{"graph", review.HistoryGraph, "* abc7890 (HEAD -> main) subject", review.HistoryCommit, "abc7890"},
		{"branch", review.HistoryBranches, "feature origin/feature 2026-10-30 subject", review.HistoryBranch, "feature"},
		{"tag", review.HistoryTags, "v1.2.3 2026-10-30", review.HistoryTag, "v1.2.3"},
		{"stash", review.HistoryStashes, "stash@{0} 2026-10-30 WIP", review.HistoryStash, "stash@{0}"},
		{"remote", review.HistoryRemotes, "upstream  https://example.com/repo.git", review.HistoryRemote, "upstream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, ref := gitDrawerDetail(tt.kind, tt.row)
			if kind != tt.want || ref != tt.ref {
				t.Fatalf("detail = %q %q, want %q %q", kind, ref, tt.want, tt.ref)
			}
		})
	}
}
