package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestReviewMouseSelectsVisibleDiffAndCopies(t *testing.T) {
	m, _ := reviewOS(t)
	openReviewed(t, m)
	frame := strings.Split(ansi.Strip(m.renderReview()), "\n")
	for y, line := range frame {
		if x := strings.Index(line, "for attempt"); x >= 0 {
			x = ansi.StringWidth(line[:x])
			m.ReviewMousePress(x, y)
			m.ReviewMouseDrag(x+3, y)
			s := &m.review.selection
			if !s.active {
				t.Fatal("visible diff press did not begin a selection")
			}
			start, end, ok := s.rowBounds(s.ay)
			if !ok || ansi.Strip(ansi.Cut(s.lines[s.ay], start, end)) != "for" {
				t.Fatalf("selection did not address the displayed text: %d %d", start, end)
			}
			if cmd := m.ReviewMouseRelease(x+3, y); cmd == nil {
				t.Fatal("selection did not produce a clipboard command")
			}
			if s.active {
				t.Fatal("release left selection active")
			}
			return
		}
	}
	t.Fatalf("diff line not visible: %v", frame)
}

func TestReviewSelectionIsCancelledWhenFrameChanges(t *testing.T) {
	s := reviewMouseSelection{}
	s.record([]string{"abc"}, 5, 3, 3)
	s.active = true
	s.record([]string{"def"}, 5, 3, 3)
	if s.active {
		t.Fatal("changed frame retained old selection")
	}
}
