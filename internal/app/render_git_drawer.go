package app

import (
	"strings"

	"github.com/getan/tuios-mini/internal/overlay"
	"github.com/getan/tuios-mini/internal/theme"
)

// renderGitDrawer draws the history panel: a window of the rows around the
// selection, with the selected row marked. A repository row shows its path;
// its window id is the second tab-separated field and is not drawn.
func (m *OS) renderGitDrawer() (string, overlay.Geometry, []overlayRowHit) {
	pal := theme.UI()
	bg := pal.Canvas
	width := overlay.DialogFitWidth(76, m.GetRenderWidth())
	d := &m.gitDrawer
	visible := min(gitDrawerVisibleRows(), max(m.GetRenderHeight()-10, 4))
	if d.selected < d.scroll {
		d.scroll = d.selected
	}
	if d.selected >= d.scroll+visible {
		d.scroll = d.selected - visible + 1
	}
	body := make([]string, 0, visible+2)
	switch {
	case d.loading:
		body = append(body, dialogBodyLine("reading…", width, pal.FgMute, bg))
	case d.err != "":
		body = append(body, dialogBodyLine(d.err, width, pal.Warn, bg))
	case len(d.rows) == 0:
		body = append(body, dialogBodyLine("Nothing here.", width, pal.FgMute, bg))
	default:
		end := min(d.scroll+visible, len(d.rows))
		for i := d.scroll; i < end; i++ {
			line := d.rows[i]
			if d.kind == "repos" || strings.Contains(line, "\t") {
				line = strings.SplitN(line, "\t", 2)[0]
			}
			marker := "  "
			ink := pal.Fg
			if i == d.selected {
				marker = "› "
				ink = pal.AccentBright
			}
			body = append(body, overlay.Fill(
				overlay.Style(bg).Foreground(ink).Render(overlay.Truncate(marker+line, width)),
				width, bg))
		}
	}
	hints := []overlay.Hint{
		{Key: "j/k", Label: "move"},
		{Key: "esc", Label: "close"},
	}
	if d.kind == "repos" {
		hints = append([]overlay.Hint{{Key: "enter", Label: "focus pane"}}, hints...)
	}
	content, geo := overlay.Dialog{
		Title: "git · " + d.title,
		Width: width,
		Body:  strings.Join(body, "\n"),
		Hints: hints,
	}.Render(pal)
	return content, geo, nil
}
