package update

import (
	"context"
	"os"
	"time"

	"github.com/fum4/waker/internal/state"
)

const (
	// CheckEvery caps the background check: one request a day at most,
	// shared by serve and every CLI command (state_dir/update.json).
	CheckEvery = 24 * time.Hour
	// CheckTimeout bounds the check's one request.
	CheckTimeout = 5 * time.Second
	// EnvNoCheck, set (to anything but 0), stops every network contact
	// waker makes on its own; an explicit `waker update` still works.
	EnvNoCheck = "WAKER_NO_UPDATE_CHECK"
)

// Unattended reports whether waker may contact the release home without
// being asked ($WAKER_NO_UPDATE_CHECK unset).
func Unattended() bool {
	v := os.Getenv(EnvNoCheck)
	return v == "" || v == "0"
}

// Check is the background look for a newer release, cached in a state dir.
type Check struct {
	Source   Source
	StateDir string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Latest is the newest release tag waker knows of: the one recorded when
// the last check is under CheckEvery old, else the answer of one GET of
// <base>/latest bounded by CheckTimeout. The slot is recorded before the
// request, so a failing network costs one try a day; when it can't be
// recorded (a state dir this user can't write), Latest stays off the
// network. It is silent: ok is false when no tag is known.
func (c Check) Latest(ctx context.Context) (tag Tag, ok bool) {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	rec, err := state.LoadUpdate(c.StateDir)
	if err != nil {
		rec = state.Update{} // unreadable or torn: start over
	}
	age := now.Sub(rec.CheckedAt)
	if rec.CheckedAt.IsZero() || age < 0 || age >= CheckEvery {
		rec.CheckedAt = now
		if state.SaveUpdate(c.StateDir, rec) == nil {
			ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
			defer cancel()
			if t, err := c.Source.Latest(ctx); err == nil {
				rec.Latest = t.String()
				_ = state.SaveUpdate(c.StateDir, rec) // the next check is a day away either way
			}
		}
	}
	t, err := ParseTag(rec.Latest)
	return t, err == nil
}

// Available is the line that announces a newer release than cur, or ""
// when there is none (or cur is a dev build, which no release replaces).
func Available(cur Version, latest Tag) string {
	if !cur.Known || !cur.OlderThan(latest) {
		return ""
	}
	return "waker " + latest.String() + " available (running " + cur.String() + "): waker update"
}
