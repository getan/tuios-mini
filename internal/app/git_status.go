package app

import (
	"context"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/getan/tuios-mini/internal/gitstatus"
	"github.com/getan/tuios-mini/internal/review"
)

// The project section's Git tab: the changed paths of the pane's repository,
// grouped by index side, with the same letters and inks the files tree uses.
//
// It is a navigator, not a Git client. A changed file opens the existing review
// (which owns diffs and notes); an untracked file opens the native preview. No
// staging, commit or fetch lives here.

// gitStatusOrigin captures the pane and machine a reading belongs to. A path
// alone is not an identity: two panes (or machines) can report the same path.
type gitStatusOrigin struct {
	Session, Window, Dir        string
	Host, AttachedHost, CwdHost string
}

// gitStatusEntry is a drawn row, including the index side and source it named
// when drawn. A later status reply must not retarget an outstanding click.
type gitStatusEntry struct {
	gitstatus.Entry
	Origin      gitStatusOrigin
	Root, Index string
}

// gitStatusView is the last reading and the latest request generation.
type gitStatusView struct {
	Dir      string
	Status   gitstatus.Status
	Origin   gitStatusOrigin
	asked    gitStatusOrigin
	gen      uint64
	inFlight bool
	at       time.Time
	entries  []gitStatusEntry
}

// GitStatusMsg carries a finished reading back to the loop.
type GitStatusMsg struct {
	Gen    uint64
	Origin gitStatusOrigin
	Status gitstatus.Status
}

// GitTabActive reports whether the project section is showing changes.
func (m *OS) GitTabActive() bool { return m.projectTab == 1 }

// SetProjectTab switches the project section between files (0) and git (1).
func (m *OS) SetProjectTab(tab int) tea.Cmd {
	if tab < 0 || tab > 1 || tab == m.projectTab {
		return nil
	}
	m.projectTab = tab
	if tab == 1 {
		// Switching in must not show a stale reading: ask now rather than wait
		// for the beat that follows the next message.
		m.gitStatus.at = time.Time{}
		return m.GitStatusSyncCmd()
	}
	return nil
}

// GitStatusSyncCmd reads the repository's changed paths on the Git tab's beat.
// It is deliberately separate from gitstate.Read: that is branch drift and is
// cheap; this walks the working tree, so it runs only while the tab is shown.
func (m *OS) GitStatusSyncCmd() tea.Cmd {
	if !m.GitTabActive() {
		return nil
	}
	w := m.GetFocusedWindow()
	want := gitStatusOrigin{Session: m.SessionName, Dir: m.filesWantDir(), AttachedHost: m.AttachedHost}
	if w != nil {
		want.Window, want.Host, want.CwdHost = w.ID, w.Host, w.CwdHost
	}
	v := &m.gitStatus
	if want != v.asked {
		v.gen++
		v.asked, v.inFlight, v.at = want, false, time.Time{}
	}
	// Never pass a remote directory to this machine's git, even if it happens
	// to exist here too. The remote files listing is not local read authority.
	if w == nil || want.Dir == "" || want.Host != "" || want.AttachedHost != "" || want.CwdHost != "" {
		return nil
	}
	if v.inFlight || time.Since(v.at) < gitRefreshInterval {
		return nil
	}
	v.gen++
	gen := v.gen
	v.inFlight, v.at = true, time.Now()
	return func() tea.Msg {
		return GitStatusMsg{Gen: gen, Origin: want, Status: gitstatus.Read(context.Background(), want.Dir)}
	}
}

// ApplyGitStatus installs a reading unless the tab moved on.
func (m *OS) ApplyGitStatus(msg GitStatusMsg) {
	v := &m.gitStatus
	if msg.Gen != v.gen || msg.Origin != v.asked {
		return
	}
	v.Dir, v.Status, v.Origin, v.inFlight = msg.Origin.Dir, msg.Status, msg.Origin, false
}

// gitTabRows is what the Git tab draws: two tab rows, then one header per
// non-empty group and its entries.
func (m *OS) gitTabRows() []fileRowSpec {
	rows := m.projectTabRows()
	m.gitStatus.entries = m.gitStatus.entries[:0]
	if m.filesView.Otherwise() {
		return rows
	}
	if o := m.gitStatus.asked; o.Host != "" || o.AttachedHost != "" || o.CwdHost != "" {
		return append(rows, fileRowSpec{Note: true, Warn: true, Name: "Review Git on that machine"})
	}
	if m.gitStatus.Origin != m.gitStatus.asked || m.gitStatus.Dir == "" {
		return append(rows, fileRowSpec{Note: true, Name: "reading changes..."})
	}
	st := m.gitStatus.Status
	if st.Err != "" {
		return append(rows, fileRowSpec{Note: true, Warn: true, Name: st.Err})
	}
	if st.Root != "" {
		rows = append(rows, fileRowSpec{Note: true, Name: st.Branch, Key: "branch"})
	}
	add := func(title, side string, entries []gitstatus.Entry) {
		if len(entries) == 0 {
			return
		}
		rows = append(rows, fileRowSpec{Note: true, Name: title + " " + strconv.Itoa(len(entries)), Key: title})
		for _, e := range entries {
			m.gitStatus.entries = append(m.gitStatus.entries, gitStatusEntry{
				Entry: e, Origin: m.gitStatus.Origin, Root: st.Root, Index: side,
			})
			rows = append(rows, fileRowSpec{
				Kind:  sidebarRowGitEntry,
				Index: len(m.gitStatus.entries) - 1,
				Key:   title + ":" + e.Path,
				Name:  e.Path,
			})
		}
	}
	add("Conflicts", review.IndexUnstaged, st.Conflicts)
	add("Staged", review.IndexStaged, st.Staged)
	add("Changes", review.IndexUnstaged, st.Unstaged)
	add("Untracked", "", st.Untracked)
	if st.Empty() {
		return append(rows, fileRowSpec{Note: true, Name: "clean"})
	}
	return rows
}

// projectTabRows is the two-tab bar every project view starts with.
func (m *OS) projectTabRows() []fileRowSpec {
	return []fileRowSpec{
		{Kind: sidebarRowProjectFiles, Index: -1, Key: "files", Name: "files"},
		{Kind: sidebarRowProjectGit, Index: -1, Key: "git", Name: "git"},
	}
}

// Otherwise reports a listing state the Git tab should not draw over.
func (v fileViewState) Otherwise() bool {
	return v.Elsewhere != "" || v.Err != ""
}

// ActivateGitEntry opens exactly the path and index side the drawn row names,
// on its captured pane, not whichever pane happens to have focus now.
func (m *OS) ActivateGitEntry(index int) tea.Cmd {
	if index < 0 || index >= len(m.gitStatus.entries) {
		return nil
	}
	e := m.gitStatus.entries[index]
	if e.Origin.Host != "" || e.Origin.AttachedHost != "" || e.Origin.CwdHost != "" || m.AttachedHost != "" {
		m.reviewNotify("That repository is on another machine. Attach there to review it", "info", m.Settings.NotificationDuration)
		return nil
	}
	// An untracked path has no diff to review; it opens as the file it is.
	if e.Letter == gitstatus.Untracked {
		return m.OpenDocPreview(e.Root, e.Path, false)
	}
	if !m.reviewSupported() || !m.IsDaemonSession {
		m.reviewNotify("This diff needs a local daemon with review-diff support", "info", m.Settings.NotificationDuration)
		return nil
	}
	if e.Origin.Session == "" || e.Origin.Window == "" || e.Root == "" {
		return nil
	}
	// Starting another row invalidates the previous read, even if it is still
	// pending. Index diffs have no fan/base comparison to probe.
	r := &m.review
	*r = reviewState{gen: r.gen + 1, pending: true, who: e.Origin.Session, openedAt: time.Now(), split: r.split}
	r.query = reviewQuery{
		Session: e.Origin.Session, Window: e.Origin.Window,
		Path: e.Path, OldPath: e.Orig, Index: e.Index, Worktree: e.Root,
	}
	return m.reviewLoadCmd(r.query, false)
}
