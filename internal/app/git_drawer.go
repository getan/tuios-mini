package app

import (
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/review"
)

// gitDrawerState is one read-only Git drawer (commits, branches, stashes,
// tags, remotes, graph, a file's history, or the session's repositories).
// It is an overlay, not a rail row: the lists are too wide for twenty-six
// columns and a row menu cannot hold a hundred of them.
type gitDrawerState struct {
	open     bool
	gen      uint64
	kind     review.HistoryKind
	title    string
	rows     []string
	selected int
	scroll   int
	loading  bool
	err      string
}

// gitHistoryMsg is one drawer's answer. Gen drops an answer for a drawer the
// person has already closed or replaced.
type gitHistoryMsg struct {
	Gen   uint64
	Title string
	Rows  []string
	Err   string
}

// GitDrawerOpen reports whether a Git drawer owns the keyboard.
func (m *OS) GitDrawerOpen() bool { return m.gitDrawer.open }

// SidebarGitHistory reads one drawer for the repository the Git tab shows.
// File history needs a changed row under the cursor; the other kinds do not.
func (m *OS) SidebarGitHistory(kind review.HistoryKind) tea.Cmd {
	if !m.GitTabActive() {
		return nil
	}
	origin := m.gitStatus.Origin
	if origin.Session == "" {
		m.ShowNotification("No repository to read yet.", "info", m.Settings.NotificationDuration)
		return nil
	}
	path := ""
	if kind == review.HistoryFile {
		e, ok := m.gitEntryAtCursor()
		if !ok {
			m.ShowNotification("Put the cursor on a changed file first.", "info", m.Settings.NotificationDuration)
			return nil
		}
		path = e.Path
	}
	m.gitDrawer.gen++
	gen := m.gitDrawer.gen
	m.gitDrawer = gitDrawerState{
		open: true, gen: gen, kind: kind, title: gitDrawerTitle(kind), loading: true,
	}
	call := m.inboxCaller()
	params := map[string]any{
		"session": origin.Session, "window": origin.Window,
		"kind": string(kind), "limit": 100,
	}
	if path != "" {
		params["path"] = path
	}
	return func() tea.Msg {
		raw, err := call("review-history", params, 15*time.Second)
		if err != nil {
			return gitHistoryMsg{Gen: gen, Err: err.Error()}
		}
		var res struct {
			Title string   `json:"title"`
			Rows  []string `json:"rows"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return gitHistoryMsg{Gen: gen, Err: err.Error()}
		}
		return gitHistoryMsg{Gen: gen, Title: res.Title, Rows: res.Rows}
	}
}

func gitDrawerTitle(kind review.HistoryKind) string {
	switch kind {
	case review.HistoryCommits:
		return "commits"
	case review.HistoryGraph:
		return "graph"
	case review.HistoryBranches:
		return "branches"
	case review.HistoryTags:
		return "tags"
	case review.HistoryStashes:
		return "stashes"
	case review.HistoryRemotes:
		return "remotes"
	case review.HistoryFile:
		return "file history"
	case review.HistoryRepos:
		return "repositories"
	}
	return "git"
}

// GitDrawerMove moves the selection and keeps it in view.
func (m *OS) GitDrawerMove(delta int) {
	d := &m.gitDrawer
	if len(d.rows) == 0 {
		return
	}
	d.selected = min(max(d.selected+delta, 0), len(d.rows)-1)
	visible := gitDrawerVisibleRows()
	if d.selected < d.scroll {
		d.scroll = d.selected
	}
	if d.selected >= d.scroll+visible {
		d.scroll = d.selected - visible + 1
	}
}

// GitDrawerEdge moves to the first or last row.
func (m *OS) GitDrawerEdge(last bool) {
	d := &m.gitDrawer
	if last {
		m.GitDrawerMove(len(d.rows))
		return
	}
	m.GitDrawerMove(-len(d.rows))
}

// GitDrawerClose closes the drawer and invalidates an answer still in flight.
func (m *OS) GitDrawerClose() {
	m.gitDrawer = gitDrawerState{gen: m.gitDrawer.gen + 1}
}

// GitDrawerActivate chooses a repository row: it focuses a pane showing that
// repository, so the Git tab reads it next. The other drawers are read-only.
func (m *OS) GitDrawerActivate() tea.Cmd {
	d := &m.gitDrawer
	if d.kind != review.HistoryRepos || d.selected < 0 || d.selected >= len(d.rows) {
		return nil
	}
	parts := strings.Split(d.rows[d.selected], "\t")
	if len(parts) < 2 {
		return nil
	}
	idx := m.windowIndexByID(parts[1])
	if idx < 0 {
		return nil
	}
	m.FocusWindow(idx)
	m.GitDrawerClose()
	return m.GitStatusSyncCmd()
}

func gitDrawerVisibleRows() int { return 20 }
