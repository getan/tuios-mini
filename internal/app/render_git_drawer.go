package app

import (
	"strings"

	"github.com/getan/tuios-mini/internal/overlay"
	"github.com/getan/tuios-mini/internal/review"
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
			if d.kind == review.HistoryRepos {
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
		{Key: "esc/ctrl+c", Label: "close"},
	}
	if gitDrawerHasDetail(d.kind) {
		hints = append([]overlay.Hint{{Key: "enter", Label: "open"}}, hints...)
	}
	if d.kind == review.HistoryRepos {
		hints = append([]overlay.Hint{{Key: "enter", Label: "select repo"}}, hints...)
	}
	if gitDrawerIsDetail(d.kind) {
		hints[len(hints)-1].Label = "back"
	}
	content, geo := overlay.Dialog{
		Title: "git · " + d.title,
		Width: width,
		Body:  strings.Join(body, "\n"),
		Hints: hints,
	}.Render(pal)
	var rows []overlayRowHit
	if !d.loading && d.err == "" && len(d.rows) > 0 {
		end := min(d.scroll+visible, len(d.rows))
		for i := d.scroll; i < end; i++ {
			rowY := geo.BodyY + (i - d.scroll)
			rows = append(rows, overlayRowHit{
				Rect: overlay.Rect{X0: 0, Y0: rowY, X1: geo.Width, Y1: rowY + 1},
				Idx:  i,
			})
		}
	}
	return content, geo, rows
}

func gitDrawerHasDetail(kind review.HistoryKind) bool {
	switch kind {
	case review.HistoryCommits, review.HistoryGraph, review.HistoryBranches,
		review.HistoryTags, review.HistoryStashes, review.HistoryRemotes, review.HistoryFile:
		return true
	}
	return false
}

func gitDrawerIsDetail(kind review.HistoryKind) bool {
	switch kind {
	case review.HistoryCommit, review.HistoryBranch, review.HistoryTag,
		review.HistoryStash, review.HistoryRemote:
		return true
	}
	return false
}
