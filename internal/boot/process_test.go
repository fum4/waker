package boot

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

func testApp(t *testing.T, start ...string) config.App {
	t.Helper()
	cfg, err := config.Parse([]byte("apps: [{name: t, listen: '127.0.0.1:1', upstream: 'http://127.0.0.1:1', start: [x]}]"))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Apps[0]
	a.Start = start
	a.Cwd = t.TempDir()
	a.StopTimeout = config.Duration(2 * time.Second)
	return a
}

func TestForegroundStartIsStoppedBySignallingItsGroup(t *testing.T) {
	p := New(testApp(t, "sh", "-c", "sleep 30 & sleep 30"))
	exit, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}
	if p.PID() == 0 {
		t.Fatal("no pid while running")
	}
	start := time.Now()
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("SIGTERM should end sleep at once, took %v", time.Since(start))
	}
	if err := <-exit; err == nil {
		t.Fatal("a signalled start command should report a non-nil exit")
	}
	if p.PID() != 0 {
		t.Fatal("pid after stop")
	}
}

func TestStartReportsEarlyExit(t *testing.T) {
	p := New(testApp(t, "sh", "-c", "exit 3"))
	exit, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-exit; err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("exit = %v", err)
	}
}

func TestStartOfAMissingCommandFails(t *testing.T) {
	if _, err := New(testApp(t, "/nonexistent/waker-app")).Start(); err == nil {
		t.Fatal("want an error")
	}
}

func TestStopCommandRunsInTheAppCwdWithItsEnv(t *testing.T) {
	a := testApp(t, "true")
	a.Env = map[string]string{"WAKER_TEST": "hello"}
	a.Stop = []string{"sh", "-c", "echo $WAKER_TEST > stopped"}
	p := New(a)
	if err := p.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(a.Cwd, "stopped"))
	if err != nil || strings.TrimSpace(string(got)) != "hello" {
		t.Fatalf("stop output = %q, %v", got, err)
	}
}

func TestRunTimesOut(t *testing.T) {
	p := New(testApp(t, "true"))
	err := p.Run(context.Background(), []string{"sleep", "30"}, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("Run = %v", err)
	}
}

func TestHealthHTTP(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{{200, true}, {302, true}, {399, true}, {404, false}, {503, false}}
	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				t.Errorf("probed %s", r.URL.Path)
			}
			if tt.status == 302 {
				w.Header().Set("Location", "/elsewhere")
			}
			w.WriteHeader(tt.status)
		}))
		a := testApp(t, "true")
		a.Upstream, a.Health.Path = srv.URL, "/healthz"
		if got := New(a).Healthy(context.Background()); got != tt.want {
			t.Errorf("status %d: healthy = %v, want %v", tt.status, got, tt.want)
		}
		srv.Close()
	}
}

func TestHealthOfANonHTTPAnswerIsDownNotACrash(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("garbage\r\n\r\n"))
			c.Close()
		}
	}()
	a := testApp(t, "true")
	a.Upstream = "http://" + ln.Addr().String()
	if New(a).Healthy(context.Background()) {
		t.Fatal("garbage counted as healthy")
	}
}

func TestHealthRefusedIsDown(t *testing.T) {
	a := testApp(t, "true")
	a.Upstream = "http://127.0.0.1:1"
	if New(a).Healthy(context.Background()) {
		t.Fatal("refused counted as healthy")
	}
}

func TestHealthCommand(t *testing.T) {
	a := testApp(t, "true")
	a.Health = config.Health{Command: []string{"true"}, Timeout: config.Duration(time.Second)}
	if !New(a).Healthy(context.Background()) {
		t.Fatal("true is unhealthy")
	}
	a.Health.Command = []string{"false"}
	if New(a).Healthy(context.Background()) {
		t.Fatal("false is healthy")
	}
}
