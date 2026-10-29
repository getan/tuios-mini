package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/getan/tuios-mini/internal/config"
)

// TestHiddenWindowButtonsKeepTitleOppositeControls is the reduced-SSH chrome:
// hiding the controls must not move the pane name to the other end of the bar.
// With the default left-hand controls the name stays on the right, exactly as
// it does when the controls are visible.
func TestHiddenWindowButtonsKeepTitleOppositeControls(t *testing.T) {
	const badge = " name "
	for _, tc := range []struct {
		name     string
		position string
		right    bool
	}{
		{"default left controls", config.WindowButtonPositionLeft, true},
		{"right controls", config.WindowButtonPositionRight, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := config.DefaultSettings()
			s.WindowTitleAlign = "auto"
			s.WindowButtonPosition = tc.position
			row := layoutBorderRow(badge, "", 24, lipgloss.Color("62"), true, &s)
			if row.pillStart != -1 {
				t.Fatalf("a buttonless row reported a pill at %d", row.pillStart)
			}
			text := ansi.Strip(row.text)
			left := strings.HasPrefix(text, s.GetWindowBorderTopLeft()+badge)
			right := strings.HasSuffix(text, badge+s.GetWindowBorderTopRight())
			if tc.right && !right {
				t.Fatalf("title is not on the right with hidden left controls: %q", text)
			}
			if !tc.right && !left {
				t.Fatalf("title is not on the left with hidden right controls: %q", text)
			}
		})
	}
}

// TestExplicitTitleAlignWinsWithoutButtons keeps the config authoritative:
// auto follows the controls, but an explicit alignment still places the name
// where the user asked.
func TestExplicitTitleAlignWinsWithoutButtons(t *testing.T) {
	const badge = " name "
	s := config.DefaultSettings()
	s.WindowButtonPosition = config.WindowButtonPositionLeft
	for _, tc := range []struct {
		align string
		right bool
	}{
		{"left", false},
		{"right", true},
	} {
		s.WindowTitleAlign = tc.align
		row := layoutBorderRow(badge, "", 24, lipgloss.Color("62"), true, &s)
		text := ansi.Strip(row.text)
		at := strings.Index(text, badge)
		if at < 0 {
			t.Fatalf("the title is missing: %q", text)
		}
		if tc.right && at < len(text)/2 {
			t.Errorf("right alignment did not win: %q", text)
		}
		if !tc.right && at > len(text)/2 {
			t.Errorf("left alignment did not win: %q", text)
		}
	}
}
