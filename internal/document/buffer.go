package document

import (
	"strings"
	"unicode/utf8"
)

// edit is one undoable step. The buffer is small (MaxBytes), so undo keeps
// whole snapshots rather than a delta log: a snapshot cannot desynchronise
// from the text it is meant to restore.
type edit struct {
	text           string
	cursor, anchor int
}

// Buffer is one editable text with a cursor at a byte offset and an optional
// selection. Anchor equals Cursor when nothing is selected.
type Buffer struct {
	text   string
	cursor int
	anchor int
	ver    uint64

	saved string
	undo  []edit
	redo  []edit
	// coalesce is set after a run of typed characters, so the next character
	// joins that step instead of making a step of its own.
	coalesce bool
}

// NewBuffer returns a buffer holding text with the caret at its end.
func NewBuffer(text string) *Buffer {
	return &Buffer{text: text, cursor: len(text), anchor: len(text), saved: text}
}

// Text is the buffer's current text.
func (b *Buffer) Text() string { return b.text }

// Version changes on every edit and load, so a renderer can cache the
// highlighted lines against the text it drew rather than re-tokenising a
// document on every frame.
func (b *Buffer) Version() uint64 { return b.ver }

// Dirty reports whether the text differs from the saved text.
func (b *Buffer) Dirty() bool { return b.text != b.saved }

// MarkSaved records the current text as the saved one.
func (b *Buffer) MarkSaved() { b.saved = b.text }

// MarkSavedText records the snapshot actually written by an asynchronous save.
// Newer edits, selection and undo history are left intact. Comparing text,
// rather than only version numbers, also recognises undo back to this snapshot.
func (b *Buffer) MarkSavedText(text string) {
	b.saved = text
	b.coalesce = false
}

// SetText replaces the buffer and clears history, for a fresh load.
func (b *Buffer) SetText(text string) {
	b.text, b.cursor, b.anchor, b.saved = text, len(text), len(text), text
	b.undo, b.redo, b.coalesce = nil, nil, false
	b.ver++
}

// Cursor is the caret's byte offset, always on a rune boundary.
func (b *Buffer) Cursor() int { return b.cursor }

// Selection returns the selected byte range, empty when nothing is selected.
func (b *Buffer) Selection() (int, int) {
	if b.anchor < b.cursor {
		return b.anchor, b.cursor
	}
	return b.cursor, b.anchor
}

// Select sets the selection and leaves the cursor at its end.
func (b *Buffer) Select(start, end int) {
	start = clampRune(b.text, start)
	end = clampRune(b.text, end)
	if start > end {
		start, end = end, start
	}
	b.anchor, b.cursor = start, end
}

// SelectAll selects the whole buffer.
func (b *Buffer) SelectAll() { b.anchor, b.cursor = 0, len(b.text) }

// SetCursor places the caret, dropping any selection.
func (b *Buffer) SetCursor(pos int) {
	b.cursor = clampRune(b.text, pos)
	b.anchor = b.cursor
}

// Collapsed reports whether the selection is empty.
func (b *Buffer) Collapsed() bool { return b.anchor == b.cursor }

// Insert replaces the selection with s.
func (b *Buffer) Insert(s string) {
	b.record(true)
	start, end := b.Selection()
	b.text = b.text[:start] + s + b.text[end:]
	b.cursor = start + len(s)
	b.anchor = b.cursor
	b.ver++
}

// InsertNewline inserts a line feed, joining the typing step.
func (b *Buffer) InsertNewline() {
	b.record(true)
	b.coalesce = false
	start, end := b.Selection()
	b.text = b.text[:start] + "\n" + b.text[end:]
	b.cursor = start + 1
	b.anchor = b.cursor
	b.ver++
}

// Backspace deletes the selection, or the rune before the caret.
func (b *Buffer) Backspace() {
	if !b.Collapsed() {
		b.record(false)
		start, end := b.Selection()
		b.text = b.text[:start] + b.text[end:]
		b.cursor, b.anchor = start, start
		b.ver++
		return
	}
	if b.cursor == 0 {
		return
	}
	b.record(false)
	_, size := utf8.DecodeLastRuneInString(b.text[:b.cursor])
	start := b.cursor - size
	b.text = b.text[:start] + b.text[b.cursor:]
	b.cursor, b.anchor = start, start
	b.ver++
}

// DeleteForward removes the selection, or the rune after the caret.
func (b *Buffer) DeleteForward() {
	if !b.Collapsed() {
		b.Backspace()
		return
	}
	if b.cursor >= len(b.text) {
		return
	}
	b.record(false)
	_, size := utf8.DecodeRuneInString(b.text[b.cursor:])
	b.text = b.text[:b.cursor] + b.text[b.cursor+size:]
	b.ver++
}

// SelectLeft extends the selection one rune left; SelectRight one right.
func (b *Buffer) SelectLeft() {
	b.anchor = b.anchorAtMove()
	b.cursor = prevRune(b.text, b.cursor)
}

// SelectRight extends the selection one rune right.
func (b *Buffer) SelectRight() {
	b.anchor = b.anchorAtMove()
	b.cursor = nextRune(b.text, b.cursor)
}

// anchorAtMove turns a selection extended by a previous move into a fresh
// anchor, so repeated shift-moves extend rather than restart.
func (b *Buffer) anchorAtMove() int {
	if b.anchor == b.cursor {
		return b.cursor
	}
	return b.anchor
}

// MoveLeft moves the caret one rune left, collapsing a selection to its start.
func (b *Buffer) MoveLeft(extend bool) { b.move(prevRune(b.text, b.cursor), extend) }

// MoveRight moves the caret one rune right.
func (b *Buffer) MoveRight(extend bool) { b.move(nextRune(b.text, b.cursor), extend) }

// MoveUp moves the caret one screen line up, keeping its column when it can.
// col is the caller's remembered column, returned updated.
func (b *Buffer) MoveUp(extend bool, col int) int {
	line, _ := lineOf(b.text, b.cursor)
	if line == 0 {
		b.move(0, extend)
		return col
	}
	prevStart, prevEnd := lineBounds(b.text, line-1)
	b.move(prevStart+min(col, prevEnd-prevStart), extend)
	return col
}

// MoveDown moves the caret one line down, keeping its column when it can.
func (b *Buffer) MoveDown(extend bool, col int) int {
	_, start := lineOf(b.text, b.cursor)
	end := lineEnd(b.text, start)
	if end >= len(b.text) {
		b.move(len(b.text), extend)
		return col
	}
	nextStart := end + 1
	nextEnd := lineEnd(b.text, nextStart)
	b.move(nextStart+min(col, nextEnd-nextStart), extend)
	return col
}

// Column is the caret's byte column within its line.
func (b *Buffer) Column() int {
	_, start := lineOf(b.text, b.cursor)
	return b.cursor - start
}

// MoveHome moves to the start of the line, MoveEnd to its end.
func (b *Buffer) MoveHome(extend bool) {
	_, start := lineOf(b.text, b.cursor)
	b.move(start, extend)
}

// MoveEnd moves to the end of the line.
func (b *Buffer) MoveEnd(extend bool) {
	_, start := lineOf(b.text, b.cursor)
	b.move(lineEnd(b.text, start), extend)
}

func (b *Buffer) move(to int, extend bool) {
	to = clampRune(b.text, to)
	if extend {
		if b.anchor == b.cursor {
			b.anchor = b.cursor
		}
	} else {
		b.anchor = to
	}
	b.cursor = to
	b.coalesce = false
}

// Undo restores the previous edit.
func (b *Buffer) Undo() {
	if len(b.undo) == 0 {
		return
	}
	b.redo = append(b.redo, b.snapshot())
	last := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	b.restore(last)
}

// Redo reapplies the last undone edit.
func (b *Buffer) Redo() {
	if len(b.redo) == 0 {
		return
	}
	b.undo = append(b.undo, b.snapshot())
	last := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	b.restore(last)
}

func (b *Buffer) snapshot() edit {
	return edit{text: b.text, cursor: b.cursor, anchor: b.anchor}
}

func (b *Buffer) restore(e edit) {
	b.text, b.cursor, b.anchor = e.text, e.cursor, e.anchor
	b.coalesce = false
	b.ver++
}

// record pushes the current state for undo. A run of typed characters shares
// one step: without it a typed word would cost one snapshot per character,
// and undo would step back letters rather than words.
func (b *Buffer) record(typing bool) {
	if typing && b.coalesce {
		b.redo = nil
		return
	}
	b.undo = append(b.undo, b.snapshot())
	if len(b.undo) > 200 {
		b.undo = b.undo[len(b.undo)-200:]
	}
	b.redo = nil
	b.coalesce = typing
}

// clampRune keeps an offset inside text and on a rune boundary.
func clampRune(text string, pos int) int {
	if pos <= 0 {
		return 0
	}
	if pos >= len(text) {
		return len(text)
	}
	for pos > 0 && !utf8.RuneStart(text[pos]) {
		pos--
	}
	return pos
}

func prevRune(text string, pos int) int {
	if pos <= 0 {
		return 0
	}
	_, size := utf8.DecodeLastRuneInString(text[:pos])
	return pos - size
}

func nextRune(text string, pos int) int {
	if pos >= len(text) {
		return len(text)
	}
	_, size := utf8.DecodeRuneInString(text[pos:])
	return pos + size
}

// lineOf returns the zero-based line index and the line's start offset.
func lineOf(text string, pos int) (line, start int) {
	pos = clampRune(text, pos)
	line = strings.Count(text[:pos], "\n")
	if i := strings.LastIndexByte(text[:pos], '\n'); i >= 0 {
		return line, i + 1
	}
	return 0, 0
}

// lineBounds returns the start and end of line n.
func lineBounds(text string, n int) (start, end int) {
	for i := 0; i < n; i++ {
		next := strings.IndexByte(text[start:], '\n')
		if next < 0 {
			return len(text), len(text)
		}
		start += next + 1
	}
	return start, lineEnd(text, start)
}

// lineEnd returns the offset of the line feed ending the line, or len(text).
func lineEnd(text string, start int) int {
	if i := strings.IndexByte(text[start:], '\n'); i >= 0 {
		return start + i
	}
	return len(text)
}

// LineCount is how many lines the text has, counting a trailing newline as the
// end of the last line rather than a new empty one.
func LineCount(text string) int {
	if text == "" {
		return 1
	}
	n := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}
