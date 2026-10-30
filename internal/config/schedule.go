package config

import (
	"fmt"
	"time"

	"github.com/fum4/waker/internal/schedule"
)

// MaxScheduleFor caps one window. A week is the longest period a 5-field
// cron repeats on reliably (`0 9 * * 1`); a longer window outlasts its own
// next fire, which is mode on with extra steps.
const MaxScheduleFor = 7 * 24 * time.Hour

// ScheduleEntry is one window: every fire of Cron keeps the app on for For.
type ScheduleEntry struct {
	Cron string   `yaml:"cron" json:"cron"`
	For  Duration `yaml:"for" json:"for"`
}

// Windows returns the schedule in the form package schedule reads.
func (a App) Windows() []schedule.Window {
	ws := make([]schedule.Window, len(a.Schedule))
	for i, e := range a.Schedule {
		ws[i] = schedule.Window{Cron: e.Cron, For: e.For.D()}
	}
	return ws
}

// dropInheritedOnSchedules clears a schedule an app in mode on inherited:
// it is always on, so "every app wakes at 9" simply doesn't apply to it.
// Its own schedule stays, for validation to reject.
func (c *Config) dropInheritedOnSchedules() {
	for i := range c.Apps {
		if a := &c.Apps[i]; a.Mode == ModeOn && c.inherited.fromDefaults(i, "schedule") {
			a.Schedule = nil
		}
	}
}

func (a App) validateSchedule(field string, add addFunc) {
	if len(a.Schedule) > 0 && a.Mode == ModeOn {
		add(field+".schedule", "has no effect in mode on (the app is always on)")
	}
	for j, e := range a.Schedule {
		f := fmt.Sprintf("%s.schedule[%d]", field, j)
		if _, err := schedule.Parse(e.Cron); err != nil {
			add(f+".cron", "%q: %v", e.Cron, err)
		}
		if e.For <= 0 || e.For.D() > MaxScheduleFor {
			add(f+".for", "%s must be positive and at most %s", e.For, Duration(MaxScheduleFor))
		}
	}
}
