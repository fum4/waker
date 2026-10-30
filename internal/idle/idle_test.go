package idle

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fum4/waker/internal/clock"
)

func newTracker() (*Tracker, *clock.Fake, *atomic.Int32) {
	c := clock.NewFake()
	var n atomic.Int32
	return NewTracker(c, func() { n.Add(1) }), c, &n
}

func TestExpiresAtTheExactDeadline(t *testing.T) {
	tr, c, _ := newTracker()
	idle := 30 * time.Minute
	c.Advance(29 * time.Minute)
	if tr.Expire(idle) {
		t.Fatal("expired 1m early")
	}
	if left, ok := tr.DeadlineIn(idle); !ok || left != time.Minute {
		t.Fatalf("DeadlineIn = %v, %v; want 1m", left, ok)
	}
	c.Advance(time.Minute)
	if !tr.Expire(idle) {
		t.Fatal("did not expire at the deadline")
	}
}

func TestExpiresOnceUntilTheNextActivity(t *testing.T) {
	tr, c, notified := newTracker()
	c.Advance(10 * time.Second)
	if !tr.Expire(10 * time.Second) {
		t.Fatal("no first expiry")
	}
	c.Advance(10 * time.Second)
	if tr.Expire(10 * time.Second) {
		t.Fatal("expired twice")
	}
	if _, ok := tr.DeadlineIn(10 * time.Second); ok {
		t.Fatal("a deadline while disarmed")
	}
	before := notified.Load()
	tr.Touch()
	if notified.Load() != before+1 {
		t.Fatal("re-arming did not notify the loop")
	}
	if left, ok := tr.DeadlineIn(10 * time.Second); !ok || left != 10*time.Second {
		t.Fatalf("after touch DeadlineIn = %v, %v", left, ok)
	}
}

func TestNoDeadlineWhileARequestIsInFlight(t *testing.T) {
	tr, c, notified := newTracker()
	tr.Begin()
	c.Advance(time.Hour)
	if tr.Expire(10*time.Second) || tr.Inflight() != 1 {
		t.Fatal("expired mid-request")
	}
	if _, ok := tr.DeadlineIn(10 * time.Second); ok {
		t.Fatal("deadline while in flight")
	}
	before := notified.Load()
	tr.End()
	if notified.Load() <= before {
		t.Fatal("End did not notify")
	}
	if left, ok := tr.DeadlineIn(10 * time.Second); !ok || left != 10*time.Second {
		t.Fatalf("clock must restart at the request's end: %v, %v", left, ok)
	}
}

func TestActivityResetsTheClock(t *testing.T) {
	tr, c, _ := newTracker()
	c.Advance(29 * time.Minute)
	tr.Touch()
	c.Advance(29 * time.Minute)
	if tr.Expire(30 * time.Minute) {
		t.Fatal("expired despite activity")
	}
	if got := tr.IdleIn(30 * time.Minute); got != time.Minute {
		t.Fatalf("IdleIn = %v", got)
	}
}

func TestDisarm(t *testing.T) {
	tr, c, _ := newTracker()
	tr.Disarm()
	c.Advance(time.Hour)
	if tr.Expire(time.Second) {
		t.Fatal("expired while disarmed")
	}
}

func TestSignalNeverBlocksNorLoses(t *testing.T) {
	s := NewSignal()
	s.Notify()
	s.Notify() // must not block
	select {
	case <-s:
	default:
		t.Fatal("pending wake-up lost")
	}
}

func TestLoopSleepsToTheStepAndWakesOnSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sig := NewSignal()
	var steps atomic.Int32
	done := make(chan struct{})
	go func() {
		Loop(ctx, sig, func() time.Duration { steps.Add(1); return time.Hour })
		close(done)
	}()
	waitFor(t, func() bool { return steps.Load() == 1 })
	sig.Notify()
	waitFor(t, func() bool { return steps.Load() == 2 })
	cancel()
	<-done
}

func TestLoopHonoursShortDeadlines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var steps atomic.Int32
	go Loop(ctx, NewSignal(), func() time.Duration { steps.Add(1); return 10 * time.Millisecond })
	waitFor(t, func() bool { return steps.Load() >= 3 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 5s")
		}
		time.Sleep(time.Millisecond)
	}
}
