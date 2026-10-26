package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/review"
)

// Scoped reviews keep the original note editor and verbs. The security
// boundary here is scope: a note reply can contain every note of the pane,
// but only notes visible in this file may be edited or included by S. A focus
// change must not retarget those verbs, and automated keys keep their refusal.
func TestProjectReviewKeepsCommentsScopedThroughTheirLifecycle(t *testing.T) {
	m, f := reviewOS(t)
	repo := t.TempDir()
	const path = "api/retry.go"
	f.diff["worktree"], f.diff["index"] = repo, review.IndexStaged
	f.notes = []review.Note{{ID: "elsewhere", Path: "docs/retry.md", Line: 1, Side: "new", Text: "unrelated note", By: "human"}}
	m.review = reviewState{gen: 1, pending: true}
	q := reviewQuery{Session: "api-2", Window: "w-1", Path: path, Index: review.IndexStaged, Worktree: repo}
	m.applyReviewDiff(m.reviewLoadCmd(q, false)().(ReviewDiffMsg))
	if !m.ReviewOpen() {
		t.Fatal("the scoped review did not open")
	}
	// Move focus away before doing any comment work.
	m.SessionName, m.FocusedWindow = "other-session", -1
	place := func(kind reviewRowKind) {
		t.Helper()
		for i, row := range m.reviewRows(m.reviewRowsWidth()) {
			if row.kind == kind {
				m.review.cursor = i
				return
			}
		}
		t.Fatalf("no row of kind %v", kind)
	}
	submit := func(text string) {
		t.Helper()
		m.ReviewEditorType(text)
		cmd := m.ReviewEditorSubmit()
		if cmd == nil {
			t.Fatal("comment submission produced no command")
		}
		m.applyReviewNotes(cmd().(ReviewNotesMsg))
		if len(m.review.files) != 1 || m.review.files[0].path != path {
			t.Fatal("comment reply widened the file list")
		}
		for _, n := range m.review.notes {
			if n.Path != path {
				t.Fatalf("comment from another file entered the scoped view: %+v", n)
			}
		}
	}
	place(reviewRowLine)
	m.ReviewNote(false)
	submit("please explain this line")
	place(reviewRowNote)
	m.ReviewEditNote()
	m.review.editor.draft = ""
	submit("updated comment")
	frame := ansi.Strip(m.renderReview())
	if !strings.Contains(frame, "updated comment") || strings.Contains(frame, "unrelated note") {
		t.Fatalf("comment not shown in the existing diff UI:\n%s", frame)
	}
	place(reviewRowHunk)
	m.ReviewNote(true)
	submit("please test this hunk")
	m.applyReviewDiff(m.reviewReload()().(ReviewDiffMsg))
	if len(m.review.notes) != 2 {
		t.Fatalf("reload lost comments: %+v", m.review.notes)
	}
	place(reviewRowNote)
	cmd := m.ReviewResolveNote()
	if cmd == nil {
		t.Fatal("resolve produced no command")
	}
	m.applyReviewNotes(cmd().(ReviewNotesMsg))
	if len(m.review.notes) != 1 {
		t.Fatalf("resolve lost the wrong notes: %+v", m.review.notes)
	}
	if cmd := m.ReviewSend(); cmd == nil {
		t.Fatal("the original send-review action is missing")
	} else {
		_ = cmd()
	}
	last := f.calls[len(f.calls)-1]
	ids, _ := last.params["ids"].([]string)
	if last.verb != "send-review" || len(ids) != 1 || ids[0] != m.review.notes[0].ID {
		t.Fatalf("S did not send exactly the visible unsent note: %+v", last)
	}
	for _, call := range f.calls {
		if call.verb != "review-note" && call.verb != "send-review" {
			continue
		}
		if call.params["session"] != "api-2" || call.params["window"] != "w-1" || call.params["human_nonce"] != "nonce-1" {
			t.Fatalf("comment followed current focus or lost the human nonce: %+v", call)
		}
		if call.params["expected_worktree"] != repo {
			t.Fatalf("comment mutation has no repository guard: %+v", call)
		}
	}
	before := len(f.calls)
	m.ProcessingRemoteKeys = true
	if m.ReviewSend() != nil {
		t.Fatal("send-keys can send human comments")
	}
	place(reviewRowNote)
	if m.ReviewResolveNote() != nil {
		t.Fatal("send-keys can remove human comments")
	}
	m.ReviewEditNote()
	m.ReviewEditorType("automated")
	if m.ReviewEditorSubmit() != nil || len(f.calls) != before {
		t.Fatal("automated edits escaped the original comment permission gate")
	}
}
