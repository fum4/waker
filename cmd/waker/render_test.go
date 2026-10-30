package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
	"github.com/fum4/waker/internal/memory"
)

func TestRenderStatus(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	since := now.Add(-3 * time.Minute)
	idleIn := config.Duration(9 * time.Second)
	s := app.Snapshot{
		Name: "shop", Listen: "127.0.0.1:8080", Upstream: "http://127.0.0.1:3000",
		Mode: config.ModeOnDemand, Idle: config.Duration(10 * time.Second),
		DeclaredMode: config.ModeOff, DeclaredIdle: config.Duration(30 * time.Minute),
		OverrideSince: &since, State: app.Up, Priority: 7.5, Heartbeat: true,
		LastActivity: now.Add(-time.Second), IdleIn: &idleIn,
		Evicted: &app.Eviction{By: "top", Priority: 10, At: now.Add(-2 * time.Hour)},
	}
	var buf bytes.Buffer
	renderStatus(&buf, s, now)
	for _, want := range []string{
		"shop — 127.0.0.1:8080 → http://127.0.0.1:3000\n",
		"  mode:      on-demand, idle 10s (override, set 3m ago; declared off)\n",
		"  state:     up\n",
		"  activity:  1s ago · idle stop in 9s\n",
		"  priority:  7.5 · last evicted by top (p10) 2h ago\n",
		"  heartbeat: injected into its pages\n",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in:\n%s", want, buf.String())
		}
	}
	s.Heartbeat, s.State, s.Error, s.OverrideSince = false, app.Failed, "start command exited: exit status 1", nil
	buf.Reset()
	renderStatus(&buf, s, now)
	for _, want := range []string{"state:     failed — start command exited", "heartbeat: off (heartbeat: false)", "mode:      on-demand, idle 10s\n"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in:\n%s", want, buf.String())
		}
	}
}

func TestRenderDependencies(t *testing.T) {
	tests := []struct {
		name      string
		deps, by  []string
		want, not []string
	}{
		{"none", nil, nil, nil, []string{"needs:", "needed by:"}},
		{"a dependent", []string{"db", "cache"}, nil, []string{"  needs:     db, cache\n"}, []string{"needed by:"}},
		{"a dependency kept up", nil, []string{"api", "web"}, []string{"  needed by: api, web · kept up, no idle stop or eviction\n"}, []string{"needs:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderStatus(&buf, app.Snapshot{Name: "db", State: app.Up, DependsOn: tt.deps, KeptUpBy: tt.by}, time.Now())
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

func TestMemoryAndPolicyLines(t *testing.T) {
	learned, declared := int64(900), int64(800)
	tests := []struct {
		m    memory.Breakdown
		want string
	}{
		{memory.Breakdown{NeedMiB: 1024, HeadroomMiB: 2844, HeadroomFrom: "linear p5", RequiredMiB: 3868},
			"need 1024 + headroom(linear p5) 2844 = 3868 MiB required to boot (need: the default, nothing declared or learned)"},
		{memory.Breakdown{DeclaredMiB: &declared, LearnedMiB: &learned, NeedMiB: 900, HeadroomMiB: 100, HeadroomFrom: "app", ReserveMiB: 256, RequiredMiB: 1256},
			"need 900 + headroom(app) 100 + reserve 256 = 1256 MiB required to boot (need = max(declared 800, learned 900))"},
		{memory.Breakdown{LearnedMiB: &learned, NeedMiB: 900, HeadroomMiB: 1422, HeadroomFrom: "linear p7.5", RequiredMiB: 2322},
			"(need = max(declared none, learned 900))"},
	}
	for _, tt := range tests {
		if got := memoryText(tt.m); !strings.Contains(got, tt.want) {
			t.Errorf("memoryText = %q, want %q", got, tt.want)
		}
	}
	var buf bytes.Buffer
	avail := int64(8000)
	renderPolicy(&buf, control.Policy{Headroom: "linear max 5GiB, min 512MiB", Eviction: "lower-priority, priority-then-idle", AvailableMiB: &avail})
	if want := "memory policy — headroom linear max 5GiB, min 512MiB · reserve 0 MiB · eviction lower-priority, priority-then-idle · available 8000 MiB\n"; buf.String() != want {
		t.Fatalf("policy = %q", buf.String())
	}
}

func TestAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Second: "0s", 89 * time.Second: "89s", 90 * time.Second: "1m",
		89 * time.Minute: "89m", 90 * time.Minute: "1h", 47 * time.Hour: "47h", 48 * time.Hour: "2d",
	} {
		if got := ago(d); got != want {
			t.Errorf("ago(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestStatusWithoutAWaker(t *testing.T) {
	dir := t.TempDir()
	cfg := dir + "/waker.yaml"
	writeFile(t, cfg, "control_socket: "+dir+"/none.sock\napps:\n  - {name: web, listen: '127.0.0.1:8080', upstream: 'http://127.0.0.1:3000', start: [./serve]}\n")
	code, _, stderr := runCLI("status", "--config", cfg)
	if code != 1 || !strings.Contains(stderr, "isn't running") {
		t.Fatalf("= %d %q", code, stderr)
	}
	if code, _, _ := runCLI("mode", "web", "--config", cfg); code != 2 {
		t.Fatalf("mode with one arg = %d", code)
	}
}

func TestOffWarning(t *testing.T) {
	tests := []struct {
		name string
		snap app.Snapshot
		want string
	}{
		{"off, dependents up", app.Snapshot{Name: "db", Mode: config.ModeOff, KeptUpBy: []string{"api", "web"}}, "warning: db is off while api, web (up) still need it"},
		{"off, no dependents", app.Snapshot{Name: "db", Mode: config.ModeOff}, ""},
		{"on-demand, dependents up", app.Snapshot{Name: "db", Mode: config.ModeOnDemand, KeptUpBy: []string{"api"}}, ""},
	}
	for _, tt := range tests {
		if got := offWarning(tt.snap); got != tt.want {
			t.Errorf("%s: offWarning = %q, want %q", tt.name, got, tt.want)
		}
	}
}
