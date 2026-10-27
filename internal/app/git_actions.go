package app

import (
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// gitActionMsg is one finished stage, unstage or commit.
type gitActionMsg struct {
	OK  string
	Err string
}

// SidebarCursorOnGit reports whether the rail's cursor is on a changed-path
// row of the Git tab. The file actions consult their own section first; this
// is the same gate for the Git tab's own keys.
func (m *OS) SidebarCursorOnGit() bool {
	if !m.SidebarFocused || !m.GitTabActive() {
		return false
	}
	row, ok := m.sidebarCursorRow()
	return ok && row.Kind == sidebarRowGitEntry
}

// gitEntryAtCursor is the entry the cursor names, with the origin it was drawn
// from. An answer that arrived since the draw cannot retarget the key.
func (m *OS) gitEntryAtCursor() (gitStatusEntry, bool) {
	row, ok := m.sidebarCursorRow()
	if !ok || row.Kind != sidebarRowGitEntry || row.WindowIndex < 0 || row.WindowIndex >= len(m.gitStatus.entries) {
		return gitStatusEntry{}, false
	}
	return m.gitStatus.entries[row.WindowIndex], true
}

// SidebarGitStage stages or unstages the change under the cursor. Only the
// index moves: a stage is the same verb the review uses, so a file staged here
// and one staged from the diff are the same index entry.
func (m *OS) SidebarGitStage(unstage bool) tea.Cmd {
	e, ok := m.gitEntryAtCursor()
	if !ok {
		m.ShowNotification("Put the cursor on a changed file first.", "info", m.Settings.NotificationDuration)
		return nil
	}
	if unstage && e.Index != "staged" {
		m.ShowNotification("Only a staged change can be unstaged.", "info", m.Settings.NotificationDuration)
		return nil
	}
	if !unstage && e.Index == "staged" {
		m.ShowNotification("That change is already staged.", "info", m.Settings.NotificationDuration)
		return nil
	}
	action := "stage"
	if unstage {
		action = "unstage"
	}
	call, nonce := m.inboxCaller(), m.inboxNonce()
	params := map[string]any{
		"action": action, "session": e.Origin.Session,
		"window": e.Origin.Window, "path": e.Path,
	}
	if nonce != "" {
		params["human_nonce"] = nonce
	}
	path := e.Path
	return func() tea.Msg {
		if _, err := call("review-stage", params, 15*time.Second); err != nil {
			return gitActionMsg{Err: err.Error()}
		}
		verb := "Staged "
		if unstage {
			verb = "Unstaged "
		}
		return gitActionMsg{OK: verb + path + "."}
	}
}

// SidebarGitCommitPrompt opens the one-line commit message dialog on the
// staged changes. The origin is captured now so a status refresh cannot move
// the commit to another pane or repository.
func (m *OS) SidebarGitCommitPrompt() bool {
	if !m.GitTabActive() {
		return false
	}
	if m.filePrompt.Busy {
		m.ShowNotification("Wait for the last file action to finish.", "info", m.Settings.NotificationDuration)
		return true
	}
	if m.gitStatus.Status.Root == "" || m.gitStatus.Origin.Session == "" {
		m.ShowNotification("No repository to commit in yet.", "info", m.Settings.NotificationDuration)
		return true
	}
	m.filePrompt = filePromptState{
		Kind:      filePromptCommit,
		Dir:       m.gitStatus.Status.Root,
		GitOrigin: m.gitStatus.Origin,
	}
	return true
}

// commitStagedCmd sends the message to the pane's repository. The daemon
// commits the index only, so the commit holds exactly the Staged section.
func (m *OS) commitStagedCmd(origin gitStatusOrigin, message string) tea.Cmd {
	call, nonce := m.inboxCaller(), m.inboxNonce()
	return func() tea.Msg {
		params := map[string]any{
			"session": origin.Session, "window": origin.Window, "message": message,
		}
		if nonce != "" {
			params["human_nonce"] = nonce
		}
		raw, err := call("review-commit", params, 30*time.Second)
		if err != nil {
			return gitActionMsg{Err: err.Error()}
		}
		var res struct {
			Commit string `json:"commit"`
		}
		_ = json.Unmarshal(raw, &res)
		short := strings.TrimSpace(res.Commit)
		if len(short) > 8 {
			short = short[:8]
		}
		if short == "" {
			return gitActionMsg{OK: "Committed."}
		}
		return gitActionMsg{OK: "Committed " + short + "."}
	}
}

// HandleGitAction says what happened and rereads the status at once, rather
// than waiting for the refresh interval: the row the key acted on has moved,
// and the next key must find the new side.
func (m *OS) HandleGitAction(msg gitActionMsg) tea.Cmd {
	m.filePrompt.Busy = false
	if msg.Err != "" {
		m.ShowNotification(msg.Err, "error", m.Settings.NotificationDuration)
		return nil
	}
	if msg.OK != "" {
		m.ShowNotification(msg.OK, "success", m.Settings.NotificationDuration)
	}
	m.gitStatus.inFlight = false
	m.gitStatus.at = time.Time{}
	return m.GitStatusSyncCmd()
}
