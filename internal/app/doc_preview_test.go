package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/getan/tuios-mini/internal/gitstatus"
	"github.com/getan/tuios-mini/internal/overlay"
)

func previewOS(t *testing.T) (*OS, string) {
	t.Helper()
	m := keystrokeOS(t, 1, 100, 30)
	dir := t.TempDir()
	return m, dir
}

func openPreview(t *testing.T, m *OS, dir, name string, edit bool) {
	t.Helper()
	cmd := m.OpenDocPreview(dir, name, edit)
	if cmd == nil {
		t.Fatal("OpenDocPreview returned no command")
	}
	got := cmd()
	msg, ok := got.(docOpenMsg)
	if !ok {
		t.Fatalf("OpenDocPreview command returned %T", got)
	}
	m.HandleDocOpen(msg)
	if m.docPreview.err != "" {
		t.Fatalf("open failed: %s", m.docPreview.err)
	}
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDocPreviewEditsAndSaves(t *testing.T) {
	m, dir := previewOS(t)
	path := writeFile(t, dir, "a.txt", "hello")
	openPreview(t, m, dir, "a.txt", true)
	if !m.DocPreviewOpen() {
		t.Fatal("the sheet did not open")
	}
	if m.DocPreviewPath() != "a.txt" {
		t.Fatalf("path = %q", m.DocPreviewPath())
	}

	m.DocPreviewKey(tea.KeyPressMsg{Code: '!', Text: "!"})
	if !m.docPreview.buf.Dirty() {
		t.Fatal("typing did not dirty the buffer")
	}
	cmd := m.DocPreviewSave()
	msg, ok := cmd().(docSaveMsg)
	if !ok {
		t.Fatalf("save returned %T", cmd())
	}
	m.HandleDocSave(msg)
	if m.docPreview.note != "saved" {
		t.Fatalf("note = %q", m.docPreview.note)
	}
	if m.docPreview.buf.Dirty() {
		t.Fatal("the buffer is dirty after a save")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello!" {
		t.Fatalf("file = %q", body)
	}
}

func TestDocPreviewDirtyCloseAsksFirst(t *testing.T) {
	m, dir := previewOS(t)
	writeFile(t, dir, "a.txt", "hello")
	openPreview(t, m, dir, "a.txt", true)
	m.DocPreviewKey(tea.KeyPressMsg{Code: 'x', Text: "x"})

	m.CloseDocPreview(false)
	if !m.DocPreviewOpen() {
		t.Fatal("a dirty close closed without asking")
	}
	if !m.docPreview.confirm {
		t.Fatal("no confirmation was asked for")
	}
	m.CloseDocPreview(true)
	if m.DocPreviewOpen() {
		t.Fatal("the forced close left the sheet open")
	}
}

func TestDocPreviewRefusesABinaryFile(t *testing.T) {
	m, dir := previewOS(t)
	writeFile(t, dir, "blob.bin", "a\x00b")
	openPreview(t, m, dir, "blob.bin", true)
	if m.docPreview.doc.Editable {
		t.Fatal("a binary file was called editable")
	}
	m.DocPreviewKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.docPreview.buf.Dirty() {
		t.Fatal("a read-only document took text")
	}
	if cmd := m.DocPreviewSave(); cmd != nil {
		t.Fatal("a read-only document offered a save")
	}
}

func TestDocPreviewSaveConflictKeepsTheBuffer(t *testing.T) {
	m, dir := previewOS(t)
	path := writeFile(t, dir, "a.txt", "hello")
	openPreview(t, m, dir, "a.txt", true)
	m.DocPreviewKey(tea.KeyPressMsg{Code: '!', Text: "!"})

	// Move the file after it was read: the save must refuse rather than clobber.
	writeFile(t, dir, "a.txt", "changed elsewhere")
	msg, ok := m.DocPreviewSave()().(docSaveMsg)
	if !ok {
		t.Fatal("save returned the wrong message")
	}
	m.HandleDocSave(msg)
	if !m.docPreview.conflict {
		t.Fatalf("no conflict was reported: note = %q", m.docPreview.note)
	}
	if !m.docPreview.buf.Dirty() {
		t.Fatal("the local text was dropped")
	}
	body, _ := os.ReadFile(path)
	if string(body) != "changed elsewhere" {
		t.Fatalf("the other writer's text was overwritten: %q", body)
	}
}

func TestDocPreviewClickPlacesTheCaret(t *testing.T) {
	m, dir := previewOS(t)
	writeFile(t, dir, "a.txt", "alpha\nbravo\ncharlie")
	openPreview(t, m, dir, "a.txt", true)
	m.GetCanvas(true) // records the body rectangle

	line := 1
	y := m.docPreview.body.Y0 + line - m.docPreview.scroll
	x := m.docPreview.body.X0 + m.docPreview.gutter + 2
	m.DocPreviewClick(x, y)
	got := m.docPreview.buf.Cursor()
	if want := len("alpha\nbr"); got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
}

func TestGitTabGroupsChangesAndOpensUntracked(t *testing.T) {
	m, dir := previewOS(t)
	writeFile(t, dir, "new.txt", "fresh")
	m.filesView.Dir = dir
	m.gitStatus = gitStatusView{
		Dir: dir,
		Status: gitstatus.Status{
			Root:   dir,
			Branch: "main",
			Unstaged: []gitstatus.Entry{
				{Path: "a.go", Letter: gitstatus.Modified},
			},
			Untracked: []gitstatus.Entry{
				{Path: "new.txt", Letter: gitstatus.Untracked},
			},
		},
	}
	m.projectTab = 1
	rows := m.gitTabRows()
	var labels []string
	for _, r := range rows {
		labels = append(labels, r.Name)
	}
	joined := strings.Join(labels, "|")
	for _, want := range []string{"Changes 1", "Untracked 1", "a.go", "new.txt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the Git tab is missing %q in %q", want, joined)
		}
	}
	// The tab bar is one icon row; its two targets are published by the
	// renderer, not by a name on the row.
	if rows[0].Kind != sidebarRowProjectTabs {
		t.Errorf("first row is %v, want the one-row tab bar", rows[0].Kind)
	}

	// The untracked row is the last one; it opens the file, not the review.
	last := rows[len(rows)-1]
	if last.Kind != sidebarRowGitEntry {
		t.Fatalf("last row is %v, want a Git entry", last.Kind)
	}
	if cmd := m.ActivateGitEntry(last.Index); cmd == nil {
		t.Fatal("the untracked row opened nothing")
	}
}

func TestProjectTabsAreOneRowAndCarryTheCursor(t *testing.T) {
	m, dir := previewOS(t)
	m.filesView = fileViewState{Show: 1, Dir: dir, Want: dir}
	rows := m.projectTabRows()
	if len(rows) != 1 || rows[0].Kind != sidebarRowProjectTabs {
		t.Fatalf("the tab bar is not one row: %+v", rows)
	}
	if got := rows[0].Key; got != "tabs" {
		t.Fatalf("tab bar key = %q", got)
	}
}

var _ = overlay.Rect{}
