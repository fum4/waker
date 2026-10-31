package update

import (
	"context"
	"os"
	"time"

	"github.com/fum4/waker/internal/state"
)

const (
	// CheckEvery spaces the background checks: one request a day, shared
	// by serve and every CLI command through state_dir/update.json.
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
	// Timeout bounds the request; zero is CheckTimeout.
	Timeout time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Latest is the newest release tag waker knows of. When the last check is
// a day old (or there is none) it first asks <base>/latest, once. Any
// answer, a failure or a timeout included, counts as the day's check, so
// an offline box isn't slowed down by every command; a ctx the caller
// cancels doesn't. If update.json can't be written (a state dir this user
// can't write) nothing could cap the checks, so it makes none. It is
// silent: ok is false when no tag is known.
func (c Check) Latest(ctx context.Context) (tag Tag, ok bool) {
	now := c.now()
	rec, err := state.LoadUpdate(c.StateDir)
	if err != nil {
		rec = state.Update{} // unreadable or torn: start over
	}
	if age := now.Sub(rec.CheckedAt); rec.CheckedAt.IsZero() || age < 0 || age >= CheckEvery {
		if state.SaveUpdate(c.StateDir, rec) == nil { // the record is writable
			timeout := c.Timeout
			if timeout == 0 {
				timeout = CheckTimeout
			}
			rctx, cancel := context.WithTimeout(ctx, timeout)
			t, err := c.Source.Latest(rctx)
			cancel()
			if ctx.Err() == nil {
				rec.CheckedAt = now
				if err == nil {
					rec.Latest = t.String()
				}
				_ = state.SaveUpdate(c.StateDir, rec) // best effort: silent
			}
		}
	}
	return recorded(rec)
}

func (c Check) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func recorded(rec state.Update) (Tag, bool) {
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
