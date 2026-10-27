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
