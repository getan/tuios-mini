package app

import (
	"strings"
	"testing"

	"github.com/getan/tuios-mini/internal/terminal"
)

// dockTabTestOS is an OS with one window per named workspace, wide enough to
// draw a full dock.
func dockTabTestOS(t testing.TB, current int, workspaces ...int) *OS {
	t.Helper()
	wins := make([]*terminal.Window, 0, len(workspaces))
	for i, ws := range workspaces {
		w := newTestWindow(t, "dock-tab-"+strings.Repeat("x", i+1), 60, 20)
		w.Workspace = ws
		wins = append(wins, w)
	}
	m := newTestOS(wins[0])
	m.Windows = wins
	m.Width, m.Height = 160, 40
	m.CurrentWorkspace = current
	return m
}

// TestDockWorkspaceAddTabOpensTheNextFreeOne pins the "+" affordance: making a
// workspace should be a click, not a remembered keybind. It also means a single
// occupied workspace still draws a strip, because "1 +" is worth showing.
func TestDockWorkspaceAddTabOpensTheNextFreeOne(t *testing.T) {
	m := dockTabTestOS(t, 1, 1, 1)
	m.renderDockString()
	if len(m.dockWorkspaceHits) != 2 {
		t.Fatalf("one workspace plus the add tab should draw 2 tabs, got %d", len(m.dockWorkspaceHits))
	}
	add := m.dockWorkspaceHits[len(m.dockWorkspaceHits)-1]
	if add.Workspace != 0 {
		t.Fatalf("the trailing tab should be the add tab (workspace 0), got %d", add.Workspace)
	}
	// Clicking it resolves to a real, empty workspace.
	if got := m.DockWorkspaceAt(add.X0, add.Y); got != 2 {
		t.Fatalf("the add tab resolved to workspace %d, want the next free one (2)", got)
	}
}

// TestDockWorkspaceAddTabSkipsNamedEmptyWorkspaces keeps a name as a claim on
// the workspace. A named workspace with no panes is reachable from the switcher
// and must not be handed to the unrelated tab the "+" creates.
func TestDockWorkspaceAddTabSkipsNamedEmptyWorkspaces(t *testing.T) {
	m := dockTabTestOS(t, 1, 1)
	m.NumWorkspaces = 3
	m.WorkspaceNames = map[int]string{2: "mole"}

	if got := m.nextFreeWorkspace(); got != 3 {
		t.Fatalf("the + tab chose named workspace 2, got %d", got)
	}

	m.WorkspaceNames[3] = "trader"
	if got := m.nextFreeWorkspace(); got != 0 {
		t.Fatalf("the + tab chose a reserved workspace, got %d", got)
	}
	m.renderDockString()
	for _, h := range m.dockWorkspaceHits {
		if h.Workspace == 0 {
			t.Fatal("the add tab is drawn while every free workspace is named")
		}
	}
}

// TestDockWorkspaceAddTabHidesWhenFull keeps the strip honest: with every
// workspace in use there is nothing to add.
func TestDockWorkspaceAddTabHidesWhenFull(t *testing.T) {
	m := dockTabTestOS(t, 1, 1, 1)
	m.NumWorkspaces = 1
	m.renderDockString()
	for _, h := range m.dockWorkspaceHits {
		if h.Workspace == 0 {
			t.Fatal("the add tab is drawn with no free workspace to open")
		}
	}
}

// TestDockWorkspaceTabsAreUniformWidth keeps the strip from reflowing under the
// pointer: switching workspace must not move the other tabs.
func TestDockWorkspaceTabsAreUniformWidth(t *testing.T) {
	m := dockTabTestOS(t, 1, 1, 2, 3)
	m.renderDockString()
	before := append([]dockWorkspaceHit(nil), m.dockWorkspaceHits...)

	m.CurrentWorkspace = 3
	m.renderDockString()

	if len(before) != len(m.dockWorkspaceHits) {
		t.Fatalf("tab count changed on a switch: %d then %d", len(before), len(m.dockWorkspaceHits))
	}
	for i, h := range m.dockWorkspaceHits {
		if h.X0 != before[i].X0 || h.X1 != before[i].X1 {
			t.Errorf("tab %d moved from [%d,%d) to [%d,%d) on a switch",
				h.Workspace, before[i].X0, before[i].X1, h.X0, h.X1)
		}
	}
}
