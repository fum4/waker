package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

// Starting inside a window boots a declared-off app at once, the way Run
// boots mode-on apps.
func TestStartupInsideAWindowBoots(t *testing.T) {
	cfg, _ := newConfig(t, "")
	cfg.Apps[0].Mode = config.ModeOff
	// Every minute for a week: inside a window whenever the test runs.
	cfg.Apps[0].Schedule = []config.ScheduleEntry{{Cron: "* * * * *", For: config.Duration(config.MaxScheduleFor)}}
	d, c := start(t, cfg)
	ctx := context.Background()
	waitFor(t, 5*time.Second, func() bool { return d.all[0].State(ctx) == app.Up })
	r, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Apps[0]
	if s.Mode != config.ModeOn || s.DeclaredMode != config.ModeOff || s.Schedule == nil || !s.Schedule.Active || s.Schedule.Until == nil {
		t.Fatalf("snapshot = %+v, schedule %+v", s, s.Schedule)
	}
}
