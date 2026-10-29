// Package gitstatus reads one repository's changed paths for the rail.
//
// It is deliberately narrower than a Git client: status letters, the two
// index sides kept apart, branch and upstream drift, and the decorations a
// tree row needs. Nothing here stages, commits or opens a file.
//
// The read is one bounded subprocess. A repository on a stalled mount costs
// one refresh, never the client's update loop; callers run Read off that loop.
package gitstatus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/getan/tuios-mini/internal/gitstate"
)

// Letter is a file's Git status. The vocabulary is VS Code's and
// herdr-sidebar's, so the two views and the diff cannot disagree.
type Letter byte

const (
	Modified    Letter = 'M'
	Added       Letter = 'A'
	Deleted     Letter = 'D'
	Renamed     Letter = 'R'
	Copied      Letter = 'C'
	Untracked   Letter = 'U'
	Conflicted  Letter = '!'
	TypeChanged Letter = 'T'
	Ignored     Letter = 'I'
)

// Entry is one changed path on one side of the index.
type Entry struct {
	// Path is repo-relative, slash separated.
	Path string
	// Orig is a rename or copy source, empty otherwise.
	Orig   string
	Letter Letter
}

// ReadError is a read that did not produce a trustworthy answer. A caller
// must show it rather than an empty, seemingly clean tree.
type ReadError struct{ Err error }

func (e *ReadError) Error() string { return "git status: " + e.Err.Error() }
func (e *ReadError) Unwrap() error { return e.Err }

// Status is one repository's answer. An error is returned as a value, not by
// omitting the tree, so the rail can say why it is showing nothing.
type Status struct {
	Root        string
	Branch      string
	Upstream    string
	Ahead       int
	Behind      int
	Detached    bool
	HasUpstream bool
	Staged      []Entry
	Unstaged    []Entry
	Untracked   []Entry
	Conflicts   []Entry
	Err         string
}

// Changed reports how many paths carry a status, the untracked included.
func (s Status) Changed() int {
	return len(s.Staged) + len(s.Unstaged) + len(s.Untracked) + len(s.Conflicts)
}

// Empty reports a clean, successfully read tree.
func (s Status) Empty() bool { return s.Err == "" && s.Changed() == 0 }

// ReadTimeout bounds the one subprocess Read runs.
const ReadTimeout = 3 * time.Second

// Read reads the repository containing dir. A directory outside a repository
// answers an empty Status with Err == "not a repository"; a failed git answers
// Err with git's own message, never a clean tree.
func Read(ctx context.Context, dir string) Status {
	_, _, root, ok := gitstate.Locate(dir)
	if !ok {
		return Status{Err: "not a git repository"}
	}
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	// A background observation must not take index.lock to refresh stat data.
	// normal, not all: an untracked directory is one entry instead of a walk
	// through every file below it. A workspace whose pane starts in a large
	// home or scratch tree must not enumerate that tree just to refresh the
	// rail. Tracked paths and untracked files at the directory's own level are
	// still reported.
	cmd := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", root, "status",
		"--porcelain=v1", "-z", "--branch", "--untracked-files=normal", "--renames")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return Status{Root: root, Err: "git status did not answer in time"}
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Status{Root: root, Err: msg}
	}
	if len(out) > 8<<20 {
		return Status{Root: root, Err: "git status output is larger than 8 MiB"}
	}
	st := Parse(out)
	st.Root = root
	return st
}

// Parse reads `git status --porcelain=v1 -z --branch` output.
func Parse(out []byte) Status {
	var st Status
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) == 0 {
			continue
		}
		if bytes.HasPrefix(f, []byte("## ")) {
			st.Branch, st.Upstream, st.Ahead, st.Behind, st.Detached = parseBranch(string(f[3:]))
			st.HasUpstream = st.Upstream != ""
			continue
		}
		if len(f) < 3 {
			continue
		}
		x, y := f[0], f[1]
		rel := string(f[3:])
		orig := ""
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			if i+1 < len(fields) {
				i++
				orig = string(fields[i])
			}
		}
		switch {
		case x == '?' && y == '?':
			st.Untracked = append(st.Untracked, Entry{Path: rel, Letter: Untracked})
		case x == '!' && y == '!':
			// Ignored paths are not listed; the tree decorates them from its
			// own ignore rules when that is on.
		case isConflict(x, y):
			st.Conflicts = append(st.Conflicts, Entry{Path: rel, Letter: Conflicted})
		default:
			if l := letterFor(x); l != 0 {
				st.Staged = append(st.Staged, Entry{Path: rel, Orig: orig, Letter: l})
			}
			if l := letterFor(y); l != 0 {
				st.Unstaged = append(st.Unstaged, Entry{Path: rel, Orig: orig, Letter: l})
			}
		}
	}
	return st
}

func isConflict(x, y byte) bool {
	switch string([]byte{x, y}) {
	case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
		return true
	}
	return false
}

func letterFor(c byte) Letter {
	switch c {
	case 'M':
		return Modified
	case 'A':
		return Added
	case 'D':
		return Deleted
	case 'R':
		return Renamed
	case 'C':
		return Copied
	case 'T':
		return TypeChanged
	}
	return 0
}

// parseBranch reads "main...origin/main [ahead 1, behind 2]", "main" or
// "HEAD (no branch)".
func parseBranch(s string) (branch, upstream string, ahead, behind int, detached bool) {
	if strings.HasPrefix(s, "HEAD (no branch)") {
		return "HEAD", "", 0, 0, true
	}
	if i := strings.Index(s, " ["); i >= 0 {
		for _, part := range strings.Split(strings.TrimSuffix(s[i+2:], "]"), ", ") {
			n := 0
			if _, err := fmt.Sscanf(part, "ahead %d", &n); err == nil {
				ahead = n
			}
			if _, err := fmt.Sscanf(part, "behind %d", &n); err == nil {
				behind = n
			}
		}
		s = s[:i]
	}
	if i := strings.Index(s, "..."); i >= 0 {
		return s[:i], s[i+3:], ahead, behind, false
	}
	return s, "", ahead, behind, false
}

// Decorations maps repo-relative file paths and folders to one letter. A
// folder carries the loudest status below it, so a collapsed tree still says
// something changed; a conflict outranks a tracked change, which outranks an
// untracked file.
type Decorations struct {
	files map[string]Letter
	dirs  map[string]Letter
}

// BuildDecorations folds both index sides into one view. The working-tree
// side wins an equally loud tie: that is the state on disk.
func BuildDecorations(st Status) Decorations {
	d := Decorations{files: map[string]Letter{}, dirs: map[string]Letter{}}
	for _, group := range [][]Entry{st.Staged, st.Unstaged, st.Untracked, st.Conflicts} {
		for _, e := range group {
			d.add(e.Path, e.Letter)
			if e.Orig != "" {
				d.add(e.Orig, Deleted)
			}
		}
	}
	return d
}

func rank(l Letter) int {
	switch l {
	case Conflicted:
		return 3
	case Modified, Added, Deleted, Renamed, Copied, TypeChanged:
		return 2
	case Untracked:
		return 1
	}
	return 0
}

func (d Decorations) add(rel string, letter Letter) {
	rel = path.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "../") {
		return
	}
	if cur, ok := d.files[rel]; !ok || rank(letter) >= rank(cur) {
		d.files[rel] = letter
	}
	mark := letter
	switch rank(letter) {
	case 3:
		mark = Conflicted
	case 2:
		mark = Modified
	}
	for dir := path.Dir(rel); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if cur, ok := d.dirs[dir]; !ok || rank(mark) > rank(cur) {
			d.dirs[dir] = mark
		}
	}
}

// File returns the file's own letter, if it has one.
func (d Decorations) File(rel string) (Letter, bool) {
	l, ok := d.files[path.Clean(rel)]
	return l, ok
}

// Dir returns a folder's aggregate letter, if anything below it changed.
func (d Decorations) Dir(rel string) (Letter, bool) {
	l, ok := d.dirs[path.Clean(rel)]
	return l, ok
}

// ColorToken is the semantic colour name the UI maps through the theme. It is
// a name rather than a colour so the palette stays in internal/theme.
func (l Letter) ColorToken() string {
	switch l {
	case Modified, TypeChanged:
		return "git_modified"
	case Added:
		return "git_added"
	case Untracked, Renamed, Copied:
		return "git_untracked"
	case Deleted:
		return "git_deleted"
	case Conflicted:
		return "git_conflict"
	case Ignored:
		return "git_ignored"
	}
	return ""
}

// String is the letter shown beside a file. It is always visible, so colour is
// never the only carrier of the status.
func (l Letter) String() string {
	if l == Untracked {
		return "U"
	}
	return string(rune(l))
}

// ParseAhead is exported for tests that build a header by hand.
func ParseAhead(s string) (int, error) { return strconv.Atoi(strings.TrimSpace(s)) }

// ErrNotRepository is the sentinel a caller can test for.
var ErrNotRepository = errors.New("not a git repository")
