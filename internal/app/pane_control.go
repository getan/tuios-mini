package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getan/tuios-mini/internal/session"
	"github.com/getan/tuios-mini/internal/terminal"
)

// The pane-control file is deliberately shared with Golder rather than copied
// into tuios session state. tuios is the operator surface that grants a lease;
// Golder remains the enforcement point and atomically consumes one-call leases
// immediately before input reaches a PTY.
const (
	paneControlVersion = 1
	paneControlOnce    = "one-call"
	paneControlSession = "session"
	paneControlTTL     = 90 * time.Second

	paneControlActionPrefix = "pane_control:"
)

type paneControlAgent struct {
	ID             string    `json:"agent_id"`
	Label          string    `json:"label"`
	Project        string    `json:"project"`
	Role           string    `json:"role"`
	ConversationID string    `json:"conversation_id"`
	SessionID      string    `json:"session_id"`
	PaneID         string    `json:"pane_id"`
	Workspace      int       `json:"workspace"`
	Mode           string    `json:"mode"`
	State          string    `json:"state,omitempty"`
	PID            int       `json:"pid"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type paneControlLease struct {
	SessionID      string    `json:"session_id"`
	WindowID       string    `json:"window_id"`
	Workspace      int       `json:"workspace"`
	AgentID        string    `json:"agent_id"`
	AgentLabel     string    `json:"agent_label"`
	ConversationID string    `json:"conversation_id"`
	Scope          string    `json:"scope"`
	Uses           int       `json:"uses,omitempty"`
	DaemonID       string    `json:"daemon_id"`
	HostHint       string    `json:"host_hint,omitempty"`
	Foreground     string    `json:"foreground,omitempty"`
	Remote         bool      `json:"remote,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type paneControlFile struct {
	Version int                         `json:"version"`
	Agents  map[string]paneControlAgent `json:"agents"`
	Leases  map[string]paneControlLease `json:"leases"`
}

func paneControlLeaseKey(sessionID, windowID string) string { return sessionID + "\x00" + windowID }

func (m *OS) paneControlSocket() (string, error) {
	if m.LocalSocket != "" {
		return filepath.Clean(m.LocalSocket), nil
	}
	return session.GetSocketPath()
}

func paneControlPath(socket string) string {
	return filepath.Clean(socket) + ".golder-pane-control.json"
}

func loadPaneControl(path string) (paneControlFile, error) {
	st := paneControlFile{Version: paneControlVersion, Agents: map[string]paneControlAgent{}, Leases: map[string]paneControlLease{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return paneControlFile{}, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return paneControlFile{}, fmt.Errorf("decode Golder pane control: %w", err)
	}
	if st.Version != paneControlVersion {
		return paneControlFile{}, fmt.Errorf("golder pane-control version %d is unsupported", st.Version)
	}
	if st.Agents == nil {
		st.Agents = map[string]paneControlAgent{}
	}
	if st.Leases == nil {
		st.Leases = map[string]paneControlLease{}
	}
	return st, nil
}

func savePaneControl(path string, st paneControlFile) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".golder-pane-control-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(st); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func prunePaneControl(st *paneControlFile, now time.Time) bool {
	changed := false
	cut := now.Add(-paneControlTTL)
	for id, agent := range st.Agents {
		if agent.UpdatedAt.Before(cut) {
			delete(st.Agents, id)
			changed = true
		}
	}
	for key, lease := range st.Leases {
		agent, ok := st.Agents[lease.AgentID]
		if !ok || agent.ConversationID != lease.ConversationID || agent.SessionID != lease.SessionID || agent.Workspace != lease.Workspace {
			delete(st.Leases, key)
			changed = true
		}
	}
	return changed
}

func (m *OS) withPaneControl(write bool, fn func(*paneControlFile) error) error {
	socket, err := m.paneControlSocket()
	if err != nil {
		return err
	}
	path := paneControlPath(socket)
	unlock, err := lockPaneControl(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	st, err := loadPaneControl(path)
	if err != nil {
		return err
	}
	changed := prunePaneControl(&st, time.Now())
	if err := fn(&st); err != nil {
		return err
	}
	if write || changed {
		return savePaneControl(path, st)
	}
	return nil
}

func (m *OS) paneControlAgents(workspace int) ([]paneControlAgent, error) {
	var agents []paneControlAgent
	err := m.withPaneControl(false, func(st *paneControlFile) error {
		for _, agent := range st.Agents {
			blocked := agent.State == "needs_input" || agent.State == "needs-input"
			if agent.SessionID == m.SessionName && agent.Workspace == workspace && agent.Mode != "read-only" && !blocked &&
				m.paneControlAgentLive(agent, workspace) {
				agents = append(agents, agent)
			}
		}
		return nil
	})
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].Label == agents[j].Label {
			return agents[i].ID < agents[j].ID
		}
		return agents[i].Label < agents[j].Label
	})
	return agents, err
}

func (m *OS) paneControlAgentLive(agent paneControlAgent, workspace int) bool {
	for _, win := range m.Windows {
		if win != nil && win.ID == agent.PaneID {
			return win.Workspace == workspace && win.AgentState != "" &&
				win.AgentState != "needs_input" && win.AgentState != "needs-input"
		}
	}
	return false
}

func (m *OS) paneControlLease(windowID string) (paneControlLease, bool, error) {
	var lease paneControlLease
	var ok bool
	err := m.withPaneControl(false, func(st *paneControlFile) error {
		lease, ok = st.Leases[paneControlLeaseKey(m.SessionName, windowID)]
		return nil
	})
	return lease, ok, err
}

func paneControlTargetEligible(win *terminal.Window) bool {
	if win == nil || win.AgentState != "" {
		return false
	}
	cmd := strings.ToLower(filepath.Base(strings.TrimSpace(win.ForegroundCmd)))
	return cmd != "golder" && cmd != "kitty-browse"
}

func paneControlRemote(win *terminal.Window) bool {
	if win == nil || win.CwdHost != "" || win.Host != "" {
		return win != nil && (win.CwdHost != "" || win.Host != "")
	}
	fields := strings.Fields(win.ForegroundCmd)
	if len(fields) == 0 {
		return false
	}
	name := strings.ToLower(filepath.Base(fields[0]))
	switch name {
	case "ssh", "mosh", "mosh-client", "autossh":
		return true
	default:
		return false
	}
}

func paneControlAction(windowID, agentID, scope string, transfer bool) string {
	return paneControlActionPrefix + strings.Join([]string{"grant", windowID, agentID, scope, strconv.FormatBool(transfer)}, ":")
}

func paneControlAgentsAction(windowID string) string {
	return paneControlActionPrefix + "agents:" + windowID
}

func paneControlAgentAction(windowID, agentID string) string {
	return paneControlActionPrefix + "agent:" + windowID + ":" + agentID
}

func paneControlRevokeAction(windowID string) string {
	return paneControlActionPrefix + "revoke:" + windowID
}

// IsPaneControlAction lets the shared dispatcher route menu-only dynamic rows
// without registering an unbounded action for every online Agent and pane.
func IsPaneControlAction(action string) bool {
	return strings.HasPrefix(action, paneControlActionPrefix)
}

func parsePaneControlAction(action string) (verb, windowID, agentID, scope string, transfer bool, err error) {
	parts := strings.Split(strings.TrimPrefix(action, paneControlActionPrefix), ":")
	if len(parts) == 2 && parts[0] == "revoke" && parts[1] != "" {
		return parts[0], parts[1], "", "", false, nil
	}
	if len(parts) != 5 || parts[0] != "grant" || parts[1] == "" || parts[2] == "" ||
		(parts[3] != paneControlOnce && parts[3] != paneControlSession) {
		return "", "", "", "", false, errors.New("invalid pane-control action")
	}
	transfer, err = strconv.ParseBool(parts[4])
	return parts[0], parts[1], parts[2], parts[3], transfer, err
}

func paneControlDaemonID(socket string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(socket); err == nil {
		socket = resolved
	}
	info, err := os.Stat(socket)
	if err != nil {
		return "", err
	}
	raw := fmt.Sprintf("pane-daemon-v1\x00%s\x00%d\x00%s", filepath.Clean(socket), info.ModTime().UnixNano(), info.Mode())
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), nil
}

func (m *OS) paneControlWindow(id string) *terminal.Window {
	for _, win := range m.Windows {
		if win != nil && win.ID == id {
			return win
		}
	}
	return nil
}

// RunPaneControlAction applies one explicit operator menu choice. A conflict
// never transfers here on the first click: it opens a second popup whose action
// carries transfer=true and names both controllers.
func (m *OS) RunPaneControlAction(action string) {
	if m.menuPaneControl != action {
		m.ShowNotification("Choose Agent control from the pane menu", "warning", m.Settings.NotificationDuration)
		return
	}
	m.menuPaneControl = ""
	parts := strings.Split(strings.TrimPrefix(action, paneControlActionPrefix), ":")
	if len(parts) == 2 && parts[0] == "agents" && parts[1] != "" {
		m.openPaneControlAgentMenu(parts[1])
		return
	}
	if len(parts) == 3 && parts[0] == "agent" && parts[1] != "" && parts[2] != "" {
		m.openPaneControlScopeMenu(parts[1], parts[2])
		return
	}
	verb, windowID, agentID, scope, transfer, err := parsePaneControlAction(action)
	if err != nil {
		m.ShowNotification(err.Error(), "error", m.Settings.NotificationDuration)
		return
	}
	if verb == "revoke" {
		revoked := false
		err = m.withPaneControl(true, func(st *paneControlFile) error {
			key := paneControlLeaseKey(m.SessionName, windowID)
			if _, ok := st.Leases[key]; ok {
				delete(st.Leases, key)
				revoked = true
			}
			return nil
		})
		if err != nil {
			m.ShowNotification("Pane control: "+err.Error(), "error", m.Settings.NotificationDuration)
		} else if revoked {
			m.ShowNotification("Pane write control revoked", "success", m.Settings.NotificationDuration)
		}
		return
	}

	win := m.paneControlWindow(windowID)
	if !paneControlTargetEligible(win) || win.Workspace <= 0 {
		m.ShowNotification("This pane cannot receive Agent write control", "warning", m.Settings.NotificationDuration)
		return
	}
	socket, socketErr := m.paneControlSocket()
	if socketErr != nil {
		m.ShowNotification("Pane control: "+socketErr.Error(), "error", m.Settings.NotificationDuration)
		return
	}
	daemonID, daemonErr := paneControlDaemonID(socket)
	if daemonErr != nil {
		m.ShowNotification("Pane control: "+daemonErr.Error(), "error", m.Settings.NotificationDuration)
		return
	}
	var selected paneControlAgent
	var previous paneControlLease
	var conflict bool
	err = m.withPaneControl(true, func(st *paneControlFile) error {
		agent, ok := st.Agents[agentID]
		blocked := agent.State == "needs_input" || agent.State == "needs-input"
		if !ok || agent.SessionID != m.SessionName || agent.Workspace != win.Workspace || agent.Mode == "read-only" || blocked ||
			!m.paneControlAgentLive(agent, win.Workspace) {
			return errors.New("selected Agent is no longer eligible in this workspace")
		}
		selected = agent
		key := paneControlLeaseKey(m.SessionName, windowID)
		if current, ok := st.Leases[key]; ok && current.AgentID != agent.ID && !transfer {
			previous, conflict = current, true
			return nil
		}
		uses := 0
		if scope == paneControlOnce {
			uses = 1
		}
		st.Leases[key] = paneControlLease{
			SessionID: m.SessionName, WindowID: windowID, Workspace: win.Workspace,
			AgentID: agent.ID, AgentLabel: agent.Label, ConversationID: agent.ConversationID,
			Scope: scope, Uses: uses, DaemonID: daemonID, HostHint: win.CwdHost,
			Foreground: strings.TrimSpace(win.ForegroundCmd), Remote: paneControlRemote(win),
			CreatedAt: time.Now().UTC(),
		}
		return nil
	})
	if err != nil {
		m.ShowNotification("Pane control: "+err.Error(), "error", m.Settings.NotificationDuration)
		return
	}
	if conflict {
		m.openPaneControlTransferMenu(win, previous, selected, scope)
		return
	}
	m.ShowNotification("Write control: "+selected.Label+" ("+scope+")", "success", m.Settings.NotificationDuration)
}

func shortPaneControlLabel(label string) string {
	label = strings.TrimSpace(label)
	for _, suffix := range []string{" · Main Agent", " - Main Agent"} {
		label = strings.TrimSuffix(label, suffix)
	}
	return label
}

func paneControlAgentLabel(agent paneControlAgent) string {
	label := strings.TrimSpace(agent.Project)
	if label == "" {
		label = shortPaneControlLabel(agent.Label)
	}
	if agent.Role != "" && agent.Role != "main" {
		label += " · " + agent.Role
	}
	return label
}

func (m *OS) openPaneControlAgentMenu(windowID string) {
	win := m.paneControlWindow(windowID)
	if !paneControlTargetEligible(win) || win.Workspace <= 0 {
		m.ShowNotification("This pane cannot receive Agent write control", "warning", m.Settings.NotificationDuration)
		return
	}
	agents, err := m.paneControlAgents(win.Workspace)
	if err != nil {
		m.ShowNotification("Pane control: "+err.Error(), "error", m.Settings.NotificationDuration)
		return
	}
	items := make([]ContextMenuItem, 0, len(agents)+1)
	for _, agent := range agents {
		items = append(items, ContextMenuItem{Label: paneControlAgentLabel(agent), Action: paneControlAgentAction(windowID, agent.ID)})
	}
	items = append(items, ContextMenuItem{Label: "Cancel", Action: paneControlActionPrefix + "cancel"})
	cm := &ContextMenu{
		Title: "Choose Agent", Target: CtxTargetPane, WindowIndex: -1,
		AnchorX: m.Width / 2, AnchorY: m.Height / 2, Selected: -1, ItemH: 1, Items: items,
	}
	cm.Selected = cm.Next(1)
	m.ContextMenu = cm
}

func (m *OS) openPaneControlScopeMenu(windowID, agentID string) {
	win := m.paneControlWindow(windowID)
	if !paneControlTargetEligible(win) || win.Workspace <= 0 {
		m.ShowNotification("This pane cannot receive Agent write control", "warning", m.Settings.NotificationDuration)
		return
	}
	agents, err := m.paneControlAgents(win.Workspace)
	if err != nil {
		m.ShowNotification("Pane control: "+err.Error(), "error", m.Settings.NotificationDuration)
		return
	}
	var selected paneControlAgent
	for _, agent := range agents {
		if agent.ID == agentID {
			selected = agent
			break
		}
	}
	if selected.ID == "" {
		m.ShowNotification("That Agent is no longer available", "warning", m.Settings.NotificationDuration)
		return
	}
	cm := &ContextMenu{
		Title: paneControlAgentLabel(selected), Target: CtxTargetPane, WindowIndex: -1,
		AnchorX: m.Width / 2, AnchorY: m.Height / 2, Selected: 0, ItemH: 1,
		Items: []ContextMenuItem{
			{Label: "Next action", Action: paneControlAction(windowID, selected.ID, paneControlOnce, false)},
			{Label: "This conversation", Action: paneControlAction(windowID, selected.ID, paneControlSession, false)},
			{Label: "Back", Action: paneControlAgentsAction(windowID)},
			{Label: "Cancel", Action: paneControlActionPrefix + "cancel"},
		},
	}
	m.ContextMenu = cm
}

func (m *OS) openPaneControlTransferMenu(win *terminal.Window, from paneControlLease, to paneControlAgent, scope string) {
	cm := &ContextMenu{
		Title: "Transfer pane control?", Target: CtxTargetPane, WindowIndex: -1,
		AnchorX: m.Width / 2, AnchorY: m.Height / 2, Selected: 3, ItemH: 1,
		Items: []ContextMenuItem{
			{Label: "From: " + shortPaneControlLabel(from.AgentLabel), Dim: true},
			{Label: "To: " + paneControlAgentLabel(to), Dim: true},
			separator(),
			{Label: "Cancel", Action: paneControlActionPrefix + "cancel"},
			{Label: "Confirm transfer", Action: paneControlAction(win.ID, to.ID, scope, true), Hint: scope, Warn: true},
		},
	}
	m.ContextMenu = cm
}

func (m *OS) paneControlMenuItems(win *terminal.Window) []ContextMenuItem {
	if !m.IsDaemonSession || m.AttachedHost != "" || win == nil {
		return nil
	}
	agents, err := m.paneControlAgents(win.Workspace)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return []ContextMenuItem{{Label: "Agent control unavailable", Dim: true}}
	}
	lease, controlled, _ := m.paneControlLease(win.ID)
	items := []ContextMenuItem{separator()}
	status := "Agent: none"
	if controlled {
		status = "Agent: " + shortPaneControlLabel(lease.AgentLabel)
	}
	statusItem := ContextMenuItem{Label: status, Dim: true}
	if controlled {
		statusItem.Hint = lease.Scope
	}
	items = append(items, statusItem)
	if !paneControlTargetEligible(win) {
		return items
	}
	if len(agents) > 0 {
		items = append(items, ContextMenuItem{Label: "Agent control…", Action: paneControlAgentsAction(win.ID)})
	} else {
		items = append(items, ContextMenuItem{Label: "No eligible Agents", Dim: true})
	}
	if controlled {
		items = append(items, ContextMenuItem{Label: "Revoke Agent control", Action: paneControlRevokeAction(win.ID), Warn: true})
	}
	return items
}
