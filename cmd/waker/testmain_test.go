package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fum4/waker/internal/service"
)

// TestMain keeps every test off the machine's real services: the host is
// a temp root (no real unit is read or written) and the runner records
// commands instead of running them (no systemctl, no launchctl).
func TestMain(m *testing.M) {
	// A developer's own $WAKER_CONFIG must never be the file a test reads,
	// let alone the one init --force overwrites.
	os.Unsetenv("WAKER_CONFIG")
	root, err := os.MkdirTemp("", "waker-cmd-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = os.MkdirAll(filepath.Join(root, "run", "systemd", "system"), 0o755)
	home := filepath.Join(root, "home", "ana")
	currentHost = func() (service.Host, error) {
		return service.Host{OS: "linux", Home: home, User: "ana", UID: 1000, EUID: 1000, ConfigHome: filepath.Join(home, ".config"), Root: root}, nil
	}
	serviceRunner = &ran
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

// ran is the test runner: it records each command and runs none.
var ran recorder

type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) Run(argv ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, strings.Join(argv, " "))
	return "", nil
}

// take returns the commands recorded since the last take.
func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.calls
	r.calls = nil
	return c
}
