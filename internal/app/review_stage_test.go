package app

import (
	"testing"

	"github.com/getan/tuios-mini/internal/review"
)

func TestReviewStageLinePicksAChangedSide(t *testing.T) {
	h := &review.Hunk{Lines: []review.Line{
		{Op: review.OpContext},
		{Op: review.OpAdd},
		{Op: review.OpDelete},
	}}
	if got, ok := reviewStageLine(h, reviewRow{line: 1, other: -1}); !ok || got != 1 {
		t.Fatalf("new side = %d, %v", got, ok)
	}
	if got, ok := reviewStageLine(h, reviewRow{line: 0, other: 2}); !ok || got != 2 {
		t.Fatalf("old side = %d, %v", got, ok)
	}
	if _, ok := reviewStageLine(h, reviewRow{line: 0, other: -1}); ok {
		t.Fatal("a context line was stageable")
	}
}
