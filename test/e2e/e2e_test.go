// Package e2e builds the waker binary and a fake app binary and drives the
// whole lifecycle through real processes, sockets and the CLI.
package e2e

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type harness struct {
	t       *testing.T
	waker   string
	config  string
	listen  string
	appAddr string
	logo    []byte
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func build(t *testing.T, dir, pkg string) string {
	t.Helper()
	out := filepath.Join(dir, filepath.Base(pkg))
	cmd := exec.Command("go", "build", "-o", out, pkg)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
	return out
}

func setup(t *testing.T) *harness { return setupWith(t, func(string) string { return "" }) }

// setupWith adds extra(dir)'s lines (indented four spaces) to the app.
func setupWith(t *testing.T, extra func(dir string) string) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{t: t, listen: freeAddr(t), appAddr: freeAddr(t), logo: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)}
	h.waker = build(t, dir, "github.com/fum4/waker/cmd/waker")
	fakeapp := build(t, dir, "github.com/fum4/waker/internal/testapp/fakeapp")
	logo := filepath.Join(dir, "logo.svg")
	write(t, logo, string(h.logo))
	h.config = filepath.Join(dir, "waker.yaml")
	write(t, h.config, "state_dir: "+dir+"/state\ncontrol_socket: "+dir+"/run/control.sock\n"+
		"defaults: {branding: {logo: "+logo+", accent: '#ff3366'}}\napps:\n  - name: shop\n    listen: '"+h.listen+"'\n"+
		"    upstream: 'http://"+h.appAddr+"'\n    start: ["+fakeapp+", -listen, '"+h.appAddr+"']\n"+
		"    health: {path: /healthz, interval: 100ms}\n    idle: 2s\n"+extra(dir))
	return h
}

func (h *harness) serve() *exec.Cmd {
	h.t.Helper()
	// A file, not a buffer: the child writes it directly, with no copy goroutine to race.
	logs, err := os.CreateTemp(h.t.TempDir(), "waker-*.log")
	if err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.Command(h.waker, "serve", "--config", h.config)
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if h.t.Failed() {
			b, _ := os.ReadFile(logs.Name())
			h.t.Logf("waker logs:\n%s", b)
		}
	})
	h.eventually("the control socket answers", 10*time.Second, func() bool { _, err := h.cli("status"); return err == nil })
	return cmd
}

func (h *harness) cli(args ...string) (string, error) {
	out, err := exec.Command(h.waker, append(args, "--config", h.config)...).CombinedOutput()
	return string(out), err
}

func (h *harness) state() string {
	h.t.Helper()
	out, err := h.cli("status", "--json")
	if err != nil {
		h.t.Fatalf("status: %v\n%s", err, out)
	}
	var r struct {
		Apps []struct {
			State  string          `json:"state"`
			Memory json.RawMessage `json:"memory"`
		} `json:"apps"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil || len(r.Apps) != 1 || len(r.Apps[0].Memory) == 0 {
		h.t.Fatalf("status json %v: %s", err, out)
	}
	return r.Apps[0].State
}

// get fetches path as a browser navigation.
func (h *harness) get(path string) (int, string) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", "http://"+h.listen+path, nil)
	req.Header.Set("Accept", "text/html")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// post sends body as a program would (no Accept: text/html).
func (h *harness) post(path, body string) (int, string) {
	h.t.Helper()
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Post("http://"+h.listen+path, "text/plain", strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (h *harness) appUp() bool {
	c, err := net.DialTimeout("tcp", h.appAddr, time.Second)
	if err == nil {
		c.Close()
	}
	return err == nil
}

func (h *harness) eventually(what string, limit time.Duration, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("%s: not within %s", what, limit)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (h *harness) wakeToUp() {
	h.t.Helper()
	code, body := h.get("/")
	if code != 503 || !strings.Contains(body, "Starting the servers") || !strings.Contains(body, "--sig:#ff3366") {
		h.t.Fatalf("first visit = %d %q", code, body)
	}
	h.eventually("the app serves", 10*time.Second, func() bool { code, body := h.get("/page"); return code == 200 && body == "app:/page" })
	h.eventually("status up", 10*time.Second, func() bool { return h.state() == "up" })
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	h := setup(t)
	waker := h.serve()
	if h.state() != "down" || h.appUp() {
		t.Fatal("an on-demand app must not start until visited")
	}

	h.wakeToUp()
	// Taken before the last proxied request (/html; the logo and status
	// don't count as activity): the waker's idle clock restarts somewhere
	// inside that request, never earlier, so this is a true lower bound.
	last := time.Now()
	if _, body := h.get("/html"); !strings.Contains(body, `<script src="/__waker/heartbeat.js" defer></script></body>`) {
		t.Fatalf("no heartbeat in %q", body)
	}
	if code, body := h.get("/__waker/logo"); code != 200 || body != string(h.logo) {
		t.Fatalf("logo = %d %q", code, body)
	}
	if h.state() != "up" {
		t.Fatal("status is not up")
	}

	h.eventually("the idle stop", 6*time.Second, func() bool { return !h.appUp() })
	if took := time.Since(last); took < 2*time.Second {
		t.Fatalf("stopped %s after the last request, before its 2s idle", took)
	}
	if st := h.state(); st != "down" {
		t.Fatalf("after idle stop state = %s", st)
	}

	h.wakeToUp()
	if out, err := h.cli("mode", "shop", "off"); err != nil || !strings.Contains(out, "mode:      off (override") {
		t.Fatalf("mode off: %v %s", err, out)
	}
	h.eventually("the stop on mode off", 5*time.Second, func() bool { return !h.appUp() })
	if code, body := h.get("/"); code != 503 || !strings.Contains(body, "This app is offline") {
		t.Fatalf("offline = %d %q", code, body)
	}

	if out, err := h.cli("reset", "shop"); err != nil || !strings.Contains(out, "mode:      on-demand, idle 2s\n") {
		t.Fatalf("reset: %v %s", err, out)
	}
	// A program (no text/html) is held through the cold boot, body and all.
	if code, body := h.post("/echo", "held-body"); code != 200 || body != "held-body" {
		t.Fatalf("held POST = %d %q", code, body)
	}
	if h.state() != "up" {
		t.Fatal("status is not up after the held request")
	}

	if err := waker.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := waker.Wait(); err != nil {
		t.Fatalf("waker exit: %v", err)
	}
	if h.appUp() {
		t.Fatal("the app outlived the waker")
	}
}

func TestCustomStartingPage(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	h := setupWith(t, func(dir string) string {
		write(t, filepath.Join(dir, "starting.html"), `<html><body style="--c:{{accent}}"><img src="{{logo}}"><h1>{{name}} is waking</h1></body></html>`)
		return "    pages: {starting: starting.html}\n" // relative: resolved against the config's dir
	})
	waker := h.serve()
	code, body := h.get("/")
	want := `<html><body style="--c:#ff3366"><img src="/__waker/logo"><h1>shop is waking</h1>` +
		`<script src="/__waker/wait.js" defer></script></body></html>`
	if code != 503 || body != want {
		t.Fatalf("first visit = %d %q", code, body)
	}
	if code, body := h.get("/__waker/wait.js"); code != 200 || !strings.Contains(body, "/__waker/status") {
		t.Fatalf("wait.js = %d %q", code, body)
	}
	h.eventually("the app serves", 10*time.Second, func() bool { code, body := h.get("/page"); return code == 200 && body == "app:/page" })

	if err := waker.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := waker.Wait(); err != nil {
		t.Fatalf("waker exit: %v", err)
	}
}
