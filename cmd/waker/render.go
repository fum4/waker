package main

import (
	"fmt"
	"io"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

// renderStatus prints one app the way `waker status` shows it.
func renderStatus(w io.Writer, s app.Snapshot, now time.Time) {
	fmt.Fprintf(w, "%s — %s → %s\n", s.Name, s.Listen, s.Upstream)
	line(w, "mode", modeText(s, now))
	st := string(s.State)
	if s.Error != "" {
		st += " — " + s.Error
	}
	line(w, "state", st)
	act := ago(now.Sub(s.LastActivity)) + " ago"
	if s.Inflight > 0 {
		act += fmt.Sprintf(" · %d in flight", s.Inflight)
	}
	if s.IdleIn != nil {
		act += " · idle stop in " + ago(s.IdleIn.D())
	}
	line(w, "activity", act)
	prio := fmt.Sprintf("%d", s.Priority)
	if e := s.Evicted; e != nil {
		prio += fmt.Sprintf(" · last evicted by %s (p%d) %s ago", e.By, e.Priority, ago(now.Sub(e.At)))
	}
	line(w, "priority", prio)
	hb := "injected into its pages"
	if !s.Heartbeat {
		hb = "off (heartbeat: false)"
	}
	line(w, "heartbeat", hb)
}

func modeText(s app.Snapshot, now time.Time) string {
	text := withIdle(s.Mode, s.Idle)
	if s.OverrideSince != nil {
		text += fmt.Sprintf(" (override, set %s ago; declared %s)", ago(now.Sub(*s.OverrideSince)), withIdle(s.DeclaredMode, s.DeclaredIdle))
	}
	return text
}

func withIdle(m config.Mode, idle config.Duration) string {
	if m == config.ModeOnDemand {
		return fmt.Sprintf("%s, idle %s", m, idle)
	}
	return string(m)
}

func line(w io.Writer, label, value string) {
	fmt.Fprintf(w, "  %-11s%s\n", label+":", value)
}

// ago renders a duration coarsely: 42s, 17m, 5h, 3d.
func ago(d time.Duration) string {
	s := int(max(0, d).Seconds())
	switch {
	case s < 90:
		return fmt.Sprintf("%ds", s)
	case s < 5400:
		return fmt.Sprintf("%dm", s/60)
	case s < 172800:
		return fmt.Sprintf("%dh", s/3600)
	default:
		return fmt.Sprintf("%dd", s/86400)
	}
}
