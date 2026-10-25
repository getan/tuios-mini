package wire

import (
	"context"
	"time"

	"golang.org/x/sync/semaphore"
)

// The sizes at which a reader starts charging memory, and the budgets it
// charges. The session package's frame_budget.go holds the policy that draws
// on them (which connection draws on which budget, what a refused frame is
// told); what is here is the arithmetic of the charge itself.
const (
	// LargeFrame is the payload size past which a frame is charged to the
	// budget. Smaller frames are what a client sends for every key and
	// resize, and the connection cap bounds what they can hold.
	LargeFrame = 64 << 10

	// FirstFrameChunk is the most memory a frame body is given before any of
	// it has arrived.
	FirstFrameChunk = 4 << 10

	// FrameGrowStep is the most a frame's buffer grows by at once. Up to it,
	// the buffer doubles, so a large frame is copied a few times and not
	// once per chunk.
	FrameGrowStep = 4 << 20

	// PersonBudgetBytes is the read budget of the person's own clients, and
	// PeerBudgetBytes the budget links and panes without admin share. The
	// largest charges are a verb request line, maxVerbLine times lineCopies
	// (36 MiB), and a 16 MiB frame, charged twice. The person's budget holds
	// two of either at once, and the other budget one.
	PersonBudgetBytes = 96 << 20
	PeerBudgetBytes   = 48 << 20

	// ReadBudgetWait is how long a large frame waits for its charge. The
	// client gives a write 5 seconds, and a frame that waits is not being
	// read, so this must stay well under that.
	ReadBudgetWait = 2 * time.Second

	// FrameBodyDeadline is how long a reader waits for the rest of a frame
	// once its length has arrived. 16 MiB in this time is about 560 KiB/s.
	FrameBodyDeadline = 30 * time.Second
)

// FrameCopies is how many times its size a large frame is charged: the
// payload, and either the copy the gob decoder makes of it or, while the
// buffer grows, the smaller buffer it grew from.
const FrameCopies = 2

// Budget is memory shared among reads that need a lot of it. It is a FIFO
// semaphore, and a charge is taken whole or not at all.
type Budget struct {
	sem  *semaphore.Weighted
	size int64
}

// NewBudget makes a budget of size bytes.
func NewBudget(size int64) *Budget {
	return &Budget{sem: semaphore.NewWeighted(size), size: size}
}

// Acquire takes n bytes of the budget, waiting up to wait. It returns the
// function that gives them back, or false when the budget did not free in
// time, having taken nothing. A charge larger than the budget is cut to the
// budget, so it waits for all of it and is never refused for its size alone.
func (b *Budget) Acquire(n int64, wait time.Duration) (func(), bool) {
	n = min(n, b.size)
	if n <= 0 {
		return func() {}, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := b.sem.Acquire(ctx, n); err != nil {
		return nil, false
	}
	return func() { b.sem.Release(n) }, true
}

// Whole reports whether none of the budget is taken, which is how a test sees
// that every charge was given back.
func (b *Budget) Whole() bool {
	if !b.sem.TryAcquire(b.size) {
		return false
	}
	b.sem.Release(b.size)
	return true
}

// Size is the number of bytes the budget holds.
func (b *Budget) Size() int64 { return b.size }
