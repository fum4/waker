package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

func TestRenderSchedule(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) // a Friday
	at := func(day, h int) *time.Time { t := time.Date(2026, 11, day, h, 0, 0, 0, time.UTC); return &t }
	workday := &config.ScheduleEntry{Cron: "0 9 * * 1-5", For: config.Duration(9 * time.Hour)}
	since := now.Add(-time.Minute)
	base := app.Snapshot{Name: "web", DeclaredMode: config.ModeOnDemand, DeclaredIdle: config.Duration(30 * time.Minute), Idle: config.Duration(30 * time.Minute)}
	tests := []struct {
		name      string
		mode      config.Mode
		sched     *app.ScheduleStatus
		override  *time.Time
		want, not []string
	}{
		{"none", config.ModeOnDemand, nil, nil, nil, []string{"schedule:"}},
		{"inside a window", config.ModeOn, &app.ScheduleStatus{Active: true, Until: at(6, 18), Window: workday}, nil,
			[]string{"  mode:      on (schedule; declared on-demand, idle 30m0s)\n", "  schedule:  on until 18:00 (window 0 9 * * 1-5 for 9h)\n"}, nil},
		{"outside, next after the weekend", config.ModeOnDemand, &app.ScheduleStatus{Next: at(9, 9), Window: workday}, nil,
			[]string{"  mode:      on-demand, idle 30m0s\n", "  schedule:  next window Mon 09:00 (window 0 9 * * 1-5 for 9h)\n"}, []string{"(schedule"}},
		{"override outranks the window", config.ModeOff, &app.ScheduleStatus{Active: true, Until: at(6, 18), Window: workday}, &since,
			[]string{"(override, set 60s ago", "on until 18:00 (window 0 9 * * 1-5 for 9h) · the override wins\n"}, []string{"(schedule"}},
		{"next one far off", config.ModeOnDemand, &app.ScheduleStatus{Next: at(25, 9), Window: workday}, nil,
			[]string{"next window Nov 25 09:00"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base
			s.Mode, s.Schedule, s.OverrideSince = tt.mode, tt.sched, tt.override
			var buf bytes.Buffer
			renderStatus(&buf, s, now)
			for _, w := range tt.want {
				if !strings.Contains(buf.String(), w) {
					t.Errorf("missing %q in:\n%s", w, buf.String())
				}
			}
			for _, n := range tt.not {
				if strings.Contains(buf.String(), n) {
					t.Errorf("unexpected %q in:\n%s", n, buf.String())
				}
			}
		})
	}
}

func TestCompactDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{9 * time.Hour: "9h", 90 * time.Minute: "1h30m", 45 * time.Second: "45s", 168 * time.Hour: "168h"} {
		if got := compactDuration(d); got != want {
			t.Errorf("compactDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
