package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/getan/tuios-mini/internal/overlay"
)

// Git status inks. The values are VS Code's git decoration tokens, the same
// ones herdr-sidebar paints, so the rail, the tree and the diff agree. They are
// deliberately not the ANSI warning yellow: a modified file is a warm tan in a
// dark theme, not a caution.
//
// The light palette is not an inversion of the dark one. Each value is chosen
// to stay readable on white, as VS Code's Light+ set is.
var (
	gitDark = map[string]color.Color{
		"git_modified":  lipgloss.Color("#E2C08D"),
		"git_added":     lipgloss.Color("#81B88B"),
		"git_untracked": lipgloss.Color("#73C991"),
		"git_renamed":   lipgloss.Color("#73C991"),
		"git_deleted":   lipgloss.Color("#C74E39"),
		"git_conflict":  lipgloss.Color("#E4676B"),
		"git_ignored":   lipgloss.Color("#6B6B6B"),
	}
	gitLight = map[string]color.Color{
		"git_modified":  lipgloss.Color("#895503"),
		"git_added":     lipgloss.Color("#587C0C"),
		"git_untracked": lipgloss.Color("#007100"),
		"git_renamed":   lipgloss.Color("#007100"),
		"git_deleted":   lipgloss.Color("#AD0707"),
		"git_conflict":  lipgloss.Color("#B5200D"),
		"git_ignored":   lipgloss.Color("#8C8C8C"),
	}
)

// GitInk is the ink for one status token on bg. At 16 colours the terminal's
// own palette draws it; at every other depth the VS Code value is lifted to
// the mark floor against the ground it is actually drawn on, so a tan that is
// invisible on a very light profile does not become unreadable.
func GitInk(token string, bg color.Color) color.Color {
	if token == "" {
		return nil
	}
	if Depth() == overlay.Depth16 {
		return overlay.Slot(gitSlot(token))
	}
	pal := gitDark
	if overlay.ContrastRatio(tokWhite, bg) < overlay.ContrastRatio(tokBlack, bg) {
		pal = gitLight
	}
	c, ok := pal[token]
	if !ok {
		c = pal["git_modified"]
	}
	return ReadableAt(c, bg, MarkFloor)
}

func gitSlot(token string) int {
	switch token {
	case "git_added", "git_untracked", "git_renamed":
		return 10 // bright green
	case "git_deleted", "git_conflict":
		return 9 // bright red
	case "git_ignored":
		return 8 // bright black
	default:
		return 11 // bright yellow, for the modified tan
	}
}
