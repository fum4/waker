package main

import (
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/state"
	"github.com/fum4/waker/internal/update"
	"github.com/fum4/waker/internal/update/updatetest"
	"github.com/fum4/waker/internal/version"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// autoSetup is a 0.3.0 waker at dest with update.auto on (plus extra).
func autoSetup(t *testing.T, extra string) (*updatetest.Home, string, string, *config.Config) {
	t.Helper()
	h, dest := fakeRelease(t, "0.3.0")
	t.Setenv(update.EnvNoCheck, "")
	path := checkConfig(t, "update: {auto: true}\n"+extra)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return h, dest, path, cfg
}

func loadRec(t *testing.T, cfg *config.Config) state.Update {
	t.Helper()
	rec, err := state.LoadUpdate(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestAutoUpdateInstallsMarksAndIsDaily(t *testing.T) {
	h, dest, path, cfg := autoSetup(t, "")
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	if !autoUpdate(cfg, path, dest, quiet) {
		t.Fatal("autoUpdate = false, want a re-exec")
	}
	if readString(t, dest) != string(updatetest.Script("v0.4.0")) {
		t.Error("binary not replaced")
	}
	if m, ok := update.ReadMarker(dest); !ok || m != (update.Marker{From: "v0.3.0", To: "v0.4.0"}) {
		t.Errorf("marker = %+v, %v", m, ok)
	}
	if rec := loadRec(t, cfg); rec.Last == nil || rec.Last.Outcome != state.UpdateInstalled {
		t.Errorf("record = %+v", rec.Last)
	}
	h.Publish("v0.5.0", updatetest.Script("v0.5.0"))
	if autoUpdate(cfg, path, dest, quiet) {
		t.Error("a second attempt the same day")
	}
}

func TestAutoUpdateLeavesTheBinaryAlone(t *testing.T) {
	for name, tc := range map[string]struct {
		extra   string
		blocked []string
	}{
		"auto off": {extra: "update: {auto: false}\n"},
		"blocked":  {blocked: []string{"v0.4.0"}},
	} {
		t.Run(name, func(t *testing.T) {
			h, dest := fakeRelease(t, "0.3.0")
			h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
			body := "update: {auto: true}\n"
			if tc.extra != "" {
				body = tc.extra
			}
			path := checkConfig(t, body)
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := state.SaveUpdate(cfg.StateDir, state.Update{Blocked: tc.blocked}); err != nil {
				t.Fatal(err)
			}
			if autoUpdate(cfg, path, dest, quiet) || readString(t, dest) != string(updatetest.Script("v0.3.0")) {
				t.Error("the binary changed")
			}
		})
	}
}

// A new binary that rejects the config never runs: rolled back, blocked.
func TestAutoUpdateRejectedConfigRollsBack(t *testing.T) {
	h, dest, path, cfg := autoSetup(t, "")
	h.Publish("v0.4.0", []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo 'waker 0.4.0'; exit 0; fi\necho 'unknown key' >&2\nexit 1\n"))
	if autoUpdate(cfg, path, dest, quiet) {
		t.Fatal("re-exec into a binary that rejects the config")
	}
	if readString(t, dest) != string(updatetest.Script("v0.3.0")) {
		t.Error("old binary not restored")
	}
	if _, ok := update.ReadMarker(dest); ok {
		t.Error("marker left behind")
	}
	rec := loadRec(t, cfg)
	if rec.Last == nil || rec.Last.Outcome != state.UpdateRolledBack || !slices.Contains(rec.Blocked, "v0.4.0") {
		t.Errorf("record = %+v, blocked %q", rec.Last, rec.Blocked)
	}
}

// The new binary's first start counts itself; a second start (the first
// never reached serving) rolls back, and the old binary blocks the tag.
func TestCrashedUpdateRollsBack(t *testing.T) {
	_, dest, _, cfg := autoSetup(t, "")
	writeFile(t, dest+update.PrevSuffix, string(updatetest.Script("v0.3.0")))
	writeFile(t, dest, string(updatetest.Script("v0.4.0")))
	if err := update.WriteMarker(dest, update.Marker{From: "v0.3.0", To: "v0.4.0"}); err != nil {
		t.Fatal(err)
	}
	version.Version = "0.4.0"
	if resumeUpdate(dest, quiet) {
		t.Fatal("first start rolled back")
	}
	if m, _ := update.ReadMarker(dest); m.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", m.Attempts)
	}
	if !resumeUpdate(dest, quiet) {
		t.Fatal("second start didn't roll back")
	}
	if readString(t, dest) != string(updatetest.Script("v0.3.0")) {
		t.Error("old binary not restored")
	}
	version.Version = "0.3.0" // the re-exec'd old binary
	if resumeUpdate(dest, quiet) {
		t.Error("the old binary rolled back again")
	}
	settleUpdate(cfg, dest, quiet)
	if _, ok := update.ReadMarker(dest); ok {
		t.Error("marker left behind")
	}
	rec := loadRec(t, cfg)
	if rec.Last == nil || rec.Last.Outcome != state.UpdateRolledBack || !slices.Contains(rec.Blocked, "v0.4.0") {
		t.Errorf("record = %+v, blocked %q", rec.Last, rec.Blocked)
	}
}

func TestFinishUpdateClearsTheMarker(t *testing.T) {
	_, dest, _, cfg := autoSetup(t, "")
	version.Version = "0.4.0"
	if err := update.WriteMarker(dest, update.Marker{From: "v0.3.0", To: "v0.4.0", Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	finishUpdate(cfg, dest, quiet)
	if _, ok := update.ReadMarker(dest); ok {
		t.Error("marker left behind")
	}
	if rec := loadRec(t, cfg); rec.Last == nil || rec.Last.Outcome != state.UpdateApplied {
		t.Errorf("record = %+v", rec.Last)
	}
}

// A marker for another version (the binary was replaced by hand) is
// recorded as failed and dropped, never rolled back.
func TestStaleMarkerIsDropped(t *testing.T) {
	_, dest, _, cfg := autoSetup(t, "")
	if err := update.WriteMarker(dest, update.Marker{From: "v0.2.0", To: "v0.2.5", Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	if resumeUpdate(dest, quiet) {
		t.Fatal("rolled back over a stale marker")
	}
	settleUpdate(cfg, dest, quiet)
	if _, ok := update.ReadMarker(dest); ok {
		t.Error("marker left behind")
	}
	if rec := loadRec(t, cfg); rec.Last == nil || rec.Last.Outcome != state.UpdateFailed || len(rec.Blocked) != 0 {
		t.Errorf("record = %+v, blocked %q", rec.Last, rec.Blocked)
	}
}

func TestReexecKeepsArgs(t *testing.T) {
	var got []string
	prev := execve
	execve = func(path string, argv, env []string) error { got = append([]string{path}, argv...); return nil }
	t.Cleanup(func() { execve = prev })
	if err := reexec("/bin/waker"); err != nil || got[0] != "/bin/waker" || got[1] != "/bin/waker" {
		t.Errorf("exec %q, %v", got, err)
	}
}
