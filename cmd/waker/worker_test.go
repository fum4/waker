package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

func TestWorkerCheckAndEffectiveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "waker.yaml")
	writeFile(t, path, "apps: [{name: jobs, mode: on, start: [true], health: {command: [true]}}]\n")
	code, stdout, stderr := runCLI("check", "--config", path)
	if code != 0 || !strings.Contains(stdout, "- (worker)") || !strings.Contains(stdout, "ok — 1 app(s)") {
		t.Fatalf("check = %d, %s, %s", code, stdout, stderr)
	}
	code, stdout, stderr = runCLI("check", "--effective", "--config", path)
	resolved, err := config.Parse([]byte(stdout))
	if code != 0 || err != nil {
		t.Fatalf("effective = %d, %s, %s", code, stdout, stderr)
	}
	if len(resolved.Apps) != 1 || !resolved.Apps[0].Worker() || resolved.Apps[0].Health.Path != "" || resolved.Apps[0].HeartbeatEnabled() {
		t.Fatalf("worker effective config includes HTTP behavior: %s", stdout)
	}
}

func TestWorkerStatusHasNoFakeHTTPDetails(t *testing.T) {
	var output bytes.Buffer
	renderStatus(&output, app.Snapshot{Name: "jobs", Mode: config.ModeOn, DeclaredMode: config.ModeOn, State: app.Up}, time.Now())
	text := output.String()
	if !strings.HasPrefix(text, "jobs — worker\n") || !strings.Contains(text, "state:     up") {
		t.Fatalf("status = %s", text)
	}
	for _, absent := range []string{"→", "heartbeat:", "activity:", "http://"} {
		if strings.Contains(text, absent) {
			t.Fatalf("worker status contains %q: %s", absent, text)
		}
	}
}
