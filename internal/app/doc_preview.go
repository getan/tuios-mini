package app

import (
	"fmt"
	"image/color"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/getan/tuios-mini/internal/config"
	"github.com/getan/tuios-mini/internal/diffview"
	"github.com/getan/tuios-mini/internal/document"
	"github.com/getan/tuios-mini/internal/overlay"
	"github.com/getan/tuios-mini/internal/theme"
)

// The project preview: one file, drawn natively in the content region beside
// the rail.
//
// It is not a pane and never becomes one. Reading a file must not spawn a
// shell, a PTY or a numbered workspace, and must not change the terminal
// layout behind it; the pane layers keep running untouched under a full-bleed
// sheet. The document model (open/save rules, undo, cursor) is
// internal/document, which is also where the "never save bytes we did not
// read" invariant lives.
//
// One preview is reusable. Opening another file replaces it while it is clean;
// the first actual edit pins it, so a later click cannot discard work. Closing
// a dirty document asks first.

// docPreviewState is the open document, if any. Zero when closed.
type docPreviewState struct {
	open bool
	// focus says the sheet has the keyboard. A click on the rail beside it
	// takes the focus away without closing the document, so the rail's keys
	// work again; a click on the sheet brings it back.
	focus bool
	gen   uint64
	root  string
	rel   string
	doc   document.Doc
	buf   *document.Buffer
	edit  bool
	pin   bool
	err   string
	note  string
	// There is at most one writer. Another explicit save while it runs is
	// coalesced into one save of the latest text after this writer succeeds.
	saving, saveAgain bool
	saveSeq           uint64
	// conflict is set when a save found the file changed on disk. The buffer is
	// kept: the person decides whether to reload or copy their text out.
	conflict bool
	// confirm is set by esc on a dirty document; the next esc discards.
	confirm bool
	scroll  int
	// hlVer and hl cache the highlighted lines for one buffer version.
	hlVer uint64
	hl    [][]diffview.Span
	// hlDone says a pass ran at all. A freshly opened buffer starts at
	// Version 0, which compares equal to the zero hlVer, so the first frame
	// skipped highlighting until the first edit. hlDone makes the gate about
	// "has this text been tokenised", not "is the number different".
	hlDone bool
	// body is where the document rows were last drawn, in screen coordinates,
	// for clicks and wheels.
	body overlay.Rect
	// gutter is the line-number column width of the last frame.
	gutter int
}

// docOpenMsg is a finished read.
type docOpenMsg struct {
	Gen  uint64
	Root string
	Rel  string
	Doc  document.Doc
	Err  error
	Edit bool
}

// docSaveMsg is a finished save.
type docSaveMsg struct {
	Gen, Seq uint64
	Doc      document.Doc
	Err      error
}

// DocPreviewOpen reports whether the preview owns the content region.
func (m *OS) DocPreviewOpen() bool { return m.docPreview.open }

// DocPreviewFocused reports whether the sheet has the keyboard.
func (m *OS) DocPreviewFocused() bool {
	return m.docPreview.open && m.docPreview.focus
}

// BlurDocPreview gives the keyboard back to the rail or dock without closing
// the sheet.
func (m *OS) BlurDocPreview() { m.docPreview.focus = false }

// DocPreviewEditing reports whether the document takes text.
func (m *OS) DocPreviewEditing() bool {
	return m.docPreview.open && m.docPreview.edit && m.docPreview.doc.Editable
}

// DocPreviewPath is the open document's repo-relative path.
func (m *OS) DocPreviewPath() string { return m.docPreview.rel }

// OpenDocPreview shows rel under root. edit asks for the caret; a read-only
// document ignores it and says why. A pinned (edited) document is not replaced
// by another click: that is the whole point of pinning.
func (m *OS) OpenDocPreview(root, rel string, edit bool) tea.Cmd {
	if m.docPreview.saving {
		m.docPreview.note = "saving... wait before opening another file"
		return nil
	}
	if m.docPreview.pin && m.docPreview.buf != nil && m.docPreview.buf.Dirty() {
		m.ShowNotification("Save or close the edited file first.", "warning", m.Settings.NotificationDuration)
		return nil
	}
	if m.filesView.Host != "" || m.AttachedHost != "" {
		m.ShowNotification("That file is on another machine. Attach there to read it.", "info", m.Settings.NotificationDuration)
		return nil
	}
	if root == "" || rel == "" {
		return nil
	}
	gen := m.docPreview.gen + 1
	m.docPreview = docPreviewState{
		gen: gen, open: true, focus: true, root: root,
		rel: filepath.ToSlash(rel), edit: edit, note: "reading...",
	}
	return func() tea.Msg {
		doc, err := document.Open(root, rel)
		return docOpenMsg{Gen: gen, Root: root, Rel: rel, Doc: doc, Err: err, Edit: edit}
	}
}

// HandleDocOpen installs a finished read unless a newer one has replaced it.
func (m *OS) HandleDocOpen(msg docOpenMsg) {
	if !m.docPreview.open || msg.Gen != m.docPreview.gen {
		return
	}
	if msg.Err != nil {
		m.docPreview.err = msg.Err.Error()
		m.docPreview.note = ""
		return
	}
	m.docPreview.doc = msg.Doc
	m.docPreview.buf = document.NewBuffer(msg.Doc.Text)
	if !msg.Doc.Editable {
		m.docPreview.edit = false
	}
	m.docPreview.note = ""
	if msg.Doc.Reason != "" {
		m.docPreview.note = msg.Doc.Reason + " · read only"
	}
}

// CloseDocPreview closes the sheet. A dirty buffer asks once; force is the
// answer to that question.
func (m *OS) CloseDocPreview(force bool) {
	d := &m.docPreview
	if !d.open {
		return
	}
	if d.saving {
		d.note = "saving... wait before closing"
		return
	}
	if !force && d.buf != nil && d.buf.Dirty() {
		d.confirm = true
		d.note = "unsaved changes · esc again discards, ctrl+s saves"
		return
	}
	*d = docPreviewState{gen: d.gen}
}

// DocPreviewSave writes the buffer, reporting a conflict rather than
// overwriting a file that moved since it was read.
func (m *OS) DocPreviewSave() tea.Cmd {
	d := &m.docPreview
	if !d.open || d.buf == nil || !d.doc.Editable {
		return nil
	}
	if d.saving {
		d.saveAgain = true
		return nil
	}
	d.saveSeq++
	d.saving = true
	doc, text, gen, seq := d.doc, d.buf.Text(), d.gen, d.saveSeq
	d.note = "saving..."
	return func() tea.Msg {
		updated, err := document.Save(doc, text)
		return docSaveMsg{Gen: gen, Seq: seq, Doc: updated, Err: err}
	}
}

// HandleDocSave acknowledges only the written text, then starts a queued save
// with the updated disk snapshot. Old/duplicate replies cannot clear a writer.
func (m *OS) HandleDocSave(msg docSaveMsg) tea.Cmd {
	d := &m.docPreview
	if !d.open || msg.Gen != d.gen || !d.saving || msg.Seq != d.saveSeq {
		return nil
	}
	again := d.saveAgain
	d.saving, d.saveAgain = false, false
	if msg.Err != nil {
		if msg.Err == document.ErrConflict {
			d.conflict = true
			d.note = "conflict: the file changed on disk · ctrl+r reloads, ctrl+s retries"
			return nil
		}
		d.note = "save failed: " + msg.Err.Error()
		return nil
	}
	d.doc = msg.Doc
	d.buf.MarkSavedText(msg.Doc.Text)
	d.conflict = false
	if d.buf.Dirty() {
		if again {
			return m.DocPreviewSave()
		}
		d.note = "saved earlier version / newer edits are unsaved"
		return nil
	}
	d.note = "saved"
	m.ShowNotification("Saved "+d.rel, "success", m.Settings.NotificationDuration)
	return nil
}

// DocPreviewReload discards the buffer and reads the file again.
func (m *OS) DocPreviewReload() tea.Cmd {
	d := &m.docPreview
	if !d.open || d.root == "" {
		return nil
	}
	if d.saving {
		d.note = "saving... wait before reloading"
		return nil
	}
	// An explicit reload discards the local text; the guard in OpenDocPreview
	// is for an implicit click on another file, not for this one.
	d.buf, d.pin = nil, false
	return m.OpenDocPreview(d.root, d.rel, d.edit)
}

// DocPreviewPaste inserts clipboard text at the caret. Line breaks are kept;
// this is an editor, not a shell prompt.
func (m *OS) DocPreviewPaste(text string) bool {
	d := &m.docPreview
	if !d.open || d.buf == nil || !d.doc.Editable {
		return false
	}
	d.edit = true
	d.pin = true
	d.buf.Insert(strings.ReplaceAll(text, "\r\n", "\n"))
	m.docPreviewScrollToCursor()
	return true
}

// DocPreviewKey owns every key while the sheet is open. It returns handled
// false only for a key it does not know, which the caller then drops rather
// than letting it reach the pane underneath.
func (m *OS) DocPreviewKey(msg tea.KeyPressMsg) (*OS, tea.Cmd) {
	d := &m.docPreview
	if !d.open {
		return m, nil
	}
	key := msg.String()
	if key != "esc" {
		d.confirm = false
	}
	// The ctrl chords first: a document may contain the letters, so the bare
	// letter must never be read as a command while the caret is live.
	switch key {
	case "esc":
		if d.confirm || d.buf == nil || !d.buf.Dirty() {
			m.CloseDocPreview(true)
		} else {
			m.CloseDocPreview(false)
		}
		return m, nil
	case "ctrl+s":
		return m, m.DocPreviewSave()
	case "ctrl+r":
		return m, m.DocPreviewReload()
	case "ctrl+z":
		if d.buf != nil {
			d.buf.Undo()
		}
		return m, nil
	case "ctrl+shift+z", "ctrl+y":
		if d.buf != nil {
			d.buf.Redo()
		}
		return m, nil
	case "pgup", "ctrl+u":
		d.scroll = max(d.scroll-m.previewBodyHeight()/2, 0)
		return m, nil
	case "pgdown", "ctrl+d":
		d.scroll += max(m.previewBodyHeight()/2, 1)
		return m, nil
	}
	if d.buf == nil {
		return m, nil
	}
	extend := strings.Contains(key, "shift+")
	switch key {
	case "up", "shift+up":
		d.buf.MoveUp(extend, d.buf.Column())
		m.docPreviewScrollToCursor()
		return m, nil
	case "down", "shift+down":
		d.buf.MoveDown(extend, d.buf.Column())
		m.docPreviewScrollToCursor()
		return m, nil
	case "left", "shift+left":
		if extend {
			d.buf.SelectLeft()
		} else {
			d.buf.MoveLeft(false)
		}
		m.docPreviewScrollToCursor()
		return m, nil
	case "right", "shift+right":
		if extend {
			d.buf.SelectRight()
		} else {
			d.buf.MoveRight(false)
		}
		m.docPreviewScrollToCursor()
		return m, nil
	case "home":
		d.buf.MoveHome(extend)
		return m, nil
	case "end":
		d.buf.MoveEnd(extend)
		return m, nil
	}
	if !d.doc.Editable {
		return m, nil
	}
	d.edit = true
	d.pin = true
	switch key {
	case "enter":
		d.buf.InsertNewline()
	case "backspace":
		d.buf.Backspace()
	case "delete":
		d.buf.DeleteForward()
	case "tab":
		d.buf.Insert("\t")
	default:
		if msg.Text != "" {
			d.buf.Insert(msg.Text)
		}
	}
	m.docPreviewScrollToCursor()
	return m, nil
}

// docPreviewScrollToCursor keeps the caret row inside the body.
func (m *OS) docPreviewScrollToCursor() {
	d := &m.docPreview
	height := m.previewBodyHeight()
	if d.buf == nil || height <= 0 {
		return
	}
	line, _ := m.docCursorLine()
	if line < d.scroll {
		d.scroll = line
	}
	if line >= d.scroll+height {
		d.scroll = line - height + 1
	}
}

func (m *OS) docCursorLine() (int, int) {
	d := &m.docPreview
	before := d.buf.Text()[:d.buf.Cursor()]
	line := strings.Count(before, "\n")
	start := strings.LastIndexByte(before, '\n') + 1
	return line, d.buf.Cursor() - start
}

func (m *OS) previewBodyHeight() int {
	h := m.GetUsableHeight() - 3 // header, rule, footer
	return max(h, 1)
}

// DocPreviewWheel scrolls the document under the pointer.
func (m *OS) DocPreviewWheel(delta int) {
	d := &m.docPreview
	lines := document.LineCount(d.doc.Text)
	if d.buf != nil {
		lines = len(strings.Split(d.buf.Text(), "\n"))
	}
	d.scroll = min(max(d.scroll+delta, 0), max(lines-1, 0))
}

// DocPreviewContains reports whether a screen cell is on the sheet, so a click
// on the rail beside it still reaches the rail.
func (m *OS) DocPreviewContains(x, y int) bool {
	if !m.docPreview.open {
		return false
	}
	return x >= m.GetLeftMargin() && x < m.GetLeftMargin()+m.GetContentWidth() &&
		y >= m.GetTopMargin() && y < m.GetTopMargin()+m.GetUsableHeight()
}

// DocPreviewClick places the caret on the clicked row and column.
func (m *OS) DocPreviewClick(x, y int) {
	d := &m.docPreview
	if d.buf == nil || !m.DocPreviewContains(x, y) {
		return
	}
	d.focus = true
	if !d.body.Contains(x, y) {
		return
	}
	line := d.scroll + (y - d.body.Y0)
	lines := strings.Split(d.buf.Text(), "\n")
	if line < 0 || line >= len(lines) {
		return
	}
	col := max(x-d.body.X0-d.gutter, 0)
	off := 0
	for i := 0; i < line; i++ {
		off += len(lines[i]) + 1
	}
	pos := docByteAtColumn(lines[line], col)
	d.buf.SetCursor(off + pos)
	d.edit = d.doc.Editable
}

// docByteAtColumn maps a display column to a byte offset in line, expanding
// tabs the way the renderer does.
func docByteAtColumn(line string, col int) int {
	cells := 0
	for i, r := range line {
		var w int
		if r == '\t' {
			w = 4 - cells%4
		} else {
			w = ansi.StringWidth(string(r))
		}
		if cells+w > col {
			return i
		}
		cells += w
	}
	return len(line)
}

// renderDocPreview draws the sheet. It records the body rectangle and the
// document rows so a click can name a line without redoing the layout.
func (m *OS) renderDocPreview() string {
	d := &m.docPreview
	if !d.open {
		return ""
	}
	width, height := m.GetContentWidth(), m.GetUsableHeight()
	if width < 8 || height < 5 {
		return ""
	}
	pal := theme.UI()
	dim := overlay.Style(pal.Surface).Foreground(pal.FgDim)
	accent := overlay.Style(pal.Surface).Foreground(pal.AccentBright)

	header := "Preview  "
	if d.conflict {
		header += "conflict  "
	} else if d.buf != nil && d.buf.Dirty() {
		header += "*  "
	}
	header += d.rel
	if !d.doc.Editable && d.doc.Reason != "" {
		header += "  / read only: " + d.doc.Reason
	}
	lines := []string{
		overlay.Fill(accent.Render(" "+overlay.Truncate(header, width-2)), width, pal.Surface),
		overlay.Rule(width, pal.Surface, pal),
	}
	body := m.renderDocBody(width, height-3, pal)
	lines = append(lines, body...)
	footer := " arrows move / type to edit / ctrl+s save / ctrl+z undo / esc close"
	if d.note != "" {
		footer = " " + d.note
	}
	lines = append(lines, overlay.Fill(dim.Render(overlay.Truncate(footer, width)), width, pal.Surface))
	return strings.Join(lines, "\n")
}

func (m *OS) renderDocBody(width, height int, pal overlay.Palette) []string {
	d := &m.docPreview
	out := make([]string, 0, height)
	if d.err != "" {
		return append(out, overlay.Fill(overlay.Style(pal.Surface).Foreground(pal.Warn).
			Render(" "+d.err), width, pal.Surface))
	}
	if d.buf == nil {
		return append(out, overlay.Fill(overlay.Style(pal.Surface).Foreground(pal.FgMute).
			Render(" "+d.note), width, pal.Surface))
	}
	text := d.buf.Text()
	all := strings.Split(text, "\n")
	numW := len(strconv.Itoa(len(all))) + 2
	d.gutter = numW
	if !d.hlDone || d.hlVer != d.buf.Version() {
		d.hl = diffview.Highlight(d.rel, all)
		d.hlVer = d.buf.Version()
		d.hlDone = true
	}
	cursorLine, cursorCol := m.docCursorLine()
	if d.scroll > max(len(all)-1, 0) {
		d.scroll = max(len(all)-1, 0)
	}
	// Screen coordinates: the sheet is placed at the content region's origin,
	// two rows down for the header and rule.
	d.body = overlay.Rect{
		X0: m.GetLeftMargin(), Y0: m.GetTopMargin() + 2,
		X1: m.GetLeftMargin() + width, Y1: m.GetTopMargin() + 2 + height,
	}
	starts := make([]int, len(all))
	off := 0
	for i, l := range all {
		starts[i] = off
		off += len(l) + 1
	}
	for i := d.scroll; i < len(all) && len(out) < height; i++ {
		// A file is data. A control byte in it must not reach the terminal as a
		// control sequence, so every one but a tab is shown as a substitute
		// character. Replacing bytes one for one keeps every offset the caret
		// and the selection use intact.
		all[i] = docSafeLine(all[i])
		var spans []diffview.Span
		if i < len(d.hl) {
			spans = d.hl[i]
		}
		sel := m.docSelectionOn(starts[i], all[i])
		cur := -1
		if i == cursorLine {
			cur = cursorCol
		}
		code := renderDocLine(pal, all[i], spans, sel, cur)
		gutter := overlay.Style(pal.Surface).Foreground(pal.FgMute).
			Render(fmt.Sprintf("%*d ", numW-1, i+1))
		line := overlay.Fill(gutter+code, width, pal.Surface)
		out = append(out, line)
	}
	for len(out) < height {
		out = append(out, overlay.Fill("", width, pal.Surface))
	}
	return out
}

// docSafeLine blanks the control bytes of one line except tab, in place.
func docSafeLine(s string) string {
	clean := true
	for i := 0; i < len(s); i++ {
		if (s[i] < 0x20 && s[i] != '\t') || s[i] == 0x7f {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	b := []byte(s)
	for i := range b {
		if (b[i] < 0x20 && b[i] != '\t') || b[i] == 0x7f {
			b[i] = '?'
		}
	}
	return string(b)
}

// docSelectionOn returns the selected byte range within line i, relative to
// its start, or nil.
func (m *OS) docSelectionOn(start int, text string) []int {
	d := &m.docPreview
	if d.buf == nil || d.buf.Collapsed() {
		return nil
	}
	selStart, selEnd := d.buf.Selection()
	end := start + len(text)
	if selEnd <= start || selStart > end {
		return nil
	}
	return []int{max(selStart-start, 0), min(selEnd-start, end-start)}
}

// renderDocLine draws one line by cutting it at every boundary that matters —
// syntax spans, the selection and the caret — and styling each run once. The
// caret is a reversed cell on its character, so it is visible without moving
// the code around it.
func renderDocLine(pal overlay.Palette, text string, spans []diffview.Span, sel []int, cursor int) string {
	cursor = min(max(cursor, -1), len(text))
	cursorEnd := cursor
	if cursor >= 0 && cursor < len(text) {
		_, size := utf8.DecodeRuneInString(text[cursor:])
		cursorEnd = cursor + size
	}
	if cursor == len(text) {
		cursorEnd = cursor
	}
	bounds := []int{0, len(text)}
	for _, s := range spans {
		bounds = append(bounds, max(min(s.Start, len(text)), 0), max(min(s.End, len(text)), 0))
	}
	if len(sel) == 2 {
		bounds = append(bounds, max(min(sel[0], len(text)), 0), max(min(sel[1], len(text)), 0))
	}
	if cursor >= 0 {
		bounds = append(bounds, cursor, cursorEnd)
	}
	sort.Ints(bounds)
	var b strings.Builder
	col := 0
	for i := 0; i+1 < len(bounds); i++ {
		start, end := bounds[i], bounds[i+1]
		if end <= start {
			continue
		}
		class := diffview.Plain
		for _, s := range spans {
			if s.Start <= start && end <= s.End {
				class = s.Class
				break
			}
		}
		style := overlay.Style(pal.Surface).Foreground(docClassInk(pal, class))
		if len(sel) == 2 && start >= sel[0] && end <= sel[1] {
			style = style.Background(pal.Selected)
		}
		if cursor >= 0 && start >= cursor && end <= cursorEnd {
			style = style.Reverse(true)
		}
		var shown strings.Builder
		for _, r := range text[start:end] {
			if r == '\t' {
				n := 4 - col%4
				shown.WriteString(strings.Repeat(" ", n))
				col += n
				continue
			}
			shown.WriteRune(r)
			col += ansi.StringWidth(string(r))
		}
		b.WriteString(style.Render(shown.String()))
	}
	if cursor == len(text) && len(text) <= cursor {
		b.WriteString(overlay.Style(pal.Surface).Reverse(true).Render(" "))
	}
	return b.String()
}

func docClassInk(pal overlay.Palette, class diffview.Class) color.Color {
	switch class {
	case diffview.Keyword, diffview.Preproc, diffview.Heading:
		return pal.AccentBright
	case diffview.Type, diffview.Builtin, diffview.Attr:
		return pal.Info
	case diffview.Func, diffview.Tag:
		return pal.Accent
	case diffview.String:
		return pal.Success
	case diffview.Number:
		return pal.Warning
	case diffview.Comment, diffview.Meta:
		return pal.FgMute
	case diffview.Operator, diffview.Punct:
		return pal.FgDim
	}
	return pal.Fg
}

// docPreviewLayer places the sheet over the content region, under the rail and
// every panel. The pane layers are still composed beneath it.
func (m *OS) docPreviewLayer() *lipgloss.Layer {
	content := m.renderDocPreview()
	if content == "" {
		return nil
	}
	return lipgloss.NewLayer(content).
		X(m.GetLeftMargin()).Y(m.GetTopMargin()).
		Z(config.ZIndexDock - 1).ID("project-preview")
}
