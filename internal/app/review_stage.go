package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/review"
)

// ReviewStageHunk stages or unstages the hunk under the cursor. It is the
// overlay's half of the Git tab's stage verbs: the same review-stage call, so
// a hunk staged here and one staged from the rail are the same index change.
func (m *OS) ReviewStageHunk(action string) tea.Cmd {
	return m.reviewStage(action, false)
}

// ReviewStageLine stages or unstages the added or removed line under the
// cursor. A context line has nothing to stage, so it asks for a changed line
// rather than quietly staging its whole hunk.
func (m *OS) ReviewStageLine(action string) tea.Cmd {
	return m.reviewStage(action, true)
}

func (m *OS) reviewStage(action string, lineOnly bool) tea.Cmd {
	r := &m.review
	if !r.open {
		return nil
	}
	if r.query.Against != "" {
		m.reviewNotify("Staging is for a review against the base, not a diff of two attempts", "info", m.Settings.NotificationDuration)
		return nil
	}
	e := r.currentFile()
	row, ok := m.reviewRowUnderCursor()
	if e == nil || e.file == nil || !ok || row.hunk < 0 || row.hunk >= len(e.file.Hunks) {
		m.reviewNotify("Move to a hunk of the diff first", "info", m.Settings.NotificationDuration)
		return nil
	}
	if lineOnly && row.kind != reviewRowLine {
		m.reviewNotify("Move to an added or removed line first", "info", m.Settings.NotificationDuration)
		return nil
	}
	// The side the overlay is showing decides which way the index can move.
	// The daemon would refuse the other way anyway; saying it here keeps the
	// refusal under the key that caused it.
	switch {
	case action == "stage" && r.query.Index == "staged":
		m.reviewNotify("This is the staged side. U unstages instead", "info", m.Settings.NotificationDuration)
		return nil
	case action == "unstage" && r.query.Index == "unstaged":
		m.reviewNotify("This is the working side. A stages instead", "info", m.Settings.NotificationDuration)
		return nil
	}
	h := &e.file.Hunks[row.hunk]
	params := map[string]any{
		"action": action, "session": r.query.Session, "window": r.query.Window,
		"path": e.path, "hunk": h.Header, "context": 3,
	}
	if lineOnly {
		idx, ok := reviewStageLine(h, row)
		if !ok {
			m.reviewNotify("That line is context. Move to an added or removed line", "info", m.Settings.NotificationDuration)
			return nil
		}
		params["lines"] = []int{idx}
	}
	call, nonce := m.inboxCaller(), m.inboxNonce()
	if nonce != "" {
		params["human_nonce"] = nonce
	}
	path := e.path
	return func() tea.Msg {
		if _, err := call("review-stage", params, 15*time.Second); err != nil {
			return gitActionMsg{Err: err.Error()}
		}
		verb := "Staged "
		if action == "unstage" {
			verb = "Unstaged "
		}
		what := "hunk"
		if lineOnly {
			what = "line"
		}
		return gitActionMsg{OK: verb + what + " in " + path + "."}
	}
}

// reviewStageLine is the hunk-line index the cursor names. In the split layout
// a row holds the new side in line and the old side in other; either can be the
// changed line the cursor is on. Context lines are not stageable on their own.
func reviewStageLine(h *review.Hunk, row reviewRow) (int, bool) {
	for _, i := range []int{row.line, row.other} {
		if i < 0 || i >= len(h.Lines) {
			continue
		}
		if h.Lines[i].Op != review.OpContext {
			return i, true
		}
	}
	return 0, false
}
