package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
	"github.com/fum4/waker/internal/memory"
)

// renderStatus prints one app the way `waker status` shows it.
func renderStatus(w io.Writer, s app.Snapshot, now time.Time) {
	fmt.Fprintf(w, "%s — %s → %s\n", s.Name, s.Listen, s.Upstream)
	line(w, "mode", modeText(s, now))
	if s.Schedule != nil {
		line(w, "schedule", scheduleText(*s.Schedule, s.OverrideSince != nil, now))
	}
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
	if len(s.DependsOn) > 0 {
		line(w, "needs", strings.Join(s.DependsOn, ", "))
	}
	if len(s.KeptUpBy) > 0 {
		line(w, "needed by", strings.Join(s.KeptUpBy, ", ")+" · kept up, no idle stop or eviction")
	}
	prio := fmt.Sprintf("%g", s.Priority)
	if e := s.Evicted; e != nil {
		prio += fmt.Sprintf(" · last evicted by %s (p%g) %s ago", e.By, e.Priority, ago(now.Sub(e.At)))
	}
	line(w, "priority", prio)
	if s.Memory != nil {
		line(w, "memory", memoryText(*s.Memory))
	}
	hb := "injected into its pages"
	if !s.Heartbeat {
		hb = "off (heartbeat: false)"
	}
	line(w, "heartbeat", hb)
}

// renderPolicy prints the effective resource policy above the apps.
func renderPolicy(w io.Writer, p control.Policy) {
	avail := "unknown"
	if p.AvailableMiB != nil {
		avail = fmt.Sprintf("%d MiB", *p.AvailableMiB)
	}
	fmt.Fprintf(w, "memory policy — headroom %s · reserve %d MiB · eviction %s · available %s\n",
		p.Headroom, p.ReserveMiB, p.Eviction, avail)
}

// memoryText: need N + headroom(<from>) H [+ reserve R] = X MiB required to boot (…).
func memoryText(m memory.Breakdown) string {
	text := fmt.Sprintf("need %d + headroom(%s) %d", m.NeedMiB, m.HeadroomFrom, m.HeadroomMiB)
	if m.ReserveMiB > 0 {
		text += fmt.Sprintf(" + reserve %d", m.ReserveMiB)
	}
	text += fmt.Sprintf(" = %d MiB required to boot", m.RequiredMiB)
	if m.DeclaredMiB == nil && m.LearnedMiB == nil {
		return text + " (need: the default, nothing declared or learned)"
	}
	return text + fmt.Sprintf(" (need = max(declared %s, learned %s))", orNone(m.DeclaredMiB), orNone(m.LearnedMiB))
}

func orNone(v *int64) string {
	if v == nil {
		return "none"
	}
	return fmt.Sprintf("%d", *v)
}

func modeText(s app.Snapshot, now time.Time) string {
	text := withIdle(s.Mode, s.Idle)
	switch {
	case s.OverrideSince != nil:
		text += fmt.Sprintf(" (override, set %s ago; declared %s)", ago(now.Sub(*s.OverrideSince)), withIdle(s.DeclaredMode, s.DeclaredIdle))
	case s.Schedule != nil && s.Schedule.Active:
		text += fmt.Sprintf(" (schedule; declared %s)", withIdle(s.DeclaredMode, s.DeclaredIdle))
	}
	return text
}

// scheduleText: "on until 18:00 (window 0 9 * * 1-5 for 9h)" or
// "next window Mon 09:00 (…)"; an override outranking the window says so.
func scheduleText(sc app.ScheduleStatus, overridden bool, now time.Time) string {
	var text string
	switch {
	case sc.Active && sc.Until != nil:
		text = "on until " + clockText(*sc.Until, now)
	case sc.Next != nil:
		text = "next window " + clockText(*sc.Next, now)
	default:
		return "no window ahead"
	}
	if w := sc.Window; w != nil {
		text += fmt.Sprintf(" (window %s for %s)", w.Cron, compactDuration(w.For.D()))
	}
	if overridden {
		text += " · the override wins"
	}
	return text
}

// clockText shows t in now's zone: 18:00 today, Mon 09:00 this week, else
// Jan 2 09:00.
func clockText(t, now time.Time) string {
	t = t.In(now.Location())
	switch y, m, d := t.Date(); {
	case y == now.Year() && m == now.Month() && d == now.Day():
		return t.Format("15:04")
	case t.Sub(now) < 6*24*time.Hour:
		return t.Format("Mon 15:04")
	default:
		return t.Format("Jan 2 15:04")
	}
}

// compactDuration drops time.Duration's zero tails: 9h, 1h30m, 45s.
func compactDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
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
