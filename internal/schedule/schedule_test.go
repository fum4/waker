package schedule

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the DST cases must not depend on the host's zoneinfo
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func mustNew(t *testing.T, loc *time.Location, ws ...Window) *Schedule {
	t.Helper()
	s, err := New(ws, loc)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAt(t *testing.T) {
	utc := time.UTC
	at := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, utc) } // 2026-11-02 is a Monday
	workday := Window{"0 9 * * 1-5", 9 * time.Hour}
	tests := []struct {
		name   string
		ws     []Window
		now    time.Time
		active bool
		until  time.Time // when active
		next   time.Time // when not
	}{
		{"before start", []Window{workday}, at(2, 8, 59), false, time.Time{}, at(2, 9, 0)},
		{"exactly at start is inside", []Window{workday}, at(2, 9, 0), true, at(2, 18, 0), time.Time{}},
		{"mid window", []Window{workday}, at(2, 12, 30), true, at(2, 18, 0), time.Time{}},
		{"exactly at end is outside", []Window{workday}, at(2, 18, 0), false, time.Time{}, at(3, 9, 0)},
		{"next start across midnight", []Window{workday}, at(2, 23, 0), false, time.Time{}, at(3, 9, 0)},
		{"next start across the weekend", []Window{workday}, at(6, 18, 30), false, time.Time{}, at(9, 9, 0)},
		{"started yesterday, still inside", []Window{{"0 22 * * *", 4 * time.Hour}}, at(3, 1, 0), true, at(3, 2, 0), time.Time{}},
		{"overlapping entries merge",
			[]Window{{"0 9 * * *", 3 * time.Hour}, {"0 11 * * *", 3 * time.Hour}}, at(2, 10, 0), true, at(2, 14, 0), time.Time{}},
		{"adjacent windows merge",
			[]Window{{"0 9 * * *", 2 * time.Hour}, {"0 11 * * *", time.Hour}}, at(2, 10, 0), true, at(2, 12, 0), time.Time{}},
		{"earliest next over entries",
			[]Window{{"0 20 * * *", time.Hour}, {"0 9 * * *", time.Hour}}, at(2, 12, 0), false, time.Time{}, at(2, 20, 0)},
		{"CRON_TZ prefix", []Window{{"CRON_TZ=Asia/Tokyo 0 9 * * *", time.Hour}}, at(2, 0, 30), true, at(2, 1, 0), time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustNew(t, utc, tt.ws...)
			st := s.At(tt.now)
			if st.Active != tt.active || s.Active(tt.now) != tt.active || !st.Until.Equal(tt.until) || !st.Next.Equal(tt.next) {
				t.Fatalf("At(%s) = %+v; want active %v until %s next %s", tt.now, st, tt.active, tt.until, tt.next)
			}
			b, ok := s.Boundary(tt.now)
			want := tt.next
			if tt.active {
				want = tt.until
			}
			if !ok || !b.Equal(want) {
				t.Fatalf("Boundary = %s, %v; want %s", b, ok, want)
			}
		})
	}
}

// Every fire of a two-hourly, two-hour window touches the next: always
// inside, with the reported end bounded by MaxChain rather than looping.
func TestEndlessChainIsBounded(t *testing.T) {
	s := mustNew(t, time.UTC, Window{"0 */2 * * *", 2 * time.Hour})
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	st := s.At(now)
	if !st.Active || st.Until.Sub(now) < 24*time.Hour || st.Until.Sub(now) > (MaxChain+1)*2*time.Hour {
		t.Fatalf("At = %+v", st)
	}
}

// Every case holds the instant a window should open, in local wall time,
// on a DST transition day.
func TestDSTLocalTime(t *testing.T) {
	ny := mustLoad(t, "America/New_York")
	buc := mustLoad(t, "Europe/Bucharest")
	tests := []struct {
		name string
		loc  *time.Location
		w    Window
		from time.Time // local midnight of the transition day
		want time.Time // the next start, local wall time
	}{
		{"spring forward 09:00 NY", ny, Window{"0 9 * * *", time.Hour},
			time.Date(2026, 3, 8, 0, 0, 0, 0, ny), time.Date(2026, 3, 8, 9, 0, 0, 0, ny)},
		{"fall back 09:00 NY", ny, Window{"0 9 * * *", time.Hour},
			time.Date(2026, 11, 1, 0, 0, 0, 0, ny), time.Date(2026, 11, 1, 9, 0, 0, 0, ny)},
		{"spring forward 09:00 Bucharest", buc, Window{"0 9 * * *", time.Hour},
			time.Date(2026, 3, 29, 0, 0, 0, 0, buc), time.Date(2026, 3, 29, 9, 0, 0, 0, buc)},
		{"fall back 09:00 Bucharest", buc, Window{"0 9 * * *", time.Hour},
			time.Date(2026, 10, 25, 0, 0, 0, 0, buc), time.Date(2026, 10, 25, 9, 0, 0, 0, buc)},
		// 02:30 doesn't exist on the spring-forward day: that day has no
		// window, the next opens 02:30 the day after (robfig/cron v3.0.1).
		{"spring forward 02:30 NY is skipped", ny, Window{"30 2 * * *", time.Hour},
			time.Date(2026, 3, 8, 0, 0, 0, 0, ny), time.Date(2026, 3, 9, 2, 30, 0, 0, ny)},
		{"CRON_TZ wins over the default zone", time.UTC, Window{"CRON_TZ=America/New_York 0 9 * * *", time.Hour},
			time.Date(2026, 3, 8, 0, 0, 0, 0, ny), time.Date(2026, 3, 8, 9, 0, 0, 0, ny)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustNew(t, tt.loc, tt.w)
			// Asked in UTC: the zone comes from the schedule, not from now.
			st := s.At(tt.from.UTC())
			if st.Active || !st.Next.Equal(tt.want) {
				t.Fatalf("next = %s; want %s", st.Next.In(tt.loc), tt.want)
			}
			if in := s.At(tt.want); !in.Active || !in.Until.Equal(tt.want.Add(time.Hour)) {
				t.Fatalf("at the start: %+v", in)
			}
		})
	}
}

// 01:30 happens twice on the fall-back day: both fires open a window, and
// the two touching hours merge into one.
func TestFallBackRepeatedHourMerges(t *testing.T) {
	ny := mustLoad(t, "America/New_York")
	s := mustNew(t, ny, Window{"30 1 * * *", time.Hour})
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC) // 01:30 EDT
	st := s.At(first)
	if !st.Active || !st.Until.Equal(first.Add(2*time.Hour)) {
		t.Fatalf("At = %+v; want until %s", st, first.Add(2*time.Hour))
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct{ spec, want string }{
		{"0 9 * *", "expected exactly 5 fields"},
		{"0 25 * * *", "above maximum"},
		{"@every 1h", "@every has no fixed fire times"},
		{"0 0 30 2 *", "never fires"},
		{"CRON_TZ=Mars/Olympus 0 9 * * *", "provided bad location"},
	}
	for _, tt := range tests {
		if _, err := Parse(tt.spec); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Parse(%q) = %v; want %q", tt.spec, err, tt.want)
		}
	}
	if _, err := Parse("@daily"); err != nil {
		t.Errorf("@daily: %v", err)
	}
}

func TestEmpty(t *testing.T) {
	var s *Schedule
	if s.Active(time.Now()) || s.At(time.Now()).Active || !s.Empty() {
		t.Fatal("nil schedule is active")
	}
	if _, ok := s.Boundary(time.Now()); ok {
		t.Fatal("nil schedule has a boundary")
	}
}
