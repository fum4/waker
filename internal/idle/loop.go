package idle

import (
	"context"
	"time"
)

// MaxSleep caps how long the loop sleeps without re-planning.
const MaxSleep = 60 * time.Second

// Signal wakes the idle loop to re-plan. Notify never blocks and never loses
// a wake-up: a pending one is kept until the loop takes it.
type Signal chan struct{}

// NewSignal returns a ready Signal.
func NewSignal() Signal { return make(Signal, 1) }

// Notify asks the loop to re-plan.
func (s Signal) Notify() {
	select {
	case s <- struct{}{}:
	default:
	}
}

// Loop runs step until ctx ends. step stops whatever is due and returns how
// long until the next deadline (Loop caps it at MaxSleep); the loop sleeps
// exactly that long unless the signal fires first.
func Loop(ctx context.Context, sig Signal, step func() time.Duration) {
	for {
		d := min(step(), MaxSleep)
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-sig:
		case <-t.C:
		}
		t.Stop()
	}
}
