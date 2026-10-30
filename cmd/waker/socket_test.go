package main

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/control"
)

// fakeSystemDaemon serves an empty status report on a temp socket and
// points the CLI's system-socket fallback at it.
func fakeSystemDaemon(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "control.sock")
	ln, err := control.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"policy":{},"apps":[]}`))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	prev := systemSocket
	systemSocket = sock
	t.Cleanup(func() { systemSocket = prev })
	return sock
}

const oneApp = "apps:\n  - {name: web, listen: '127.0.0.1:20080', upstream: 'http://127.0.0.1:20081', start: [./serve]}\n"

// A --system daemon listens on its RuntimeDirectory socket; a login shell
// (its own XDG_RUNTIME_DIR, nothing listening there) falls back to it.
func TestStatusFallsBackToTheSystemSocket(t *testing.T) {
	sock := fakeSystemDaemon(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("WAKER_CONTROL_SOCKET", "")
	cfg := filepath.Join(t.TempDir(), "waker.yaml")
	writeFile(t, cfg, oneApp)
	code, stdout, stderr := runCLI("status", "--config", cfg)
	if code != 0 || !strings.HasSuffix(stdout, "via "+sock+"\n") {
		t.Fatalf("= %d\n%s%s", code, stdout, stderr)
	}
}

func TestStatusPrefersTheUserSocket(t *testing.T) {
	fakeSystemDaemon(t)
	user := fakeSystemDaemon(t) // a second daemon, standing in for the user's
	systemSocket = filepath.Join(t.TempDir(), "nothing.sock")
	t.Setenv("WAKER_CONTROL_SOCKET", user)
	cfg := filepath.Join(t.TempDir(), "waker.yaml")
	writeFile(t, cfg, oneApp)
	if code, stdout, _ := runCLI("status", "--config", cfg); code != 0 || !strings.HasSuffix(stdout, "via "+user+"\n") {
		t.Fatalf("= %d %q", code, stdout)
	}
}

// A control_socket written in the file is never second-guessed.
func TestAnExplicitControlSocketNeverFallsBack(t *testing.T) {
	fakeSystemDaemon(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "waker.yaml")
	writeFile(t, cfg, "control_socket: "+dir+"/none.sock\n"+oneApp)
	code, _, stderr := runCLI("status", "--config", cfg)
	if code != 1 || !strings.Contains(stderr, "none.sock") {
		t.Fatalf("= %d %q", code, stderr)
	}
}

// The real system socket path, so the unit and the CLI agree.
func TestSystemSocketIsTheUnitsRuntimeDirectory(t *testing.T) {
	if systemSocket != "/run/waker/control.sock" {
		t.Fatalf("systemSocket = %s", systemSocket)
	}
}
