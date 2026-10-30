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
	// parent is the commits list a commit detail was opened from. Esc on the
	// detail restores this state without asking the daemon for the list again.
	parent *gitDrawerState
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

// OpenGitHistory is the mouse-facing entry to the same drawer the
// git_commits action opens.
func (m *OS) OpenGitHistory() tea.Cmd {
	return m.OpenGitHistoryKind(1)
}

// OpenGitHistoryKind opens the history row at index in gitTabHistoryKinds.
func (m *OS) OpenGitHistoryKind(index int) tea.Cmd {
	if index < 0 || index >= len(gitTabHistoryKinds) {
		return nil
	}
	return m.SidebarGitHistory(gitTabHistoryKinds[index])
}

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
	case review.HistoryCommit:
		return "commit"
	case review.HistoryBranch:
		return "branch"
	case review.HistoryTag:
		return "tag"
	case review.HistoryStash:
		return "stash"
	case review.HistoryRemote:
		return "remote"
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
	if parent := m.gitDrawer.parent; parent != nil {
		m.gitDrawer = *parent
		return
	}
	m.gitDrawer = gitDrawerState{gen: m.gitDrawer.gen + 1}
}

// GitDrawerActivate chooses a repository row, or opens the commit under the
// cursor. The commit detail is fetched only after this explicit choice, so a
// long commits list never pays for patches nobody asked to read.
func (m *OS) GitDrawerActivate() tea.Cmd {
	d := &m.gitDrawer
	if d.selected < 0 || d.selected >= len(d.rows) {
		return nil
	}
	if kind, ref := gitDrawerDetail(d.kind, d.rows[d.selected]); kind != "" {
		return m.openGitDetail(kind, ref)
	}
	if d.kind != review.HistoryRepos {
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

func (m *OS) openGitDetail(kind review.HistoryKind, ref string) tea.Cmd {
	origin := m.gitStatus.Origin
	if origin.Session == "" {
		return nil
	}
	parent := m.gitDrawer
	m.gitDrawer.gen++
	gen := m.gitDrawer.gen
	m.gitDrawer = gitDrawerState{
		open: true, gen: gen, kind: kind,
		title: gitDrawerTitle(kind) + " " + ref, loading: true, parent: &parent,
	}
	call := m.inboxCaller()
	params := map[string]any{
		"session": origin.Session, "window": origin.Window,
		"kind": string(kind), "ref": ref, "limit": 500,
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

// gitDrawerDetail maps a list row to its second-level kind and ref. Repos are
// navigation, not a detail: choosing one focuses the pane that owns it.
func gitDrawerDetail(kind review.HistoryKind, row string) (review.HistoryKind, string) {
	switch kind {
	case review.HistoryCommits, review.HistoryFile:
		fields := strings.Fields(row)
		if len(fields) > 0 {
			return review.HistoryCommit, fields[0]
		}
	case review.HistoryGraph:
		for _, field := range strings.Fields(row) {
			field = strings.Trim(field, "*|\\/ ")
			if len(field) >= 7 && strings.Trim(field, "0123456789abcdefABCDEF") == "" {
				return review.HistoryCommit, field
			}
		}
	case review.HistoryBranches:
		fields := strings.Fields(row)
		if len(fields) > 0 {
			return review.HistoryBranch, fields[0]
		}
	case review.HistoryTags:
		fields := strings.Fields(row)
		if len(fields) > 0 {
			return review.HistoryTag, fields[0]
		}
	case review.HistoryStashes:
		fields := strings.Fields(row)
		if len(fields) > 0 {
			return review.HistoryStash, fields[0]
		}
	case review.HistoryRemotes:
		fields := strings.Fields(row)
		if len(fields) > 0 {
			return review.HistoryRemote, fields[0]
		}
	}
	return "", ""
}

func gitDrawerVisibleRows() int { return 20 }
