package input

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/getan/tuios-mini/internal/app"
)

// The rail's file rows open the native sheet through the real click and key
// handlers, with the real renderer publishing the row rectangles. This is the
// integration these handlers exist for: a click, a document, an edit, a save.

func TestAFileClickOpensTheNativeSheetAndCtrlSSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.txt")
	if err := os.WriteFile(path, []byte("hello from the sheet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := filesRailOS(t, dir, "")
	before := len(o.Windows)
	_ = o.View()

	x, y := fileRowCell(t, o, "brief.txt")
	o, cmd := handleMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, o)
	if cmd == nil {
		t.Fatal("the click asked for no preview")
	}
	if model, _ := o.Update(cmd()); model != nil {
		if m, ok := model.(*app.OS); ok {
			o = m
		}
	}
	_ = o.View()

	frame := ansi.Strip(o.GetCanvas(true).Render())
	for _, want := range []string{"Preview", "brief.txt", "hello from the sheet"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the sheet is missing %q:\n%s", want, frame)
		}
	}
	if len(o.Windows) != before {
		t.Fatalf("reading a file changed the panes: %d -> %d", before, len(o.Windows))
	}

	// The sheet has the keyboard: typing edits it, not the shell.
	o, _ = HandleKeyPress(tea.KeyPressMsg{Code: 'X', Text: "X"}, o)
	o, cmd = HandleKeyPress(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, o)
	if cmd == nil {
		t.Fatal("ctrl+s asked for no save")
	}
	if model, _ := o.Update(cmd()); model != nil {
		if m, ok := model.(*app.OS); ok {
			o = m
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "X") {
		t.Fatalf("the save did not write the edit: %q", body)
	}
	if !strings.Contains(ansi.Strip(o.GetCanvas(true).Render()), "Saved brief.txt") {
		t.Fatal("the dock did not say the file was saved")
	}

	// Esc closes; the layout under the sheet is the one that was there.
	o, _ = HandleKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape}, o)
	if o.DocPreviewOpen() {
		t.Fatal("esc left the sheet open on a saved document")
	}
	if len(o.Windows) != before {
		t.Fatalf("closing the sheet changed the panes: %d -> %d", before, len(o.Windows))
	}
}

// TestARightClickOnTheSheetMissesIt keeps the rail's own menus reachable while
// the sheet is up: the sheet covers the content region, not the rail.
func TestARightClickOnTheSheetMissesIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "brief.txt"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := filesRailOS(t, dir, "")
	_ = o.View()
	x, y := fileRowCell(t, o, "brief.txt")
	o, cmd := handleMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, o)
	if cmd == nil {
		t.Fatal("the click asked for no preview")
	}
	if model, _ := o.Update(cmd()); model != nil {
		if m, ok := model.(*app.OS); ok {
			o = m
		}
	}
	_ = o.View()
	if !o.DocPreviewOpen() {
		t.Fatal("the click did not open the sheet")
	}

	// The row is still on the rail and still has a menu.
	x, y = fileRowCell(t, o, "brief.txt")
	o, _ = handleMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight}, o)
	if !o.ContextMenuActive() {
		t.Fatalf("a right press on the rail hit the sheet instead:\n%s",
			ansi.Strip(o.GetCanvas(true).Render()))
	}
}
