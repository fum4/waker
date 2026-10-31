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
	"github.com/fum4/waker/internal/state"
)

var (
	userHeal   = []string{"systemctl --user daemon-reload", "systemctl --user enable waker.service"}
	systemHeal = []string{"systemctl daemon-reload", "systemctl enable waker.service"}
	envChange  = [2]string{`env:     {A: "1"}`, `env:     {A: "2"}`}
)

// asService makes systemd report pid as the service's main process (this
// test process: serve is the service).
func asService(t *testing.T, pid int) { ran.setMainPID(t, pid) }

// at sets the loop guard's clock.
func at(t *testing.T, ts time.Time) {
	t.Helper()
	prev := now
	now = func() time.Time { return ts }
	t.Cleanup(func() { now = prev })
}

// changes is the recorded commands that change something (not the
// `systemctl show` that finds the main process).
func changes() []string {
	var out []string
	for _, c := range ran.take() {
		if !strings.Contains(c, " show ") {
			out = append(out, c)
		}
	}
	return out
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

// heal edits the config with old → new (none when old is empty), runs
// serve's self-heal and returns what it did and its log.
func heal(t *testing.T, path, old, new string) (healResult, string) {
	t.Helper()
	if old != "" {
		body := readString(t, path)
		if !strings.Contains(body, old) {
			t.Fatalf("%q not in config", old)
		}
		writeFile(t, path, strings.Replace(body, old, new, 1))
	}
	var logs bytes.Buffer
	res := selfHeal(load(t, path), path, slog.New(slog.NewTextHandler(&logs, nil)))
	return res, logs.String()
}

// drifts reports whether `waker check` still says the unit differs.
func drifts(t *testing.T, path string) bool {
	t.Helper()
	_, out, errOut := runCLI("check", "--config", path)
	ran.take()
	return strings.Contains(out+errOut, "differs")
}

// Every service key changed, then a start: the unit says it, and serve
// re-execs exactly when the running process can't have it yet.
func TestSelfHealEachKey(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		unitLine string
		want     healResult
	}{
		{"restart", "restart: on-failure   # crashes only", "restart: always", "Restart=always\n", healLogged},
		{"path", "service:\n", "service:\n  path: /usr/bin:/bin\n", `Environment="PATH=/usr/bin:/bin"`, healReexec},
		{"env", envChange[0], `env:     {A: "2", B: "x"}`, `Environment="B=x"`, healReexec},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeHost(t, 1000)
			asService(t, os.Getpid())
			path, unit := healConfig(t, commented)
			res, logs := heal(t, path, tc.old, tc.new)
			if res != tc.want {
				t.Errorf("result = %v, want %v\n%s", res, tc.want, logs)
			}
			if got := readString(t, unit); !strings.Contains(got, tc.unitLine) {
				t.Errorf("unit lacks %q:\n%s", tc.unitLine, got)
			}
			if calls := changes(); !slices.Equal(calls, userHeal) {
				t.Errorf("calls = %q, want %q", calls, userHeal)
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
	asService(t, os.Getpid())
	path, unit := healConfig(t, strings.Replace(commented, "service:\n", "service:\n  system: true\n  path: /usr/bin:/bin\n", 1))
	res, logs := heal(t, path, "system: true\n", "system: true\n  user: bob\n")
	if res != healReexec {
		t.Errorf("a new user needs a re-exec, got %v\n%s", res, logs)
	}
	if got := readString(t, unit); !strings.Contains(got, "User=bob\n") {
		t.Errorf("unit:\n%s", got)
	}
	if calls := changes(); !slices.Equal(calls, systemHeal) {
		t.Errorf("calls = %q", calls)
	}
}

func TestSelfHealLeavesTheUnitAlone(t *testing.T) {
	system := strings.Replace(commented, "service:\n", "service:\n  system: true\n  path: /usr/bin:/bin\n", 1)
	tests := []struct {
		name     string
		system   bool
		euid     int
		pid      int
		body     string
		old, new string
		logWant  string
	}{
		{"run by hand", false, 1000, 1, commented, envChange[0], envChange[1], ""},
		{"app command lost from the new path", false, 1000, os.Getpid(),
			commented, "service:\n", "service:\n  path: /nowhere\n", "loses app commands"},
		{"system unit as a non-root user", true, 1000, os.Getpid(),
			system, envChange[0], envChange[1], "only root writes a system unit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A system unit is installed by root; the service then runs as tc.euid.
			installer := 1000
			if tc.system {
				installer = 0
			}
			fakeHost(t, installer)
			t.Setenv("SUDO_USER", "")
			path, unit := healConfig(t, tc.body)
			h, _ := currentHost()
			h.EUID = tc.euid
			currentHost = func() (service.Host, error) { return h, nil } // fakeHost's cleanup restores it
			asService(t, tc.pid)
			before := readString(t, unit)
			res, logs := heal(t, path, tc.old, tc.new)
			if res == healReexec || readString(t, unit) != before {
				t.Errorf("result = %v, unit rewritten = %v", res, readString(t, unit) != before)
			}
			if calls := changes(); len(calls) != 0 {
				t.Errorf("ran %q", calls)
			}
			if tc.logWant == "" {
				if res != healNone || logs != "" {
					t.Errorf("result = %v, logs = %q; want none (serve's drift warning covers it)", res, logs)
				}
			} else if !strings.Contains(logs, tc.logWant) || !strings.Contains(logs, "waker restart") {
				t.Errorf("logs = %q, want %q and the command", logs, tc.logWant)
			}
		})
	}
}

// A command missing under the old PATH too doesn't block an unrelated edit.
func TestSelfHealIgnoresACommandAlreadyMissing(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, os.Getpid())
	path, unit := healConfig(t, strings.Replace(commented, "start: [sh]", "start: [no-such-waker-cmd]", 1))
	if res, logs := heal(t, path, envChange[0], envChange[1]); res != healReexec {
		t.Errorf("result = %v\n%s", res, logs)
	}
	if !strings.Contains(readString(t, unit), `Environment="A=2"`) {
		t.Error("unit not rewritten")
	}
	ran.take()
}

func TestSelfHealSkipsAUnitWakerDidntWrite(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, os.Getpid())
	path, unit := healConfig(t, commented)
	writeFile(t, unit, "[Service]\nExecStart=/bin/true\n")
	if res, _ := heal(t, path, envChange[0], envChange[1]); res != healNone {
		t.Errorf("result = %v for a unit waker didn't write", res)
	}
	if readString(t, unit) != "[Service]\nExecStart=/bin/true\n" {
		t.Error("rewrote a unit waker didn't write")
	}
	ran.take()
}

func TestSelfHealRestartNeverLogsTheCommand(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, os.Getpid())
	path, unit := healConfig(t, commented)
	res, logs := heal(t, path, `restart: on-failure   # crashes only`+"\n  "+envChange[0], "restart: never\n  "+envChange[1])
	if res != healLogged {
		t.Errorf("result = %v: restart: never can't re-exec", res)
	}
	if got := readString(t, unit); !strings.Contains(got, "Restart=no\n") || !strings.Contains(got, `Environment="A=2"`) {
		t.Errorf("unit not rewritten:\n%s", got)
	}
	if !strings.Contains(logs, "restart to apply") || !strings.Contains(logs, "waker restart") {
		t.Errorf("logs = %q", logs)
	}
	ran.take()
}

func TestSelfHealPutsTheOldUnitBackOnFailure(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, os.Getpid())
	path, unit := healConfig(t, commented)
	before := readString(t, unit)
	ran.fail = "enable"
	t.Cleanup(func() { ran.fail = "" })
	res, logs := heal(t, path, envChange[0], envChange[1])
	if res != healLogged || !strings.Contains(logs, "the old unit is back") {
		t.Errorf("result = %v, logs = %q", res, logs)
	}
	if readString(t, unit) != before {
		t.Error("the failed heal left the new unit")
	}
	if calls := changes(); calls[len(calls)-1] != "systemctl --user daemon-reload" {
		t.Errorf("calls = %q, want a reload after the restore", calls)
	}
	ran.fail = ""
	if !drifts(t, path) {
		t.Error("the drift is no longer reported")
	}
}

func TestSelfHealLoopGuard(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, os.Getpid())
	t0 := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	at(t, t0)
	path, unit := healConfig(t, commented)
	if res, logs := heal(t, path, envChange[0], envChange[1]); res != healReexec {
		t.Fatalf("first heal = %v\n%s", res, logs)
	}
	// The unit goes stale again within the guard: left alone, logged.
	at(t, t0.Add(9*time.Minute))
	before := readString(t, unit)
	res, logs := heal(t, path, envChange[1], `env:     {A: "3"}`)
	if res != healLogged || readString(t, unit) != before || !strings.Contains(logs, "loop guard") {
		t.Errorf("guarded heal: result = %v, logs = %q", res, logs)
	}
	// Past the guard, it heals again.
	at(t, t0.Add(11*time.Minute))
	if res, logs := heal(t, path, "", ""); res != healReexec || !strings.Contains(readString(t, unit), `Environment="A=3"`) {
		t.Errorf("heal after the guard: result = %v\n%s", res, logs)
	}
	ran.take()
}

// A last heal in the future (the clock stepped back) doesn't block one.
func TestSelfHealLoopGuardIgnoresAFutureHeal(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, os.Getpid())
	t0 := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	at(t, t0)
	path, _ := healConfig(t, commented)
	if err := state.SaveSelfHeal(load(t, path).StateDir, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if res, logs := heal(t, path, envChange[0], envChange[1]); res != healReexec {
		t.Errorf("result = %v\n%s", res, logs)
	}
	ran.take()
}

func TestServeExitsToReexec(t *testing.T) {
	fakeHost(t, 1000)
	asService(t, os.Getpid())
	path, _ := healConfig(t, commented)
	writeFile(t, path, strings.Replace(readString(t, path), envChange[0], envChange[1], 1))
	code, _, stderr := runCLI("serve", "--config", path)
	if code != 75 || !strings.Contains(stderr, "exiting so the supervisor restarts waker") {
		t.Fatalf("serve = %d\n%s", code, stderr)
	}
	ran.take()
	if _, err := os.Stat(filepath.Join(load(t, path).StateDir, state.SelfHealFile)); err != nil {
		t.Errorf("no loop-guard record: %v", err)
	}
}
