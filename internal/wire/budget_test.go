package wire

import (
	"testing"
	"time"
)

// The read budget is what keeps one large frame from spending the memory
// another needs. A charge is taken whole or not at all, so a frame either has
// its memory or is refused, and never holds part of it while it waits.
func TestBudgetTakeAndGiveBack(t *testing.T) {
	b := NewBudget(PersonBudgetBytes)
	if !b.Whole() {
		t.Fatal("a new budget is not whole")
	}

	release, ok := b.Acquire(PersonBudgetBytes/2, time.Second)
	if !ok {
		t.Fatal("half the budget was refused")
	}
	if b.Whole() {
		t.Fatal("a budget with half of it taken reported whole")
	}
	// A charge that does not fit is refused, and takes nothing: the half that
	// is free is still free.
	if _, ok := b.Acquire(PersonBudgetBytes, 10*time.Millisecond); ok {
		t.Fatal("a charge larger than what is free was taken")
	}
	if other, ok := b.Acquire(PersonBudgetBytes/2, 10*time.Millisecond); !ok {
		t.Fatal("the free half was refused after a charge that took nothing")
	} else {
		other()
	}
	release()
	if !b.Whole() {
		t.Fatal("the budget was not given back")
	}
}

// A charge larger than the budget is cut to the budget rather than refused for
// its size alone: the frame waits for all of it.
func TestBudgetCutsAnOversizedCharge(t *testing.T) {
	b := NewBudget(1 << 20)
	release, ok := b.Acquire(16<<20, time.Second)
	if !ok {
		t.Fatal("a charge larger than the budget was refused outright")
	}
	if b.Whole() {
		t.Fatal("the budget was not taken")
	}
	// Nothing is left while it is held.
	if _, ok := b.Acquire(1, 10*time.Millisecond); ok {
		t.Fatal("a byte was taken from a spent budget")
	}
	release()
	if !b.Whole() {
		t.Fatal("the budget was not given back")
	}
	if b.Size() != 1<<20 {
		t.Fatalf("the budget is %d bytes", b.Size())
	}
}

// A charge of nothing takes nothing and needs no release.
func TestBudgetZeroCharge(t *testing.T) {
	b := NewBudget(1 << 20)
	release, ok := b.Acquire(0, time.Millisecond)
	if !ok {
		t.Fatal("a charge of nothing was refused")
	}
	release()
	if !b.Whole() {
		t.Fatal("a charge of nothing took the budget")
	}
}

// The wait is bounded: a budget that cannot free in time refuses instead of
// holding the reader.
func TestBudgetWaitIsBounded(t *testing.T) {
	b := NewBudget(1 << 20)
	held, ok := b.Acquire(1<<20, time.Second)
	if !ok {
		t.Fatal("the whole budget was refused")
	}
	start := time.Now()
	if _, ok := b.Acquire(1<<20, 50*time.Millisecond); ok {
		t.Fatal("a spent budget gave a charge it did not have")
	}
	if waited := time.Since(start); waited < 50*time.Millisecond || waited > time.Second {
		t.Fatalf("the wait was %v, and it was given 50ms", waited)
	}
	held()
	if !b.Whole() {
		t.Fatal("the budget was not given back")
	}
}
