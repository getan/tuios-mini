package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/getan/tuios-mini/internal/fuzz"
)

// Scheduled Fuzz #5's minimized replay. Keep the narrow, crowded layout: the
// full-size seeds do not distinguish pane occlusion from separator overpaint.
func TestCICrowdedPaneReplay(t *testing.T) {
	target, err := newFuzzTarget(fuzzScratch(t))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := target.Reset(); err != nil {
		t.Fatal(err)
	}
	actions, err := fuzz.ParseScript("new-pane\nnew-pane\nnew-pane\nresize 60 20\nnew-pane\nclose-pane 0\nnew-pane\nfocus 5\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if err := target.Apply(a); err != nil {
			t.Fatal(err)
		}
	}
	f := target.(*fuzzOS)
	for i, w := range visibleFuzzPanes(f.m) {
		box := f.m.renderWindowBox(w, i, false, lipgloss.Color("#888888"))
		if rows := strings.Count(box, "\n") + 1; rows > w.Height {
			t.Errorf("%s box has %d rows in a %d-row rectangle", w.ID, rows, w.Height)
		}
	}
	if bad := checkGuestCellsAreNotPaintedOver(f); len(bad) != 0 {
		t.Fatal(bad)
	}
}
