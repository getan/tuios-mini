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
	project, _, _ := strings.Cut(label, " · ")
	return paneControlAgent{
		ID: id, Label: label, Project: project, Role: "main", ConversationID: "conversation-" + id,
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
	if !menuHasLabel(items, "Agent control…") || menuHasLabel(items, "Main Agent") {
		t.Fatalf("first-level Agent row is missing or still verbose: %+v", items)
	}
	for _, absent := range []string{"blocked ·", "reader ·", "other-workspace ·"} {
		if menuHasLabel(items, absent) {
			t.Fatalf("ineligible Agent %q was offered: %+v", absent, items)
		}
	}

	runPaneControlTestAction(m, paneControlAgentsAction(win.ID))
	if m.ContextMenu == nil || m.ContextMenu.Title != "Choose Agent" || !menuHasLabel(m.ContextMenu.Items, "annotation-workbench") {
		t.Fatalf("Agent picker = %+v", m.ContextMenu)
	}
	for _, absent := range []string{"blocked", "reader", "other-workspace"} {
		if menuHasLabel(m.ContextMenu.Items, absent) {
			t.Fatalf("Agent picker offered %q: %+v", absent, m.ContextMenu.Items)
		}
	}
	runPaneControlTestAction(m, paneControlAgentAction(win.ID, a.ID))
	if m.ContextMenu == nil || m.ContextMenu.Title != "annotation-workbench" ||
		!menuHasLabel(m.ContextMenu.Items, "Next action") || !menuHasLabel(m.ContextMenu.Items, "This conversation") {
		t.Fatalf("scope picker = %+v", m.ContextMenu)
	}
	runPaneControlTestAction(m, paneControlAction(win.ID, a.ID, paneControlSession, false))
	items = m.paneControlMenuItems(win)
	if !menuHasLabel(items, "Agent: annotation-workbench") || !menuHasLabel(items, "Revoke Agent control") {
		t.Fatalf("current controller was not visible: %+v", items)
	}
	for _, item := range items {
		if strings.Contains(item.Label, "Agent: annotation-workbench") && item.Hint != paneControlSession {
			t.Fatalf("controller scope hint = %q", item.Hint)
		}
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
	if m.ContextMenu == nil || m.ContextMenu.Title != "Transfer pane control?" || m.ContextMenu.Selected != 3 || len(m.ContextMenu.Items) != 5 {
		t.Fatalf("transfer confirmation = %+v", m.ContextMenu)
	}
	if !strings.Contains(m.ContextMenu.Items[0].Label, "annotation-workbench") || !strings.Contains(m.ContextMenu.Items[1].Label, "label-studio") || m.ContextMenu.Items[4].Label != "Confirm transfer" {
		t.Fatalf("transfer rows do not show both controllers clearly: %+v", m.ContextMenu.Items)
	}
	runPaneControlTestAction(m, m.ContextMenu.Items[4].Action)
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

// Identical project names must not make a security-sensitive grant or transfer
// ambiguous, even when the Agents happen to share a conversation ID.
func TestPaneControlSameProjectAgentsRemainDistinguishable(t *testing.T) {
	m, socket, win := paneControlTestOS(t)
	a := paneControlTestAgent("golder-main-1111111111111111", "golder · Main Agent")
	b := paneControlTestAgent("golder-main-2222222222222222", "golder · Main Agent")
	a.PaneID, b.PaneID = "12345678-aaaa", "abcdef12-bbbb"
	a.ConversationID, b.ConversationID = "20261110-120000-123456", "20261110-120000-123456"
	writePaneControlTestState(t, socket, a, b)
	addPaneControlAgentWindows(m, a, b)

	runPaneControlTestAction(m, paneControlAgentsAction(win.ID))
	if m.ContextMenu == nil || len(m.ContextMenu.Items) != 3 {
		t.Fatalf("Agent picker = %+v", m.ContextMenu)
	}
	first, second := m.ContextMenu.Items[0], m.ContextMenu.Items[1]
	if first.Label == second.Label || !strings.Contains(first.Label, "12345678") || !strings.Contains(second.Label, "abcdef12") ||
		!strings.Contains(first.Label, "123456") || !strings.Contains(second.Label, "123456") {
		t.Fatalf("ambiguous Agent choices: %+v", m.ContextMenu.Items)
	}
	if first.Action != paneControlAgentAction(win.ID, a.ID) || second.Action != paneControlAgentAction(win.ID, b.ID) {
		t.Fatalf("display changed grant identities: %+v", m.ContextMenu.Items)
	}
	rendered, _ := m.renderContextMenu()
	if !strings.Contains(rendered, "12345678") || !strings.Contains(rendered, "abcdef12") {
		t.Fatalf("Agent identities were clipped in the actual menu: %q", rendered)
	}

	runPaneControlTestAction(m, paneControlAgentAction(win.ID, a.ID))
	if !strings.Contains(m.ContextMenu.Title, "12345678") {
		t.Fatalf("scope picker hides selected Agent: %+v", m.ContextMenu)
	}
	runPaneControlTestAction(m, paneControlAction(win.ID, a.ID, paneControlSession, false))
	if !menuHasLabel(m.paneControlMenuItems(win), "12345678") {
		t.Fatal("current controller is ambiguous")
	}
	// A controller waiting on an approval is excluded from the picker, but it
	// still holds the lease and must remain identifiable as the current owner.
	if err := m.withPaneControl(true, func(st *paneControlFile) error {
		entry := st.Agents[a.ID]
		entry.State = "needs_input"
		st.Agents[a.ID] = entry
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !menuHasLabel(m.paneControlMenuItems(win), "12345678") {
		t.Fatal("blocked current controller is ambiguous")
	}
	if err := m.withPaneControl(true, func(st *paneControlFile) error {
		entry := st.Agents[a.ID]
		entry.State = "idle"
		st.Agents[a.ID] = entry
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runPaneControlTestAction(m, paneControlAction(win.ID, b.ID, paneControlSession, false))
	if m.ContextMenu == nil || len(m.ContextMenu.Items) != 5 ||
		!strings.Contains(m.ContextMenu.Items[0].Label, "12345678") ||
		!strings.Contains(m.ContextMenu.Items[1].Label, "abcdef12") {
		t.Fatalf("ambiguous transfer confirmation: %+v", m.ContextMenu)
	}
	lease, ok, err := m.paneControlLease(win.ID)
	if err != nil || !ok || lease.AgentID != a.ID {
		t.Fatalf("transfer occurred before confirmation: %+v, %v, %v", lease, ok, err)
	}
}

func TestPaneControlCollisionDoesNotHideAgentIdentity(t *testing.T) {
	m, socket, win := paneControlTestOS(t)
	a := paneControlTestAgent("golder-main-1111111111111111", "golder · Main Agent")
	b := paneControlTestAgent("golder-main-2222222222222222", "golder · Main Agent")
	a.PaneID, b.PaneID = "12345678-aaaa", "12345678-bbbb"
	a.ConversationID, b.ConversationID = "20261110-120000-123456", "20261110-120000-123456"
	writePaneControlTestState(t, socket, a, b)
	addPaneControlAgentWindows(m, a, b)
	runPaneControlTestAction(m, paneControlAgentsAction(win.ID))
	if m.ContextMenu == nil || m.ContextMenu.Items[0].Label == m.ContextMenu.Items[1].Label {
		t.Fatalf("colliding short identities: %+v", m.ContextMenu)
	}
	runPaneControlTestAction(m, paneControlAction(win.ID, a.ID, paneControlSession, false))
	runPaneControlTestAction(m, paneControlAction(win.ID, b.ID, paneControlSession, false))
	if m.ContextMenu == nil || m.ContextMenu.Items[0].Label == strings.Replace(m.ContextMenu.Items[1].Label, "To:", "From:", 1) {
		t.Fatalf("colliding transfer identities: %+v", m.ContextMenu)
	}
}

// The preview is display-only: it cannot become an action, replace the exact
// Agent ID, or obscure which controller a transfer affects.
func TestPaneControlShowsConversationPreviews(t *testing.T) {
	m, socket, win := paneControlTestOS(t)
	a := paneControlTestAgent("a", "golder · Main Agent")
	b := paneControlTestAgent("b", "golder · Main Agent")
	a.Preview = "修复登录按钮，并且处理非常非常非常非常非常长的错误提示"
	b.Preview = "检查缓存失效问题"
	writePaneControlTestState(t, socket, a, b)
	addPaneControlAgentWindows(m, a, b)
	runPaneControlTestAction(m, paneControlAgentsAction(win.ID))
	items := m.ContextMenu.Items
	if len(items) != 5 || !items[1].Dim || !items[3].Dim || items[1].Action != "" ||
		!strings.Contains(items[1].Label, "修复登录按钮") || !strings.Contains(items[1].Label, "…") ||
		!strings.Contains(items[3].Label, "检查缓存失效问题") || items[0].Action != paneControlAgentAction(win.ID, a.ID) {
		t.Fatalf("preview rows or identity: %+v", items)
	}
	if m.ContextMenu.Next(1) != 2 {
		t.Fatal("keyboard navigation did not skip the preview row")
	}
	runPaneControlTestAction(m, paneControlAction(win.ID, a.ID, paneControlSession, false))
	if !menuHasLabel(m.paneControlMenuItems(win), "修复登录按钮") {
		t.Fatal("current controller preview is missing")
	}
	runPaneControlTestAction(m, paneControlAction(win.ID, b.ID, paneControlSession, false))
	if m.ContextMenu == nil || !menuHasLabel(m.ContextMenu.Items, "修复登录按钮") ||
		!menuHasLabel(m.ContextMenu.Items, "检查缓存失效问题") || m.ContextMenu.Items[m.ContextMenu.Selected].Label != "Cancel" {
		t.Fatalf("transfer preview or safe default: %+v", m.ContextMenu)
	}
}
