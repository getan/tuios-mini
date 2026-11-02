package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/terminal"
)

func paneControlTestOS(t *testing.T) (*OS, string, *terminal.Window) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "tuios.sock")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	win := &terminal.Window{ID: "pane-1", Workspace: 1, ForegroundCmd: "zsh"}
	m := &OS{
		Settings: config.DefaultSettings(), IsDaemonSession: true,
		LocalSocket: socket, SessionName: "work", CurrentWorkspace: 1,
		Width: 120, Height: 40, Windows: []*terminal.Window{win},
	}
	return m, socket, win
}

func paneControlTestAgent(id, label string) paneControlAgent {
	return paneControlAgent{
		ID: id, Label: label, Project: label, Role: "main", ConversationID: "conversation-" + id,
		SessionID: "work", PaneID: "agent-" + id, Workspace: 1, Mode: "ask", PID: 42, UpdatedAt: time.Now().UTC(),
	}
}

func writePaneControlTestState(t *testing.T, socket string, agents ...paneControlAgent) {
	t.Helper()
	st := paneControlFile{Version: paneControlVersion, Agents: map[string]paneControlAgent{}, Leases: map[string]paneControlLease{}}
	for _, agent := range agents {
		st.Agents[agent.ID] = agent
	}
	if err := savePaneControl(paneControlPath(socket), st); err != nil {
		t.Fatal(err)
	}
}

func addPaneControlAgentWindows(m *OS, agents ...paneControlAgent) {
	for _, agent := range agents {
		m.Windows = append(m.Windows, &terminal.Window{ID: agent.PaneID, Workspace: agent.Workspace, AgentState: "idle"})
	}
}

func menuHasLabel(items []ContextMenuItem, part string) bool {
	for _, item := range items {
		if strings.Contains(item.Label, part) {
			return true
		}
	}
	return false
}

func runPaneControlTestAction(m *OS, action string) {
	m.menuPaneControl = action
	m.RunPaneControlAction(action)
}

func TestPaneControlDynamicActionRequiresDirectMenuSelection(t *testing.T) {
	m, socket, win := paneControlTestOS(t)
	a := paneControlTestAgent("annotation", "annotation-workbench · Main Agent")
	writePaneControlTestState(t, socket, a)
	addPaneControlAgentWindows(m, a)
	action := paneControlAction(win.ID, a.ID, paneControlSession, false)

	// A tape, keybinding, or direct dispatcher call can know this string, but it
	// cannot create the private carry that selecting the live menu row creates.
	m.RunPaneControlAction(action)
	if _, ok, _ := m.paneControlLease(win.ID); ok {
		t.Fatal("a replayable dynamic action granted Agent control")
	}

	m.ContextMenu = &ContextMenu{Items: []ContextMenuItem{{Label: "grant", Action: action}}, Selected: 0}
	if got := m.ContextMenuSelectedAction(); got != action || m.menuPaneControl != action {
		t.Fatalf("menu selection did not carry exact authority: action=%q carry=%q", got, m.menuPaneControl)
	}
	m.RunPaneControlAction(action)
	if _, ok, err := m.paneControlLease(win.ID); err != nil || !ok {
		t.Fatalf("direct menu selection did not grant control: ok=%v err=%v", ok, err)
	}
}

func TestPaneControlMenuListsOnlyEligibleAgentsAndCurrentOwner(t *testing.T) {
	m, socket, win := paneControlTestOS(t)
	a := paneControlTestAgent("annotation", "annotation-workbench · Main Agent")
	blocked := paneControlTestAgent("blocked", "blocked · Main Agent")
	blocked.State = "needs_input"
	readOnly := paneControlTestAgent("reader", "reader · Main Agent")
	readOnly.Mode = "read-only"
	other := paneControlTestAgent("other", "other-workspace · Main Agent")
	other.Workspace = 2
	writePaneControlTestState(t, socket, a, blocked, readOnly, other)
	addPaneControlAgentWindows(m, a, blocked, readOnly, other)

	items := m.paneControlMenuItems(win)
	if !menuHasLabel(items, "annotation-workbench · Main Agent · Next action") ||
		!menuHasLabel(items, "annotation-workbench · Main Agent · This Agent session") {
		t.Fatalf("eligible Agent rows missing: %+v", items)
	}
	for _, absent := range []string{"blocked ·", "reader ·", "other-workspace ·"} {
		if menuHasLabel(items, absent) {
			t.Fatalf("ineligible Agent %q was offered: %+v", absent, items)
		}
	}

	runPaneControlTestAction(m, paneControlAction(win.ID, a.ID, paneControlSession, false))
	items = m.paneControlMenuItems(win)
	if !menuHasLabel(items, "Agent control: "+a.Label+" (session)") || !menuHasLabel(items, "Revoke Agent control") {
		t.Fatalf("current controller was not visible: %+v", items)
	}
}

func TestPaneControlGrantTransferConfirmationAndRevoke(t *testing.T) {
	m, socket, win := paneControlTestOS(t)
	a := paneControlTestAgent("annotation", "annotation-workbench · Main Agent")
	b := paneControlTestAgent("label", "label-studio · Main Agent")
	writePaneControlTestState(t, socket, a, b)
	addPaneControlAgentWindows(m, a, b)

	runPaneControlTestAction(m, paneControlAction(win.ID, a.ID, paneControlOnce, false))
	lease, ok, err := m.paneControlLease(win.ID)
	if err != nil || !ok || lease.AgentID != a.ID || lease.Scope != paneControlOnce || lease.Uses != 1 {
		t.Fatalf("one-call grant = %+v ok=%v err=%v", lease, ok, err)
	}
	if lease.DaemonID == "" || lease.Foreground != "zsh" {
		t.Fatalf("grant did not bind daemon and foreground identity: %+v", lease)
	}

	// The first click must not transfer. It opens a second popup that names the
	// old and new controller and defaults to Cancel.
	runPaneControlTestAction(m, paneControlAction(win.ID, b.ID, paneControlSession, false))
	lease, _, _ = m.paneControlLease(win.ID)
	if lease.AgentID != a.ID {
		t.Fatalf("first transfer click changed controller to %q", lease.AgentID)
	}
	if m.ContextMenu == nil || m.ContextMenu.Title != "Transfer pane control?" || m.ContextMenu.Selected != 0 || len(m.ContextMenu.Items) != 2 {
		t.Fatalf("transfer confirmation = %+v", m.ContextMenu)
	}
	if !strings.Contains(m.ContextMenu.Items[1].Label, a.Label) || !strings.Contains(m.ContextMenu.Items[1].Label, b.Label) {
		t.Fatalf("transfer row does not name both controllers: %+v", m.ContextMenu.Items[1])
	}
	runPaneControlTestAction(m, m.ContextMenu.Items[1].Action)
	lease, _, _ = m.paneControlLease(win.ID)
	if lease.AgentID != b.ID || lease.Scope != paneControlSession || lease.Uses != 0 {
		t.Fatalf("confirmed transfer = %+v", lease)
	}

	runPaneControlTestAction(m, paneControlRevokeAction(win.ID))
	if _, ok, err := m.paneControlLease(win.ID); err != nil || ok {
		t.Fatalf("revoked lease still exists: ok=%v err=%v", ok, err)
	}
}

func TestPaneControlRefusesAgentAndMarksSSHIdentity(t *testing.T) {
	m, socket, win := paneControlTestOS(t)
	a := paneControlTestAgent("annotation", "annotation-workbench · Main Agent")
	writePaneControlTestState(t, socket, a)
	addPaneControlAgentWindows(m, a)

	win.AgentState = "idle"
	runPaneControlTestAction(m, paneControlAction(win.ID, a.ID, paneControlSession, false))
	if _, ok, _ := m.paneControlLease(win.ID); ok {
		t.Fatal("an Agent pane received Agent write control")
	}

	win.AgentState = ""
	win.ForegroundCmd = "ssh root@nas"
	win.CwdHost = "nas"
	runPaneControlTestAction(m, paneControlAction(win.ID, a.ID, paneControlSession, false))
	lease, ok, err := m.paneControlLease(win.ID)
	if err != nil || !ok || !lease.Remote || lease.HostHint != "nas" || lease.Foreground != "ssh root@nas" {
		t.Fatalf("SSH identity = %+v ok=%v err=%v", lease, ok, err)
	}
}
