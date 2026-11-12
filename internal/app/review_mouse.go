package app

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// reviewMouseSelection addresses the visible diff cells, not the underlying
// file: split view, scrolling and clipping are exactly as the person sees them.
type reviewMouseSelection struct {
	lines          []string
	x, y, width    int
	active         bool
	ax, ay, bx, by int
}

func (s *reviewMouseSelection) record(lines []string, x, y, width int) {
	if s.active {
		if s.x == x && s.y == y && s.width == width && slices.Equal(s.lines, lines) {
			return
		}
		// Never copy old cells after a scroll, resize or refresh changed them.
		s.active = false
	}
	s.lines = append(s.lines[:0], lines...)
	s.x, s.y, s.width = x, y, width
}

func (s *reviewMouseSelection) point(x, y int) (int, int) {
	return min(max(x-s.x, 0), s.width), min(max(y-s.y, 0), len(s.lines)-1)
}

func (s *reviewMouseSelection) bounds() (int, int, int, int) {
	ax, ay, bx, by := s.ax, s.ay, s.bx, s.by
	if ay > by || (ay == by && ax > bx) {
		ax, ay, bx, by = bx, by, ax, ay
	}
	return ax, ay, bx, by
}

func (s *reviewMouseSelection) rowBounds(row int) (int, int, bool) {
	ax, ay, bx, by := s.bounds()
	if row < ay || row > by || (ay == by && ax == bx) {
		return 0, 0, false
	}
	start, end := 0, s.width
	if row == ay {
		start = ax
	}
	if row == by {
		end = bx
	}
	return start, end, start < end
}

func (s *reviewMouseSelection) draw(line string, row int) string {
	if !s.active {
		return line
	}
	start, end, ok := s.rowBounds(row)
	if !ok {
		return line
	}
	left := ansi.Cut(line, 0, start)
	middle := ansi.Strip(ansi.Cut(line, start, end))
	right := ansi.Cut(line, end, s.width)
	return left + lipgloss.NewStyle().Reverse(true).Render(middle) + right
}

// ReviewMousePress begins a selection only in the diff, never in its file list.
func (m *OS) ReviewMousePress(x, y int) {
	s := &m.review.selection
	s.active = false
	if m.ReviewCompareShown() || len(s.lines) == 0 || x < s.x || x >= s.x+s.width || y < s.y || y >= s.y+len(s.lines) {
		return
	}
	s.ax, s.ay = s.point(x, y)
	s.bx, s.by, s.active = s.ax, s.ay, true
}

func (m *OS) ReviewMouseDrag(x, y int) {
	s := &m.review.selection
	if s.active {
		s.bx, s.by = s.point(x, y)
	}
}

// ReviewMouseRelease copies the selected visible cells, excluding ANSI codes.
func (m *OS) ReviewMouseRelease(x, y int) tea.Cmd {
	s := &m.review.selection
	if !s.active {
		return nil
	}
	s.bx, s.by = s.point(x, y)
	var lines []string
	_, ay, _, by := s.bounds()
	for row := ay; row <= by; row++ {
		start, end, ok := s.rowBounds(row)
		if !ok {
			lines = append(lines, "")
			continue
		}
		text := ansi.Strip(ansi.Cut(s.lines[row], start, end))
		if row < by {
			text = strings.TrimRight(text, " ")
		}
		lines = append(lines, text)
	}
	s.active = false
	text := strings.Join(lines, "\n")
	if text == "" {
		return nil
	}
	return m.CopyToClipboard(text)
}
