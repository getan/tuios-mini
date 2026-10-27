package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/getan/tuios-mini/internal/overlay"
)

// gitTabKeyHintLines is the pinned legend while the Git view owns the rail's
// keyboard cursor. Read the live registry, not literal default keys: rebinding
// or unbinding an action changes its hint too. Nothing is advertised from the
// terminal/agent rows, where the same keys have different meanings.
func (m *OS) gitTabKeyHintLines(width int, pal overlay.Palette) []string {
	if !m.SidebarFocused || !m.GitTabActive() || m.KeybindRegistry == nil || width < 8 {
		return nil
	}
	row, ok := m.sidebarCursorRow()
	if !ok {
		return nil
	}
	switch row.Kind {
	case sidebarRowGitEntry, sidebarRowProjectGit, sidebarRowProjectTabs, sidebarRowFileCd:
	default:
		return nil
	}
	type hint struct{ action, label string }
	hints := []hint{
		{"activate", "diff"}, {"file_stage", "stage"}, {"file_unstage", "unstage"},
		{"file_commit", "commit"}, {"file_stage_all", "stage all"}, {"file_unstage_all", "unstage all"},
		{"file_sync", "sync"}, {"git_branches", "branches"}, {"git_commits", "commits"},
		{"git_graph", "graph"}, {"git_tags", "tags"}, {"git_stashes", "stashes"},
		{"git_remotes", "remotes"}, {"git_file_history", "file log"}, {"git_repos", "repos"},
	}
	avail := width - 2
	var lines []string
	var parts []string
	used := 0
	flush := func() {
		if len(parts) > 0 {
			lines = append(lines, sidebarFit(" "+strings.Join(parts, "  "), width, nil))
			parts, used = nil, 0
		}
	}
	for _, h := range hints {
		if row.Kind != sidebarRowGitEntry {
			switch h.action {
			case "activate", "file_stage", "file_unstage", "file_commit", "git_file_history":
				continue // these need a changed-path row, not the tab token
			}
		}
		keys := m.KeybindRegistry.GetSidebarFilesKeys(h.action)
		if h.action == "activate" {
			keys = m.KeybindRegistry.GetSidebarKeys(h.action)
		}
		if len(keys) == 0 {
			continue
		}
		text := overlay.Truncate(keys[0]+" "+h.label, avail)
		n := lipgloss.Width(text)
		if used > 0 && used+2+n > avail {
			flush()
		}
		if used > 0 {
			used += 2
		}
		parts = append(parts, sidebarStyle(nil, pal.FgMute).Render(text))
		used += n
	}
	flush()
	return lines
}
