package app

import (
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

// The fixture's fake clock starts at 2026-11-01 00:00 UTC. CRON_TZ=UTC keeps
// the windows independent of the host's zone.
func scheduled(mode config.Mode, idle time.Duration, entries ...config.ScheduleEntry) func(*config.App) {
	return func(a *config.App) {
		a.Mode, a.Idle, a.Schedule = mode, config.Duration(idle), entries
	}
}

func window(cron string, d time.Duration) config.ScheduleEntry {
	return config.ScheduleEntry{Cron: "CRON_TZ=UTC " + cron, For: config.Duration(d)}
}

// nineToTen is one window, 09:00–10:00 UTC every day.
var nineToTen = window("0 9 * * *", time.Hour)

func TestStartupInsideAWindowIsOn(t *testing.T) {
	f := newFixture(t, scheduled(config.ModeOnDemand, 10*time.Minute, window("0 0 * * *", 2*time.Hour)))
	if f.app.Mode() != config.ModeOn {
		t.Fatalf("mode = %s, want on", f.app.Mode())
	}
	if _, ok := f.app.DeadlineIn(); ok {
		t.Fatal("an idle deadline inside a window")
	}
	if left, ok := f.app.NextEventIn(); !ok || left != 2*time.Hour {
		t.Fatalf("NextEventIn = %s, %v; want the window's end in 2h", left, ok)
	}
	f.app.Tick() // the startup mode is the last one seen: no transition
	f.app.Wait()
	assertRuns(t, f)
	s := f.app.Snapshot(ctx)
	if s.Schedule == nil || !s.Schedule.Active || s.Schedule.Until == nil || s.Schedule.Window.Cron != "CRON_TZ=UTC 0 0 * * *" {
		t.Fatalf("schedule = %+v", s.Schedule)
	}
}

func TestWindowTransitions(t *testing.T) {
	tests := []struct {
		name     string
		mode     config.Mode
		late     time.Duration // how late the loop ticks after the window closes
		runs     []string      // after the window closes
		deadline bool          // an idle deadline of a full idle after the close
	}{
		{"declared off is stopped", config.ModeOff, 0, []string{"start", "stop"}, false},
		{"on-demand gets a full idle", config.ModeOnDemand, 0, []string{"start"}, true},
		{"on-demand, loop late by an hour", config.ModeOnDemand, time.Hour, []string{"start"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, scheduled(tt.mode, 10*time.Minute, nineToTen))
			if left, ok := f.app.NextEventIn(); !ok || left > 9*time.Hour {
				t.Fatalf("NextEventIn = %s, %v; want at most 9h", left, ok)
			}
			f.clock.Advance(9 * time.Hour)
			f.app.Tick()
			f.app.Wait()
			assertRuns(t, f, "start")
			if f.app.Mode() != config.ModeOn {
				t.Fatalf("in the window: mode %s", f.app.Mode())
			}
			if left, ok := f.app.NextEventIn(); !ok || left != time.Hour {
				t.Fatalf("in the window: NextEventIn = %s, %v; want 1h", left, ok)
			}

			f.clock.Advance(time.Hour + tt.late)
			f.app.Tick()
			f.app.Wait()
			assertRuns(t, f, tt.runs...)
			if f.app.Mode() != tt.mode {
				t.Fatalf("after: mode %s, want %s", f.app.Mode(), tt.mode)
			}
			left, ok := f.app.DeadlineIn()
			if ok != tt.deadline || (ok && left != 10*time.Minute) {
				t.Fatalf("after: DeadlineIn = %s, %v", left, ok)
			}
		})
	}
}

func TestOverrideBeatsTheSchedule(t *testing.T) {
	f := newFixture(t, scheduled(config.ModeOnDemand, 10*time.Minute, window("0 0 * * *", 2*time.Hour)))
	f.proc.setUp(true)
	// Even the declared mode pins it: the override is kept, not dropped.
	if err := f.app.SetMode(modePtr(config.ModeOnDemand), 0); err != nil {
		t.Fatal(err)
	}
	if f.app.Mode() != config.ModeOnDemand || f.app.Override() == nil {
		t.Fatalf("mode %s, override %+v", f.app.Mode(), f.app.Override())
	}
	if _, ok := f.app.DeadlineIn(); !ok {
		t.Fatal("an overridden on-demand app has no idle deadline")
	}
	f.app.Tick() // SetMode acted on the change; Tick must not act again
	f.app.Wait()
	assertRuns(t, f)

	if err := f.app.SetMode(modePtr(config.ModeOff), 0); err != nil {
		t.Fatal(err)
	}
	f.app.Wait()
	f.app.Tick()
	f.app.Wait()
	assertRuns(t, f, "stop")
	if s := f.app.Snapshot(ctx); s.Mode != config.ModeOff || s.Schedule == nil || !s.Schedule.Active {
		t.Fatalf("snapshot mode %s, schedule %+v", s.Mode, s.Schedule)
	}

	if err := f.app.SetMode(nil, 0); err != nil { // back to the schedule: on
		t.Fatal(err)
	}
	f.app.Wait()
	assertRuns(t, f, "stop", "start")
	if f.app.Mode() != config.ModeOn {
		t.Fatalf("after reset: mode %s", f.app.Mode())
	}
}

func TestNoScheduleNoStatus(t *testing.T) {
	f := newFixture(t, nil)
	if s := f.app.Snapshot(ctx); s.Schedule != nil {
		t.Fatalf("schedule = %+v", s.Schedule)
	}
	if _, ok := f.app.NextEventIn(); !ok { // on-demand: its idle deadline
		t.Fatal("no idle deadline")
	}
}

func TestOutsideAWindowShowsTheNext(t *testing.T) {
	f := newFixture(t, scheduled(config.ModeOff, 10*time.Minute, nineToTen))
	s := f.app.Snapshot(ctx).Schedule
	want := f.clock.Now().Add(9 * time.Hour)
	if s == nil || s.Active || s.Next == nil || !s.Next.Equal(want) || s.Until != nil {
		t.Fatalf("schedule = %+v; want next %s", s, want)
	}
}
