package app

import (
	"context"
	"sync"
	"time"
)

// StateCacheTTL is how long a visitor-facing state is reused. Starting
// pages poll every 3s; without a cache each poll would probe the app (and a
// command health check would spawn a process per unauthenticated request).
const StateCacheTTL = time.Second

// stateCache coalesces visitor state probes: one probe in flight per app,
// its result reused for StateCacheTTL.
type stateCache struct {
	mu      sync.Mutex
	state   State
	at      time.Time
	probing chan struct{} // closed when the in-flight probe finishes
}

// VisitorState returns the app's state for an unauthenticated poll: cached
// for StateCacheTTL, with concurrent callers sharing one probe.
func (a *App) VisitorState(ctx context.Context) State {
	c := &a.cache
	c.mu.Lock()
	if c.state != "" && a.d.Clock.Now().Sub(c.at) < StateCacheTTL {
		st := c.state
		c.mu.Unlock()
		return st
	}
	if ch := c.probing; ch != nil {
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return Down
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.state
	}
	ch := make(chan struct{})
	c.probing = ch
	c.mu.Unlock()

	// Detached from the caller: a visitor hanging up must not cancel the
	// probe others are waiting on (the health timeout bounds it).
	st := a.State(context.Background())

	c.mu.Lock()
	c.state, c.at, c.probing = st, a.d.Clock.Now(), nil
	c.mu.Unlock()
	close(ch)
	return st
}
