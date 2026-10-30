package proxy

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

type holdResult struct {
	status int
	ctype  string
	body   string
}

// slowLive is an app whose boot ends only when the returned func is called.
func slowLive(t *testing.T, mutate func(*config.App)) (*live, func()) {
	t.Helper()
	ready := make(chan struct{})
	l := newLiveProc(t, mutate, func(p *liveProcess) { p.ready = ready })
	var closed bool
	release := func() {
		if !closed {
			closed = true
			close(ready)
		}
	}
	t.Cleanup(release)
	return l, release
}

func (l *live) async(t *testing.T, method, path, body string, hdr ...string) <-chan holdResult {
	out := make(chan holdResult, 1)
	go func() {
		var rd *strings.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		var resp *http.Response
		var got string
		if rd != nil {
			resp, got = l.do(t, method, path, rd, hdr...)
		} else {
			resp, got = l.do(t, method, path, nil, hdr...)
		}
		out <- holdResult{resp.StatusCode, resp.Header.Get("Content-Type"), got}
	}()
	return out
}

// waitInflight waits until n requests are in flight (held or forwarded).
func (l *live) waitInflight(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for l.app.Activity().Inflight() != n {
		if time.Now().After(deadline) {
			t.Fatalf("inflight = %d, want %d", l.app.Activity().Inflight(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHeldThroughAColdBoot(t *testing.T) {
	hold := func(a *config.App) { a.Waiting = config.WaitingHold }
	tests := []struct {
		name   string
		mutate func(*config.App)
		method string
		path   string
		body   string
		hdr    []string
		want   string
	}{
		{"program GET", nil, "GET", "/x?q=1", "", nil, "app:/x?q=1"},
		{"program POST keeps its body", nil, "POST", "/echo", "payload", nil, "payload"},
		{"program chunked POST", nil, "POST", "/echo", "chunk", []string{"Transfer-Encoding", "chunked"}, "chunk"},
		{"browser, waiting: hold", hold, "GET", "/x", "", []string{"Accept", htmlAccept}, "app:/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, release := slowLive(t, tt.mutate)
			res := l.async(t, tt.method, tt.path, tt.body, tt.hdr...)
			// Held, and counted in flight: no idle stop, no eviction under it.
			l.waitInflight(t, 1)
			select {
			case r := <-res:
				t.Fatalf("answered before the app was up: %+v", r)
			case <-time.After(50 * time.Millisecond):
			}
			release()
			r := <-res
			if r.status != http.StatusOK || r.body != tt.want {
				t.Fatalf("held request = %+v, want 200 %q", r, tt.want)
			}
			if n := l.proc.starts.Load(); n != 1 {
				t.Fatalf("starts = %d", n)
			}
		})
	}
}

func TestBrowserGetsTheStartingPageByDefault(t *testing.T) {
	l, _ := slowLive(t, nil)
	r := <-l.async(t, "GET", "/", "", "Accept", htmlAccept)
	if r.status != http.StatusServiceUnavailable || !strings.Contains(r.body, "Powering up the servers") {
		t.Fatalf("= %+v", r)
	}
}

func TestHoldTimeout(t *testing.T) {
	short := func(w config.Waiting) func(*config.App) {
		return func(a *config.App) { a.Waiting, a.HoldTimeout = w, config.Duration(150*time.Millisecond) }
	}
	tests := []struct {
		name   string
		mutate func(*config.App)
		hdr    []string
		ctype  string
		want   string
	}{
		{"program gets 503 JSON", short(config.WaitingPage), nil, "application/json", `{"state":"starting"}`},
		{"browser gets the failed page", short(config.WaitingHold), []string{"Accept", htmlAccept}, "text/html; charset=utf-8", "<title>That didn&#39;t go as planned"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, _ := slowLive(t, tt.mutate)
			start := time.Now()
			r := <-l.async(t, "GET", "/", "", tt.hdr...)
			if took := time.Since(start); took < 150*time.Millisecond {
				t.Fatalf("answered after %s, before hold_timeout", took)
			}
			if r.status != http.StatusServiceUnavailable || r.ctype != tt.ctype || !strings.Contains(r.body, tt.want) {
				t.Fatalf("= %+v", r)
			}
			if !l.app.Booting() {
				t.Fatal("the timeout must not end the boot")
			}
		})
	}
}

func TestHeldRequestGetsTheFailure(t *testing.T) {
	l := newLiveProc(t, nil, func(p *liveProcess) { p.fail = true })
	r := <-l.async(t, "GET", "/", "")
	if r.status != http.StatusServiceUnavailable || r.body != `{"state":"failed"}` {
		t.Fatalf("= %+v", r)
	}
}
