package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
)

const (
	userCgroup   = "0::/user.slice/user-1000.slice/user@1000.service/app.slice/waker.service\n"
	systemCgroup = "0::/system.slice/waker.service\n"
	reloadUser   = "systemctl --user daemon-reload"
)

// asService makes this test process look like it runs in cgroup.
func asService(t *testing.T, cgroup string) {
	t.Helper()
	prev := ownCgroup
	ownCgroup = func() string { return cgroup }
	t.Cleanup(func() { ownCgroup = prev })
}

// at sets the loop guard's clock.
func at(t *testing.T, ts time.Time) {
	t.Helper()
	prev := now
	now = func() time.Time { return ts }
	t.Cleanup(func() { now = prev })
}

// healConfig writes body with its own state_dir, installs its service on
// the fake host and returns the config's path and the unit's.
func healConfig(t *testing.T, body string) (cfgPath, unit string) {
	t.Helper()
	cfgPath = writeConfigFile(t, body+"state_dir: "+filepath.Join(t.TempDir(), "state")+"\n")
	if code, stdout, stderr := runCLI("service", "install", "--no-save", "--config", cfgPath); code != 0 {
		t.Fatalf("install = %d\n%s%s", code, stdout, stderr)
	}
	ran.take()
	h, _ := currentHost()
	sp, err := serviceSpec(load(t, cfgPath).Service, cfgPath, h)
	if err != nil {
		t.Fatal(err)
	}
	_, unit, _ = service.CheckDrift(h, sp)
	return cfgPath, unit
}

func load(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// heal edits the config with old → new, then runs serve's self-heal and
// returns whether it asked to re-exec, and its log.
func heal(t *testing.T, path, old, new string) (bool, string) {
	t.Helper()
	if old != "" {
		body := readString(t, path)
		if !strings.Contains(body, old) {
			t.Fatalf("%q not in config", old)
		}
		writeFile(t, path, strings.Replace(body, old, new, 1))
	}
	var logs bytes.Buffer
	reexec := selfHeal(load(t, path), path, slog.New(slog.NewTextHandler(&logs, nil)))
	return reexec, logs.String()
}

// drifts reports whether `waker check` still says the unit differs.
func drifts(t *testing.T, path string) bool {
	t.Helper()
	_, out, errOut := runCLI("check", "--config", path)
	return strings.Contains(out+errOut, "differs")
}

// Every service key changed, then a start: the unit says it, and serve
// re-execs exactly when the running process can't have it yet.
func TestSelfHealEachKey(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		unitLine string
		reexec   bool
	}{
		{"restart", "restart: on-failure   # crashes only", "restart: always", "Restart=always\n", false},
		{"path", "service:\n", "service:\n  path: /usr/bin:/bin\n", `Environment="PATH=/usr/bin:/bin"`, true},
		{"env", `env:     {A: "1"}`, `env:     {A: "2", B: "x"}`, `Environment="B=x"`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeHost(t, 1000)
			asService(t, userCgroup)
			path, unit := healConfig(t, commented)
			reexec, logs := heal(t, path, tc.old, tc.new)
			if reexec != tc.reexec {
				t.Errorf("reexec = %v, want %v\n%s", reexec, tc.reexec, logs)
			}
			if got := readString(t, unit); !strings.Contains(got, tc.unitLine) {
				t.Errorf("unit lacks %q:\n%s", tc.unitLine, got)
			}
			if calls := ran.take(); !slices.Equal(calls, []string{reloadUser}) {
				t.Errorf("calls = %q, want only the reload", calls)
			}
			if drifts(t, path) {
				t.Error("still drifting after the heal")
			}
		})
	}
}

func TestSelfHealSystemUnitAsRoot(t *testing.T) {
	fakeHost(t, 0)
	t.Setenv("SUDO_USER", "")
	asService(t, systemCgroup)
	path, unit := healConfig(t, strings.Replace(commented, "service:\n", "service:\n  system: true\n  path: /usr/bin:/bin\n", 1))
	reexec, logs := heal(t, path, "system: true\n", "system: true\n  user: bob\n")
	if !reexec {
		t.Errorf("a new user needs a re-exec\n%s", logs)
	}
	if got := readString(t, unit); !strings.Contains(got, "User=bob\n") {
		t.Errorf("unit:\n%s", got)
	}
	if calls := ran.take(); !slices.Equal(calls, []string{"systemctl daemon-reload"}) {
		t.Errorf("calls = %q", calls)
	}
}

func TestSelfHealLeavesTheUnitAlone(t *testing.T) {
	envChange := [2]string{`env:     {A: "1"}`, `env:     {A: "2"}`}
	tests := []struct {
		name     string
		euid     int
		cgroup   string
		body     string
		old, new string
		logWant  string
	}{
		{"run by hand", 1000, "0::/user.slice/user-1000.slice/user@1000.service/agents.slice/claude@x.service\n",
			commented, envChange[0], envChange[1], ""},
		{"app command not on the new path", 1000, userCgroup,
			commented, "service:\n", "service:\n  path: /nowhere\n", "misses app commands"},
		{"system unit as a non-root user", 1000, systemCgroup,
			strings.Replace(commented, "service:\n", "service:\n  system: true\n  path: /usr/bin:/bin\n", 1),
			envChange[0], envChange[1], "only root writes a system unit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A system unit is installed by root; the service then runs as tc.euid.
			installer := 1000
			if tc.cgroup == systemCgroup {
				installer = 0
			}
			fakeHost(t, installer)
			t.Setenv("SUDO_USER", "")
			path, unit := healConfig(t, tc.body)
			h, _ := currentHost()
			h.EUID = tc.euid
			currentHost = func() (service.Host, error) { return h, nil } // fakeHost's cleanup restores it
			asService(t, tc.cgroup)
			before := readString(t, unit)
			reexec, logs := heal(t, path, tc.old, tc.new)
			if reexec || readString(t, unit) != before {
				t.Errorf("reexec = %v, unit rewritten = %v", reexec, readString(t, unit) != before)
			}
			if calls := ran.take(); len(calls) != 0 {
				t.Errorf("ran %q", calls)
			}
			if !strings.Contains(logs, tc.logWant) || (tc.logWant != "" && !strings.Contains(logs, "waker restart")) {
				t.Errorf("logs = %q, want %q and the command", logs, tc.logWant)
			}
		})
	}
}

func TestSelfHealSkipsAUnitWakerDidntWrite(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, userCgroup)
	path, unit := healConfig(t, commented)
	writeFile(t, unit, "[Service]\nExecStart=/bin/true\n")
	if reexec, _ := heal(t, path, `env:     {A: "1"}`, `env:     {A: "2"}`); reexec {
		t.Error("re-exec for a unit waker didn't write")
	}
	if readString(t, unit) != "[Service]\nExecStart=/bin/true\n" {
		t.Error("rewrote a unit waker didn't write")
	}
}

func TestSelfHealRestartNeverLogsTheCommand(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, userCgroup)
	path, unit := healConfig(t, commented)
	reexec, logs := heal(t, path, `restart: on-failure   # crashes only`+"\n  env:     {A: \"1\"}", "restart: never\n  env: {A: \"2\"}")
	if reexec {
		t.Error("restart: never can't re-exec")
	}
	if got := readString(t, unit); !strings.Contains(got, "Restart=no\n") || !strings.Contains(got, `Environment="A=2"`) {
		t.Errorf("unit not rewritten:\n%s", got)
	}
	if !strings.Contains(logs, "restart to apply") || !strings.Contains(logs, "waker restart") {
		t.Errorf("logs = %q", logs)
	}
}

func TestSelfHealLoopGuard(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, userCgroup)
	t0 := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	at(t, t0)
	path, unit := healConfig(t, commented)
	if reexec, logs := heal(t, path, `env:     {A: "1"}`, `env:     {A: "2"}`); !reexec {
		t.Fatalf("first heal didn't re-exec\n%s", logs)
	}
	ran.take()
	// The unit goes stale again within the guard: left alone, logged.
	at(t, t0.Add(9*time.Minute))
	before := readString(t, unit)
	reexec, logs := heal(t, path, `env:     {A: "2"}`, `env:     {A: "3"}`)
	if reexec || readString(t, unit) != before || !strings.Contains(logs, "loop guard") {
		t.Errorf("guarded heal: reexec = %v, logs = %q", reexec, logs)
	}
	// Past the guard, it heals again.
	at(t, t0.Add(11*time.Minute))
	if reexec, logs := heal(t, path, "", ""); !reexec || !strings.Contains(readString(t, unit), `Environment="A=3"`) {
		t.Errorf("heal after the guard: reexec = %v\n%s", reexec, logs)
	}
	ran.take()
}

func TestServeExitsToReexec(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, userCgroup)
	path, _ := healConfig(t, commented)
	writeFile(t, path, strings.Replace(readString(t, path), `env:     {A: "1"}`, `env:     {A: "2"}`, 1))
	code, _, stderr := runCLI("serve", "--config", path)
	if code != 75 || !strings.Contains(stderr, "exiting so the supervisor restarts waker") {
		t.Fatalf("serve = %d\n%s", code, stderr)
	}
	ran.take()
	if _, err := os.Stat(filepath.Join(load(t, path).StateDir, "self-heal.json")); err != nil {
		t.Errorf("no loop-guard record: %v", err)
	}
}
