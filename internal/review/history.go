package review

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// HistoryKind names one Git tab drawer.
type HistoryKind string

const (
	HistoryCommits  HistoryKind = "commits"
	HistoryGraph    HistoryKind = "graph"
	HistoryBranches HistoryKind = "branches"
	HistoryTags     HistoryKind = "tags"
	HistoryStashes  HistoryKind = "stashes"
	HistoryRemotes  HistoryKind = "remotes"
	HistoryFile     HistoryKind = "file"
	HistoryRepos    HistoryKind = "repos"
	HistoryCommit   HistoryKind = "commit"
)

// HistoryResult is one drawer's rows, already formatted for the overlay.
type HistoryResult struct {
	Kind  HistoryKind
	Title string
	Rows  []string
}

// History reads one of the read-only Git drawers. limit caps rows; a value
// outside 1..500 means 100.
func History(ctx context.Context, dir string, kind HistoryKind, path string, limit int) (HistoryResult, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	title := "commits"
	var args []string
	switch kind {
	case HistoryCommits:
		args = []string{"log", "--date=short", "--format=%h %ad %an %s", "-n", strconv.Itoa(limit)}
	case HistoryGraph:
		title = "graph"
		args = []string{"log", "--graph", "--oneline", "--decorate", "-n", strconv.Itoa(limit)}
	case HistoryBranches:
		title = "branches"
		args = []string{"branch", "-a", "--no-color", "--format=%(refname:short) %(upstream:short) %(committerdate:short) %(subject)"}
	case HistoryTags:
		title = "tags"
		args = []string{"tag", "--sort=-creatordate", "--format=%(refname:short) %(creatordate:short)"}
	case HistoryStashes:
		title = "stashes"
		args = []string{"stash", "list", "--format=%gd %ci %s"}
	case HistoryRemotes:
		title = "remotes"
		args = []string{"remote", "-v"}
	case HistoryFile:
		title = "file history"
		if path == "" {
			return HistoryResult{}, errors.New("file history needs a path")
		}
		if err := ValidPath(path); err != nil {
			return HistoryResult{}, err
		}
		args = []string{"log", "--follow", "--date=short", "--format=%h %ad %an %s", "-n", strconv.Itoa(limit), "--", path}
	default:
		return HistoryResult{}, fmt.Errorf("unknown history kind %q", kind)
	}
	out, err := git(ctx, dir, nil, args...)
	if err != nil {
		return HistoryResult{}, err
	}
	if kind == HistoryRemotes {
		return HistoryResult{Kind: kind, Title: title, Rows: remoteRows(out, limit)}, nil
	}
	rows := make([]string, 0, limit)
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			rows = append(rows, line)
		}
		if len(rows) >= limit {
			break
		}
	}
	return HistoryResult{Kind: kind, Title: title, Rows: rows}, nil
}

// CommitDetail reads one commit only after the person asks for it. The list
// drawer carries just the short hash; the patch, stat and message are fetched
// here on the second-level open, never while the commits drawer is loading.
func CommitDetail(ctx context.Context, dir, ref string, limit int) (HistoryResult, error) {
	if err := ValidRef(ref); err != nil {
		return HistoryResult{}, err
	}
	if limit < 1 || limit > 500 {
		limit = 500
	}
	out, err := git(ctx, dir, nil,
		"show", "--no-color", "--date=short", "--decorate=short",
		"--format=commit %H%nAuthor: %an <%ae>%nDate:   %ad%n%n    %s%n",
		"--stat", "--patch", "--find-renames", ref,
	)
	if err != nil {
		return HistoryResult{}, err
	}
	rows := make([]string, 0, limit)
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			rows = append(rows, line)
		}
		if len(rows) >= limit {
			break
		}
	}
	title := "commit " + ref
	if len(title) > 20 {
		title = title[:20]
	}
	return HistoryResult{Kind: HistoryCommit, Title: title, Rows: rows}, nil
}

// remoteRows turns `git remote -v` into one row per name and URL. Git prints
// fetch and push separately, which made the drawer show "origin" twice; the
// address is the useful part, and a remote whose push URL differs keeps both
// addresses with the direction that uses each one.
func remoteRows(out string, limit int) []string {
	rows := make([]string, 0, 4)
	seen := map[string]bool{}
	named := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimRight(line, "\r"))
		if len(fields) < 2 {
			continue
		}
		name, address := fields[0], fields[1]
		key := name + "\x00" + address
		if seen[key] {
			continue
		}
		seen[key] = true
		row := name + "  " + address
		if named[name] && len(fields) > 2 {
			row += " (" + strings.Trim(fields[2], "()") + ")"
		}
		named[name] = true
		rows = append(rows, row)
		if len(rows) >= limit {
			break
		}
	}
	return rows
}

// TopLevel is the repository root a directory is inside, or an error when it
// is not in one.
func TopLevel(ctx context.Context, dir string) (string, error) {
	out, err := git(ctx, dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
