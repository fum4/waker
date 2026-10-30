// Package schedule is the pure window math behind an app's `schedule:`:
// each entry is a cron spec and a duration, and every fire time t of the
// spec opens the window [t, t+for). It knows nothing of apps or modes.
package schedule

import (
	"errors"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// MaxChain bounds how many later fires one State call follows to merge
// overlapping or adjacent windows. A chain longer than that (e.g. a fire
// every minute, each for a minute) reports an earlier end; at that end the
// app is still found inside a window, so only the displayed end and the
// loop's wake-up are early, never the mode.
const MaxChain = 64

// Window is one schedule entry as written.
type Window struct {
	Cron string        `json:"cron"`
	For  time.Duration `json:"for"`
}

type entry struct {
	w    Window
	spec *cron.SpecSchedule
	loc  *time.Location
}

// Schedule is a parsed list of windows. The zero value (or nil) has none.
type Schedule struct{ entries []entry }

// Parse reads a standard 5-field cron spec (or a fixed descriptor such as
// @daily), with an optional CRON_TZ=Area/City prefix.
func Parse(spec string) (*cron.SpecSchedule, error) {
	s, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, err
	}
	ss, ok := s.(*cron.SpecSchedule)
	if !ok {
		// @every counts from whenever it is asked, so it has no fixed windows.
		return nil, errors.New("@every has no fixed fire times; use a cron spec")
	}
	if ss.Next(time.Now()).IsZero() {
		return nil, errors.New("never fires")
	}
	return ss, nil
}

// New parses ws; a spec without CRON_TZ is read in loc (time.Local in the
// daemon).
func New(ws []Window, loc *time.Location) (*Schedule, error) {
	s := &Schedule{}
	for i, w := range ws {
		spec, err := Parse(w.Cron)
		if err != nil {
			return nil, fmt.Errorf("schedule[%d].cron: %w", i, err)
		}
		if w.For <= 0 {
			return nil, fmt.Errorf("schedule[%d].for: must be positive", i)
		}
		eloc := spec.Location
		if eloc == time.Local {
			eloc = loc
		}
		s.entries = append(s.entries, entry{w: w, spec: spec, loc: eloc})
	}
	return s, nil
}

// Empty reports whether the schedule has no windows.
func (s *Schedule) Empty() bool { return s == nil || len(s.entries) == 0 }

// next is the entry's first fire strictly after t.
func (e entry) next(t time.Time) time.Time {
	// robfig reads a zoneless spec in t's own zone, so convert first.
	return e.spec.Next(t.In(e.loc))
}

// activeFire returns the start of a window of e that contains now.
func (e entry) activeFire(now time.Time) (time.Time, bool) {
	t := e.next(now.Add(-e.w.For))
	return t, !t.IsZero() && !t.After(now)
}

// Active reports whether now is inside any window: some fire t with
// t <= now < t+for.
func (s *Schedule) Active(now time.Time) bool {
	if s.Empty() {
		return false
	}
	for _, e := range s.entries {
		if _, ok := e.activeFire(now); ok {
			return true
		}
	}
	return false
}

// State is the schedule at one instant.
type State struct {
	Active bool
	// Until is the end of the merged window now is in (Active).
	Until time.Time
	// Next is the start of the next window (not Active); zero if none.
	Next time.Time
	// Window is the entry that opened the current window, or opens the next.
	Window Window
}

// At returns the state at now. Windows that overlap or touch, from one
// entry or several, merge into one: Until is where the chain ends.
func (s *Schedule) At(now time.Time) State {
	if s.Empty() {
		return State{}
	}
	var st State
	for _, e := range s.entries {
		if t, ok := e.activeFire(now); ok && (!st.Active || t.Add(e.w.For).After(st.Until)) {
			st = State{Active: true, Until: t.Add(e.w.For), Window: e.w}
		}
	}
	if st.Active {
		st.Until = s.chainEnd(now, st.Until)
		return st
	}
	for _, e := range s.entries {
		if t := e.next(now); !t.IsZero() && (st.Next.IsZero() || t.Before(st.Next)) {
			st.Next, st.Window = t, e.w
		}
	}
	return st
}

// chainEnd extends end over every later fire that starts at or before it.
func (s *Schedule) chainEnd(now, end time.Time) time.Time {
	steps := 0
	for grew := true; grew && steps < MaxChain; {
		grew = false
		for _, e := range s.entries {
			// Fires at exactly end touch the window and merge with it.
			for t := e.next(now); !t.IsZero() && !t.After(end) && steps < MaxChain; t = e.next(t) {
				steps++
				if t.Add(e.w.For).After(end) {
					end, grew = t.Add(e.w.For), true
				}
			}
		}
	}
	return end
}

// Boundary is the next instant the Active answer may change: the end of
// the current window or the start of the next. ok is false when none is
// coming.
func (s *Schedule) Boundary(now time.Time) (time.Time, bool) {
	st := s.At(now)
	if st.Active {
		return st.Until, true
	}
	return st.Next, !st.Next.IsZero()
}
