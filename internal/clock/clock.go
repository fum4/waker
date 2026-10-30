// Package clock abstracts time so the idle clock and boot loop are testable
// without real waits.
package clock

import (
	"context"
	"sync"
	"time"
)

// Clock tells the time and sleeps.
type Clock interface {
	Now() time.Time
	// Sleep waits d or until ctx is done, returning ctx.Err() in that case.
	Sleep(ctx context.Context, d time.Duration) error
}

// Real is the wall clock.
type Real struct{}

// Now returns time.Now().
func (Real) Now() time.Time { return time.Now() }

// Sleep waits d or until ctx is done.
func (Real) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Fake is a manual clock for tests: Sleep advances it instead of waiting.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a fake clock set to a fixed instant.
func NewFake() *Fake {
	return &Fake{now: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
}

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake time forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// Sleep advances the fake time by d without waiting.
func (f *Fake) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.Advance(d)
	return nil
}
