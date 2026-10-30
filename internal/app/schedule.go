package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/schedule"
)

// parseSchedule reads cfg's windows in local time; nil when it has none.
func parseSchedule(cfg config.App, log *slog.Logger) *schedule.Schedule {
	if len(cfg.Schedule) == 0 {
		return nil
	}
	s, err := schedule.New(cfg.Windows(), time.Local)
	if err != nil {
		log.Error("schedule ignored", "err", err) // validation rejects this first
	}
	return s
}

// ScheduleStatus is where the app stands in its schedule.
type ScheduleStatus struct {
	// Active is true inside a window, even while an override outranks it.
	Active bool `json:"active"`
	// Until is the current window's end (merged with any it touches).
	Until *time.Time `json:"until,omitempty"`
	// Next is the next window's start, when outside one.
	Next *time.Time `json:"next,omitempty"`
	// Window is the entry that opened the current window, or opens the next.
	Window *config.ScheduleEntry `json:"window,omitempty"`
}

// inWindow reports whether a schedule window is open now. The schedule is
// immutable, so no lock is needed.
func (a *App) inWindow() bool { return a.sched.Active(a.d.Clock.Now()) }

// followSchedule acts on a mode change the schedule made since the last
// tick: a window opened (wake, like mode on) or closed (back to the
// declared mode). Comparing with the last mode seen, not with the clock,
// means a boundary a late loop slept past is still handled.
func (a *App) followSchedule() {
	if a.sched.Empty() {
		return
	}
	a.mu.Lock()
	mode, prev := a.modeLocked(), a.lastMode
	a.lastMode = mode
	a.mu.Unlock()
	if mode == prev {
		return
	}
	a.d.Log.Info("schedule", "mode", mode, "was", prev)
	switch mode {
	case config.ModeOn:
		// In the background: a health probe must not stall the idle loop.
		a.goBackground(func() { a.Wake(context.Background()) })
	case config.ModeOff:
		a.goBackground(func() { a.Stop("schedule window closed") })
	case config.ModeOnDemand:
		// One full idle period from the window's end, like any mode change.
		a.activity.Touch()
		a.d.Notify()
	}
}

// NextEventIn is how long the idle loop may sleep for this app: until its
// idle stop or its next schedule boundary, whichever is sooner.
func (a *App) NextEventIn() (time.Duration, bool) {
	left, ok := a.DeadlineIn()
	now := a.d.Clock.Now()
	if b, has := a.sched.Boundary(now); has {
		if in := max(0, b.Sub(now)); !ok || in < left {
			left, ok = in, true
		}
	}
	return left, ok
}

func (a *App) scheduleStatus() *ScheduleStatus {
	if a.sched.Empty() {
		return nil
	}
	st := a.sched.At(a.d.Clock.Now())
	s := &ScheduleStatus{Active: st.Active}
	switch {
	case st.Active:
		s.Until = &st.Until
	case !st.Next.IsZero():
		s.Next = &st.Next
	default:
		return s
	}
	s.Window = &config.ScheduleEntry{Cron: st.Window.Cron, For: config.Duration(st.Window.For)}
	return s
}
