package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunUsage(t *testing.T) {
	tests := []struct {
		args     []string
		wantCode int
		wantErr  string
	}{
		{nil, 2, "usage: waker"},
		{[]string{"--help"}, 0, "usage: waker"},
		{[]string{"frobnicate"}, 2, `unknown command "frobnicate"`},
		{[]string{"version", "extra"}, 2, "takes no arguments"},
	}
	for _, tt := range tests {
		code, _, stderr := runCLI(tt.args...)
		if code != tt.wantCode || !strings.Contains(stderr, tt.wantErr) {
			t.Errorf("run(%q) = %d, %q; want %d, %q", tt.args, code, stderr, tt.wantCode, tt.wantErr)
		}
	}
}

func TestVersion(t *testing.T) {
	code, stdout, _ := runCLI("version")
	if code != 0 || !strings.HasPrefix(stdout, "waker ") {
		t.Fatalf("version = %d, %q", code, stdout)
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	bad := filepath.Join(dir, "bad.yaml")
	writeFile(t, good, "apps:\n  - {name: web, listen: '127.0.0.1:8080', upstream: 'http://127.0.0.1:3000', start: [./serve]}\n")
	writeFile(t, bad, "apps:\n  - {name: web, listen: '127.0.0.1:8080', upstream: 'http://127.0.0.1:3000', start: [./serve], priority: 12}\n")

	code, stdout, _ := runCLI("check", "--config", good)
	if code != 0 || !strings.Contains(stdout, "ok — 1 app(s)") || !strings.Contains(stdout, "on-demand") {
		t.Fatalf("check good = %d, %q", code, stdout)
	}
	code, _, stderr := runCLI("check", "--config", bad)
	if code != 1 || !strings.Contains(stderr, "apps[0] (web).priority") {
		t.Fatalf("check bad = %d, %q", code, stderr)
	}
	t.Setenv("WAKER_CONFIG", good)
	if code, _, _ := runCLI("check"); code != 0 {
		t.Fatalf("check via WAKER_CONFIG = %d", code)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
