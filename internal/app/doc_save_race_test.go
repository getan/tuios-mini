package app

import (
	"os"
	"testing"
)

// The command is held while the buffer is edited: a deterministic version of
// a slow disk. Failure modes: the reply marks newer text clean, undo compares
// against the pre-save text, two saves race using the same disk snapshot, a
// repeated reply clears a later save, or close/reload races an outstanding
// writer. These are save-ordering regressions, not editor-behaviour tests.
func TestDocumentSaveAcknowledgesOnlyTheWrittenSnapshot(t *testing.T) {
	m, dir := previewOS(t)
	path := writeFile(t, dir, "a.txt", "base")
	openPreview(t, m, dir, "a.txt", true)
	m.docPreview.buf.Insert("-first")
	first := m.DocPreviewSave()
	m.docPreview.buf.Insert("-later")
	_ = m.HandleDocSave(first().(docSaveMsg))
	if !m.docPreview.buf.Dirty() {
		t.Fatal("a save reply marked edits made during the save clean")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "base-first" {
		t.Fatalf("disk = %q, err = %v", data, err)
	}
	// The clean checkpoint must now be the text actually written, not base.
	m.docPreview.buf.Select(len("base-first"), len(m.docPreview.buf.Text()))
	m.docPreview.buf.Backspace()
	if m.docPreview.buf.Dirty() {
		t.Fatal("returning to the written snapshot is still dirty")
	}
	m.docPreview.buf.Undo()
	if !m.docPreview.buf.Dirty() {
		t.Fatal("undo to the newer, unwritten edit is clean")
	}
}

func TestDocumentRepeatedSaveSerializesTheLatestSnapshot(t *testing.T) {
	m, dir := previewOS(t)
	path := writeFile(t, dir, "a.txt", "base")
	openPreview(t, m, dir, "a.txt", true)
	m.docPreview.buf.Insert("-first")
	first := m.DocPreviewSave()
	m.docPreview.buf.Insert("-latest")
	if cmd := m.DocPreviewSave(); cmd != nil {
		t.Fatal("a second writer started before the first completed")
	}
	if cmd := m.DocPreviewSave(); cmd != nil {
		t.Fatal("a third writer started instead of coalescing the request")
	}
	reply := first().(docSaveMsg)
	next := m.HandleDocSave(reply)
	if next == nil {
		t.Fatal("the queued explicit save was lost")
	}
	if duplicate := m.HandleDocSave(reply); duplicate != nil {
		t.Fatal("a duplicated reply started a third write")
	}
	_ = m.HandleDocSave(next().(docSaveMsg))
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "base-first-latest" || m.docPreview.buf.Dirty() || m.docPreview.conflict {
		t.Fatalf("serialized save: disk=%q err=%v dirty=%v conflict=%v", data, err, m.docPreview.buf.Dirty(), m.docPreview.conflict)
	}
}

func TestDocumentSaveFailureLeavesTheBufferAndClearsTheQueue(t *testing.T) {
	m, dir := previewOS(t)
	writeFile(t, dir, "a.txt", "base")
	openPreview(t, m, dir, "a.txt", true)
	m.docPreview.buf.Insert("-local")
	first := m.DocPreviewSave()
	_ = m.DocPreviewSave()
	writeFile(t, dir, "a.txt", "external")
	if next := m.HandleDocSave(first().(docSaveMsg)); next != nil {
		t.Fatal("a queued save retried after a disk conflict")
	}
	if !m.docPreview.buf.Dirty() || !m.docPreview.conflict {
		t.Fatal("failure discarded the local edits or hid the conflict")
	}
	if retry := m.DocPreviewSave(); retry == nil {
		t.Fatal("the failed writer left saving stuck forever")
	}
}

func TestDocumentCannotBeReplacedWhileItsSaveIsRunning(t *testing.T) {
	m, dir := previewOS(t)
	writeFile(t, dir, "a.txt", "base")
	writeFile(t, dir, "b.txt", "other")
	openPreview(t, m, dir, "a.txt", true)
	m.docPreview.buf.Insert("-edit")
	first := m.DocPreviewSave()
	// Even an explicit discard cannot cancel an already-running disk write.
	m.CloseDocPreview(true)
	if !m.DocPreviewOpen() {
		t.Fatal("close abandoned a running writer")
	}
	if m.DocPreviewReload() != nil || m.OpenDocPreview(dir, "b.txt", false) != nil {
		t.Fatal("reload/replacement raced a running writer")
	}
	_ = m.HandleDocSave(first().(docSaveMsg))
	m.CloseDocPreview(true)
	if m.DocPreviewOpen() {
		t.Fatal("a completed save prevents closing")
	}
}
