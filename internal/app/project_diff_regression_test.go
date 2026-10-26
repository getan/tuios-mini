package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/review"
	"github.com/getan/tuios-mini/internal/terminal"
	"github.com/getan/tuios-mini/internal/testutil"
)

// These integration regressions use a real Git index and working tree. The
// transport is in process so focus changes and out-of-order replies can be
// scheduled deterministically, rather than raced against a PTY.
// Failure modes: both rows request the same diff, a selected path includes
// unrelated files/notes, reload loses the filter, a focus/cwd change retargets
// the request, or an A -> B -> A status reply is accepted out of order.
func TestProjectDiffReadsTheSelectedPathAndIndexSide(t *testing.T) {
	repo := testutil.GitRepo(t)
	path := "both.txt"
	writeFile(t, repo, path, "base-only\n")
	writeFile(t, repo, "other.txt", "other-base\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-qm", "base")
	writeFile(t, repo, path, "staged-only\n")
	testutil.Git(t, repo, "add", path)
	writeFile(t, repo, path, "working-only\n")
	writeFile(t, repo, "other.txt", "unrelated-change\n")

	m, _ := reviewOS(t)
	m.SessionName = "source-session"
	w := m.GetFocusedWindow()
	w.Cwd = repo
	originID := w.ID
	m.projectTab = 1
	m.ApplyGitStatus(m.GitStatusSyncCmd()().(GitStatusMsg))
	rows := m.gitTabRows()
	var selected []fileRowSpec
	for _, row := range rows {
		if row.Kind == sidebarRowGitEntry && row.Name == path {
			selected = append(selected, row)
		}
	}
	if len(selected) != 2 {
		t.Fatalf("both sides must be selectable: %+v", rows)
	}
	if selected[0].Key == selected[1].Key {
		t.Error("staged and unstaged rows have the same navigation identity")
	}

	// A different pane/session now has focus; the row still names its source.
	m.Windows = append(m.Windows, &terminal.Window{ID: "other-pane", Cwd: t.TempDir(), Workspace: m.CurrentWorkspace})
	m.FocusedWindow = len(m.Windows) - 1
	m.SessionName = "other-session"
	for i, side := range []string{review.IndexStaged, review.IndexUnstaged} {
		t.Run(side, func(t *testing.T) {
			m.CloseReview()
			calls := 0
			m.SetInboxVerbCaller(func(verb string, params map[string]any, _ time.Duration) (json.RawMessage, error) {
				if verb != "review-diff" {
					return nil, fmt.Errorf("unexpected verb %s", verb)
				}
				calls++
				if params["session"] != "source-session" || params["window"] != originID {
					t.Errorf("request followed focus: %v", params)
				}
				if params["index"] != side {
					t.Errorf("index = %v, want %s", params["index"], side)
				}
				paths, _ := params["paths"].([]string)
				if len(paths) != 1 || paths[0] != path {
					t.Errorf("paths = %v, want only %s", params["paths"], path)
				}
				ctx := context.Background()
				opt, err := review.IndexOptions(ctx, repo, side)
				if err != nil {
					return nil, err
				}
				opt.Paths = paths
				opt.Context = 3
				diff, err := review.Build(ctx, opt)
				if err != nil {
					return nil, err
				}
				return json.Marshal(map[string]any{
					"session": "source-session", "window": originID, "worktree": repo,
					"index": side, "base": diff.Base, "files": diff.Files, "totals": diff.Totals,
					"notes": []review.Note{{ID: "unrelated", Path: "other.txt", Text: "other note"}},
				})
			}, nil)
			cmd := m.ActivateGitEntry(selected[i].Index)
			if cmd == nil {
				t.Fatal("row opened no diff")
			}
			msg := cmd().(ReviewDiffMsg)
			m.applyReviewDiff(msg)
			if !m.ReviewOpen() || len(m.review.files) != 1 || m.review.files[0].path != path {
				t.Fatalf("not a single-file review: %+v, err %v", m.review.files, msg.Err)
			}
			frame := ansi.Strip(m.renderReview())
			want, absent := "base-only", "working-only"
			if side == review.IndexUnstaged {
				want, absent = "working-only", "base-only"
			}
			if !strings.Contains(frame, want) || !strings.Contains(frame, "staged-only") || strings.Contains(frame, absent) || strings.Contains(frame, "other.txt") {
				t.Fatalf("wrong diff on screen:\n%s", frame)
			}
			m.applyReviewDiff(m.reviewReload()().(ReviewDiffMsg))
			if calls != 2 {
				t.Fatalf("reload calls = %d", calls)
			}
			if cmd := m.ReviewToggleUncommitted(); cmd != nil {
				t.Fatal("u widened an index diff to a different comparison")
			}
			m.ReviewBasePrompt()
			if m.review.editor != nil {
				t.Fatal("base prompt can replace a scoped index comparison")
			}
		})
	}
}

func TestProjectDiffRejectsARepositoryChangedUnderThePane(t *testing.T) {
	m, f := reviewOS(t)
	repo := t.TempDir()
	other := t.TempDir()
	// A scoped request's answer must be checked before it can become a review.
	m.review = reviewState{gen: 1, pending: true}
	q := reviewQuery{Session: "source", Window: "pane", Path: "a.txt", Index: "staged", Worktree: repo}
	f.diff["worktree"] = other
	msg := m.reviewLoadCmd(q, false)().(ReviewDiffMsg)
	m.applyReviewDiff(msg)
	if msg.Err == nil || m.ReviewOpen() {
		t.Fatal("a diff from a different repository was displayed")
	}
}

func TestProjectStatusRejectsOutOfOrderAndRemoteReadings(t *testing.T) {
	m, _ := reviewOS(t)
	a, b := testutil.GitRepo(t), testutil.GitRepo(t)
	w := m.GetFocusedWindow()
	w.Cwd, m.projectTab = a, 1
	first := m.GitStatusSyncCmd()
	w.Cwd = b
	_ = m.GitStatusSyncCmd()
	w.Cwd = a
	last := m.GitStatusSyncCmd()
	newer := last().(GitStatusMsg)
	newer.Status.Branch = "newest"
	m.ApplyGitStatus(newer)
	m.ApplyGitStatus(first().(GitStatusMsg))
	if m.gitStatus.Status.Branch != "newest" {
		t.Fatal("an old A -> B -> A reply replaced the newest reading")
	}
	// Same path, different pane must also request its own reading.
	m.Windows = append(m.Windows, &terminal.Window{ID: "second", Cwd: a, Workspace: m.CurrentWorkspace})
	m.FocusedWindow = len(m.Windows) - 1
	if m.GitStatusSyncCmd() == nil {
		t.Fatal("two panes at the same directory share a request identity")
	}
	w = m.GetFocusedWindow()
	w.Host = "remote-host"
	if cmd := m.GitStatusSyncCmd(); cmd != nil {
		t.Fatal("a remote filesystem path was sent to local git")
	}
}

func TestProjectDiffRenameKeepsOnlyItsTwoPaths(t *testing.T) {
	// Include both names so a path-filtered rename stays a rename, not an
	// unrelated deletion or addition. Deletions must not fall back to Open.
	repo := testutil.GitRepo(t)
	writeFile(t, repo, "old.txt", "unchanged content\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-qm", "base")
	if err := os.Rename(filepath.Join(repo, "old.txt"), filepath.Join(repo, "new.txt")); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", "-A")
	m, f := reviewOS(t)
	m.GetFocusedWindow().Cwd, m.projectTab = repo, 1
	m.ApplyGitStatus(m.GitStatusSyncCmd()().(GitStatusMsg))
	rows := m.gitTabRows()
	for _, row := range rows {
		if row.Kind != sidebarRowGitEntry {
			continue
		}
		cmd := m.ActivateGitEntry(row.Index)
		if cmd == nil {
			t.Fatal("rename opened nothing")
		}
		_ = cmd()
		params := f.calls[0].params
		paths, _ := params["paths"].([]string)
		if len(paths) != 2 || paths[0] != "new.txt" || paths[1] != "old.txt" {
			t.Fatalf("rename paths = %v", params["paths"])
		}
		return
	}
	t.Fatal("rename row missing")
}
