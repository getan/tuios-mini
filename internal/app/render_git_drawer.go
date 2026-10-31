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
	// Mouse controls replace the key legend, including while loading or after
	// an error. Keep them in the body so they have real click targets.
	controlsY := len(body)
	controls := "[close]"
	if d.parent != nil {
		if width >= 16 {
			controls = "[back]  [close]"
		} else {
			body = append(body, dialogBodyLine("[back]", width, pal.AccentBright, bg))
		}
	}
	body = append(body, dialogBodyLine(controls, width, pal.AccentBright, bg))
	content, geo := overlay.Dialog{
		Title: "git · " + d.title,
		Width: width,
		Body:  strings.Join(body, "\n"),
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
	y := geo.BodyY + controlsY
	x := geo.BodyX + 1
	if d.parent != nil {
		rows = append(rows, overlayRowHit{Rect: overlay.Rect{X0: x, Y0: y, X1: x + 6, Y1: y + 1}, Idx: gitDrawerBackRow})
		if width >= 16 {
			x += 8
		} else {
			y++
		}
	}
	rows = append(rows, overlayRowHit{Rect: overlay.Rect{X0: x, Y0: y, X1: min(x+7, geo.Width-1), Y1: y + 1}, Idx: gitDrawerCloseRow})
	return content, geo, rows
}

const (
	gitDrawerBackRow  = -1
	gitDrawerCloseRow = -2
)
