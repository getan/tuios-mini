package app

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/getan/tuios-mini/internal/gitstatus"
	"github.com/getan/tuios-mini/internal/review"
)

// gitTabMaxEntries bounds the rows the Git tab builds in one frame: one
// screenful plus a little scroll, not the whole repository. A very large
// changed set would otherwise allocate and draw every row before the next key
// is handled; the remainder is reported instead.
const gitTabMaxEntries = 50

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

	// Decorations for the Files tab: one token per changed path, and one per
	// directory holding a changed path, the strongest child winning. Rebuilt
	// with each reading rather than per frame, so drawing a listing is map
	// reads and a 2000-name tree costs nothing on the render path.
	fileDeco map[string]string
	dirDeco  map[string]string
}

// gitStartupDelay keeps the Git tab's first status read and watcher off the
// startup path. The first frame and the input reader are what a person is
// waiting for; a repository on a slow mount can wait two seconds for its
// branch and changed-path reading without being noticed.
const gitStartupDelay = 2 * time.Second

// gitStartupMsg is the end of that delay.
type gitStartupMsg struct{}

// gitStartupReady reports whether the startup gate lets git work begin.
func (m *OS) gitStartupReady() bool {
	return m.gitStartupDone || m.frameRate.program == nil
}

// GitStatusMsg carries a finished reading back to the loop.
type GitStatusMsg struct {
	Gen    uint64
	Origin gitStatusOrigin
	Status gitstatus.Status
	// FileDeco and DirDeco are the path decorations built in the command
	// goroutine. Keeping the fold off Update matters for a repository with a
	// large changed set: the status itself is already read off the loop.
	FileDeco, DirDeco map[string]string
}

// GitTabActive reports whether the project section is showing changes.
func (m *OS) GitTabActive() bool { return m.projectTab == 1 }

// SetProjectTab switches the project section between files (0) and git (1).
func (m *OS) SetProjectTab(tab int) tea.Cmd {
	if tab < 0 || tab > 1 || tab == m.projectTab {
		return nil
	}
	m.projectTab = tab
	// Switching either way must not show a stale reading: the Files tab wears
	// the same decorations the Git tab lists, so both directions ask now rather
	// than wait for the beat that follows the next message.
	m.gitStatus.at = time.Time{}
	return m.GitStatusSyncCmd()
}

// GitStatusSyncCmd reads the repository's changed paths on the project
// section's beat. It is deliberately separate from gitstate.Read: that is
// branch drift and is cheap; this walks the working tree, so it runs only while
// the section is shown — either tab, because the Files tab colours a name (and
// a folder holding it) from the same reading.
//
// The beat is a fallback now: file events start the read normally, and this
// interval only reconciles what a watcher cannot see. See git_watch.go.
const gitStatusFallbackInterval = 30 * time.Second

func (m *OS) GitStatusSyncCmd() tea.Cmd {
	if !m.gitStartupReady() {
		return nil
	}
	// The Git tab is the project section's other view; a caller that selected it
	// directly (a host, a test) asks for a reading even before the rail decides
	// the section is visible. The Files tab needs the section to draw at all,
	// which filesSectionEnabled already covers.
	if !m.GitTabActive() && !m.filesSectionEnabled() {
		m.stopGitWatcher()
		return nil
	}
	w := m.GetFocusedWindow()
	want := gitStatusOrigin{Session: m.SessionName, Dir: m.filesWantDir(), AttachedHost: m.AttachedHost}
	if w != nil {
		want.Window, want.Host, want.CwdHost = w.ID, w.Host, w.CwdHost
	}
	v := &m.gitStatus
	if want != v.asked {
		m.stopGitWatcher()
		v.gen++
		v.asked, v.inFlight, v.at = want, false, time.Time{}
	}
	// Never pass a remote directory to this machine's git, even if it happens
	// to exist here too. The remote files listing is not local read authority.
	if w == nil || want.Dir == "" || want.Host != "" || want.AttachedHost != "" || want.CwdHost != "" {
		m.stopGitWatcher()
		return nil
	}
	if v.inFlight || time.Since(v.at) < gitStatusFallbackInterval {
		return nil
	}
	v.gen++
	gen := v.gen
	v.inFlight, v.at = true, time.Now()
	return func() tea.Msg {
		status := gitstatus.Read(context.Background(), want.Dir)
		fileDeco, dirDeco := gitDecorations(status)
		return GitStatusMsg{
			Gen: gen, Origin: want, Status: status,
			FileDeco: fileDeco, DirDeco: dirDeco,
		}
	}
}

// ApplyGitStatus installs a reading unless the tab moved on.
func (m *OS) ApplyGitStatus(msg GitStatusMsg) {
	v := &m.gitStatus
	if msg.Gen != v.gen || msg.Origin != v.asked {
		return
	}
	v.Dir, v.Status, v.Origin, v.inFlight = msg.Origin.Dir, msg.Status, msg.Origin, false
	v.fileDeco, v.dirDeco = msg.FileDeco, msg.DirDeco
	if v.fileDeco == nil && v.dirDeco == nil {
		// A caller that did not run through GitStatusSyncCmd (a test, or an
		// older host message) still gets a usable view.
		v.buildDecorations()
	}
}

// gitDecorationStrength ranks the tokens so a directory wears the most urgent
// change under it. A conflict must not be hidden by a modification two files
// down the tree, and an ignored path is never worth colouring a folder for.
func gitDecorationStrength(token string) int {
	switch token {
	case "git_conflict":
		return 6
	case "git_deleted":
		return 5
	case "git_modified":
		return 4
	case "git_added", "git_renamed":
		return 3
	case "git_untracked":
		return 2
	case "git_ignored":
		return 1
	}
	return 0
}

func strongerGitToken(a, b string) string {
	if gitDecorationStrength(b) > gitDecorationStrength(a) {
		return b
	}
	return a
}

// buildDecorations folds one reading into the two lookup tables the Files tab
// draws from. Every changed path is recorded as itself and rolled up into each
// directory above it, so a folder whose contents changed is visible even when
// the changed file is not on screen.
func (v *gitStatusView) buildDecorations() {
	v.fileDeco, v.dirDeco = gitDecorations(v.Status)
}

// gitDecorations is buildDecorations without a view, so the fold can run in
// the status command's goroutine.
func gitDecorations(status gitstatus.Status) (fileDeco, dirDeco map[string]string) {
	if status.Err != "" || status.Root == "" {
		return nil, nil
	}
	fileDeco = make(map[string]string)
	dirDeco = make(map[string]string)
	add := func(entries []gitstatus.Entry) {
		for _, e := range entries {
			token := e.Letter.ColorToken()
			if token == "" {
				continue
			}
			fileDeco[e.Path] = strongerGitToken(fileDeco[e.Path], token)
			for dir := path.Dir(e.Path); dir != "." && dir != "/"; dir = path.Dir(dir) {
				dirDeco[dir] = strongerGitToken(dirDeco[dir], token)
			}
		}
	}
	add(status.Conflicts)
	add(status.Staged)
	add(status.Unstaged)
	add(status.Untracked)
	return fileDeco, dirDeco
}

// fileGitToken is the ink a Files-tab row wears: the entry's own status for a
// file, and the strongest status of anything under it for a directory. Empty
// when the reading belongs to another directory — the tab may still be showing
// the previous pane's tree while the new read is in flight — or when the tree
// is clean.
func (m *OS) fileGitToken(name string, dir bool) string {
	v := &m.gitStatus
	if name == "" || v.Dir == "" || v.Dir != m.filesView.Dir || v.Status.Err != "" {
		return ""
	}
	// gitstatus reports paths relative to the repository root, while a row
	// carries the name inside the listing's directory. Only a listing at the
	// root had the two agree; inside cmd/ the file is "test.go" and the entry
	// is "cmd/test.go", so every nested file was drawn plain while its folder
	// turned green.
	key := name
	if root := v.Status.Root; root != "" && v.Dir != root {
		if rel, err := filepath.Rel(root, v.Dir); err == nil && rel != "." {
			key = path.Join(filepath.ToSlash(rel), name)
		}
	}
	if dir {
		return v.dirDeco[key]
	}
	return v.fileDeco[key]
}

// gitBranchLine is the Git tab's header: the checked-out branch and how far it
// has drifted from its upstream. herdr-sidebar prints the same "↑N ↓M" beside
// the branch, and it is the pair a fetch changes — the fact a person checks
// before deciding whether there is anything to sync. No upstream prints the
// branch alone rather than a pair of zeros.
func gitBranchLine(st gitstatus.Status) string {
	name := st.Branch
	if name == "" {
		if !st.Detached {
			return ""
		}
		name = "detached"
	}
	if !st.HasUpstream || (st.Ahead == 0 && st.Behind == 0) {
		return name
	}
	return name + " ↑" + strconv.Itoa(st.Ahead) + " ↓" + strconv.Itoa(st.Behind)
}

// gitTabRows is what the Git tab draws: the one-row tab bar, the branch and
// its drift, then one header per non-empty group and its entries.
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
		if line := gitBranchLine(st); line != "" {
			rows = append(rows, fileRowSpec{Note: true, Name: line, Key: "branch"})
		}
	}
	rows = append(rows, fileRowSpec{
		Kind: sidebarRowGitCommit,
		Key:  "commit",
		Name: "Commit changes",
	})
	add := func(title, side string, entries []gitstatus.Entry) {
		if len(entries) == 0 {
			return
		}
		rows = append(rows, fileRowSpec{Note: true, Name: title + " " + strconv.Itoa(len(entries)), Key: title})
		for _, e := range entries {
			if len(m.gitStatus.entries) >= gitTabMaxEntries {
				return
			}
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
		rows = append(rows, fileRowSpec{Note: true, Name: "clean"})
		return appendGitHistoryRows(rows)
	}
	if total, drawn := st.Changed(), len(m.gitStatus.entries); drawn < total {
		rows = append(rows, fileRowSpec{
			Note: true,
			Name: fmt.Sprintf("%d more changed paths not drawn", total-drawn),
		})
	}
	return appendGitHistoryRows(rows)
}

// appendGitHistoryRows adds the source-control drawer's read-only views in the
// same order as the Herdr sidebar: history after the changed paths, reachable
// by clicking a row rather than by remembering a prefix chord.
func appendGitHistoryRows(rows []fileRowSpec) []fileRowSpec {
	for i, kind := range gitTabHistoryKinds {
		rows = append(rows, fileRowSpec{
			Kind:  sidebarRowGitHistory,
			Index: i,
			Key:   "history:" + string(kind),
			Name:  gitTabHistoryLabel(kind),
		})
	}
	return rows
}

var gitTabHistoryKinds = []review.HistoryKind{
	review.HistoryGraph,
	review.HistoryCommits,
	review.HistoryBranches,
	review.HistoryRemotes,
	review.HistoryStashes,
	review.HistoryTags,
}

func gitTabHistoryLabel(kind review.HistoryKind) string {
	switch kind {
	case review.HistoryGraph:
		return "Graph"
	case review.HistoryCommits:
		return "Commits"
	case review.HistoryBranches:
		return "Branches"
	case review.HistoryRemotes:
		return "Remotes"
	case review.HistoryStashes:
		return "Stashes"
	case review.HistoryTags:
		return "Tags"
	}
	return "History"
}

// projectTabRows is the tab bar every project view starts with. It is one row:
// sidebarProjectTabs draws both tokens inside it and publishes one click target
// per token, so the mouse can tell files from git while the keyboard walks the
// same two rows it always did.
func (m *OS) projectTabRows() []fileRowSpec {
	return []fileRowSpec{
		{Kind: sidebarRowProjectTabs, Index: -1, Key: "tabs"},
	}
}

// Otherwise reports a listing state the Git tab should not draw over.
func (v fileViewState) Otherwise() bool {
	return v.Elsewhere != "" || v.Err != ""
}

// ActivateGitEntry opens exactly the path and index side the drawn row names,
// on its captured pane, not whichever pane happens to have focus now. Every
// changed path opens the review, untracked included: the base diff writes the
// working state as a tree, so a new file is there as an addition and review is
// where an addition is read. It used to open the file preview instead, which
// left the one row a person most wants to read with no diff and no notes.
func (m *OS) ActivateGitEntry(index int) tea.Cmd {
	if index < 0 || index >= len(m.gitStatus.entries) {
		return nil
	}
	e := m.gitStatus.entries[index]
	if e.Origin.Host != "" || e.Origin.AttachedHost != "" || e.Origin.CwdHost != "" || m.AttachedHost != "" {
		m.reviewNotify("That repository is on another machine. Attach there to review it", "info", m.Settings.NotificationDuration)
		return nil
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
