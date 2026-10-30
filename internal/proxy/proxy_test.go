package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/config"
)

func TestWakeThenProxy(t *testing.T) {
	l := newLive(t, nil)
	resp, body := l.do(t, "GET", "/", nil, "Accept", htmlAccept)
	if resp.StatusCode != 503 || resp.Header.Get("X-Robots-Tag") != "noindex" || resp.Header.Get("Retry-After") != "10" {
		t.Fatalf("first request = %d %v", resp.StatusCode, resp.Header)
	}
	contains(t, body, "Starting the servers")
	contains(t, body, `<p class="name">shop</p>`)
	l.waitUp(t)

	resp, body = l.do(t, "GET", "/page?q=1", nil)
	if body != "app:/page?q=1" || len(resp.Header.Values("Set-Cookie")) != 2 {
		t.Fatalf("proxied = %q, cookies %v", body, resp.Header.Values("Set-Cookie"))
	}
	big := strings.Repeat("x", 200_000)
	if _, body = l.do(t, "POST", "/echo", strings.NewReader(big)); body != big {
		t.Fatalf("200 KB echo came back as %d bytes", len(body))
	}
	if _, body = l.do(t, "POST", "/echo", io.MultiReader(strings.NewReader("ab"), strings.NewReader("cd"))); body != "abcd" {
		t.Fatalf("chunked echo = %q", body)
	}
	if _, body = l.do(t, "GET", "/stream", nil); body != "streamed-body" {
		t.Fatalf("close-delimited stream = %q", body)
	}
	if resp, _ = l.do(t, "POST", "/__waker/active", nil); resp.StatusCode != 204 {
		t.Fatalf("active = %d", resp.StatusCode)
	}
	if resp, _ = l.do(t, "GET", "/__waker/nope", nil); resp.StatusCode != 404 {
		t.Fatalf("unknown reserved = %d", resp.StatusCode)
	}
	if n := l.proc.starts.Load(); n != 1 {
		t.Fatalf("starts = %d", n)
	}
}

func TestIgnoredPathNeverWakes(t *testing.T) {
	l := newLive(t, nil)
	resp, body := l.do(t, "GET", "/poll", nil)
	if resp.StatusCode != 503 || body != `{"state":"down"}` || resp.Header.Get("Retry-After") != "10" {
		t.Fatalf("= %d %q", resp.StatusCode, body)
	}
	if l.proc.starts.Load() != 0 {
		t.Fatal("an ignored path woke the app")
	}
}

func TestVisitorsGetOnlyTheirAppsState(t *testing.T) {
	l := newLive(t, nil)
	resp, body := l.do(t, "GET", "/__waker/status", nil, "Accept", htmlAccept)
	if resp.StatusCode != 200 || body != `{"state":"down"}` || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status = %d %q", resp.StatusCode, body)
	}
	if resp, _ = l.do(t, "GET", "/__waker/health", nil); resp.StatusCode != 404 {
		t.Fatalf("health = %d", resp.StatusCode)
	}
}

func TestAssetsAreServedWithoutWaking(t *testing.T) {
	l := newLive(t, nil)
	for path, ctype := range map[string]string{
		"/__waker/font.woff2":   "font/woff2",
		"/__waker/logo":         "image/svg+xml",
		"/__waker/heartbeat.js": "text/javascript; charset=utf-8",
	} {
		resp, body := l.do(t, "GET", path, nil)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != ctype || resp.Header.Get("Cache-Control") != "private, max-age=3600" || body == "" {
			t.Errorf("%s = %d %v", path, resp.StatusCode, resp.Header)
		}
		if resp, _ = l.do(t, "POST", path, nil); resp.StatusCode != 404 {
			t.Errorf("POST %s = %d", path, resp.StatusCode)
		}
	}
	if l.proc.starts.Load() != 0 {
		t.Fatal("an asset woke the app")
	}
}

func TestModeOffAnswersOffline(t *testing.T) {
	l := newLive(t, func(a *config.App) { a.Mode = config.ModeOff })
	resp, body := l.do(t, "GET", "/", nil, "Sec-Fetch-Mode", "navigate")
	if resp.StatusCode != 503 || !strings.Contains(body, "This app is offline") {
		t.Fatalf("= %d %q", resp.StatusCode, body)
	}
	if _, body = l.do(t, "GET", "/__waker/status", nil); body != `{"state":"offline"}` {
		t.Fatalf("status in mode off = %q", body)
	}
	if l.proc.starts.Load() != 0 {
		t.Fatal("mode off woke the app")
	}
}

func TestAbsoluteFormTargetIsStillReserved(t *testing.T) {
	l := newLive(t, nil)
	got := l.raw(t, "GET http://shop/__waker/status HTTP/1.1\r\nHost: shop\r\nConnection: close\r\n\r\n")
	if !strings.HasPrefix(got, "200") || !strings.Contains(got, `{"state":"down"}`) || l.proc.starts.Load() != 0 {
		t.Fatalf("= %q", got)
	}
}

type echoed struct {
	Headers [][2]string    `json:"headers"`
	Body    string         `json:"body"`
	Framing map[string]any `json:"framing"`
}

func (e echoed) values(name string) []string {
	var out []string
	for _, p := range e.Headers {
		if p[0] == name {
			out = append(out, p[1])
		}
	}
	return out
}

func rawEcho(t *testing.T, l *live, req string) echoed {
	t.Helper()
	got := l.raw(t, req)
	var e echoed
	if err := json.Unmarshal([]byte(got[strings.Index(got, "\n")+1:]), &e); err != nil {
		t.Fatalf("%v: %q", err, got)
	}
	return e
}

func TestHeadersReachTheAppMinusHopByHop(t *testing.T) {
	l := newLive(t, nil)
	l.app.Wake(t.Context())
	l.waitUp(t)
	e := rawEcho(t, l, "GET /headers HTTP/1.1\r\nHost: shop\r\nCookie: a=1\r\nCookie: b=2\r\n"+
		"Connection: close, X-Secret\r\nX-Secret: 1\r\nKeep-Alive: 5\r\nProxy-Authorization: x\r\nTE: trailers\r\n\r\n")
	if c := e.values("cookie"); len(c) != 2 || c[0] != "a=1" || c[1] != "b=2" {
		t.Fatalf("cookies = %v", c)
	}
	for _, hop := range []string{"x-secret", "keep-alive", "proxy-authorization", "connection"} {
		if v := e.values(hop); len(v) > 0 {
			t.Errorf("%s reached the app: %v", hop, v)
		}
	}
}

func TestChunkedWinsOverContentLength(t *testing.T) {
	l := newLive(t, nil)
	l.app.Wake(t.Context())
	l.waitUp(t)
	e := rawEcho(t, l, "POST /headers HTTP/1.1\r\nHost: shop\r\nConnection: close\r\nTransfer-Encoding: chunked\r\nContent-Length: 999\r\n\r\n3\r\nabc\r\n0\r\n\r\n")
	if e.Body != "abc" || len(e.values("content-length")) > 0 {
		t.Fatalf("= %+v", e)
	}
}

func TestMalformedContentLengthIs400(t *testing.T) {
	l := newLive(t, nil)
	got := l.raw(t, "POST /x HTTP/1.1\r\nHost: shop\r\nContent-Length: abc\r\n\r\n")
	if !strings.HasPrefix(got, "400") || l.proc.starts.Load() != 0 {
		t.Fatalf("= %q", got)
	}
}

func TestConnectionCapAnswers503(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.NotFoundHandler()}
	go func() { _ = srv.Serve(LimitListener(ln, 1)) }()
	defer srv.Close()
	hold, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	time.Sleep(50 * time.Millisecond) // let the server accept the holder
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	got, _ := io.ReadAll(c)
	if !bytes.HasPrefix(got, []byte("HTTP/1.1 503")) || !bytes.Contains(got, []byte(`{"state":"busy"}`)) {
		t.Fatalf("= %q", got)
	}
}

func TestASlowRequestIsInFlightUntilItEnds(t *testing.T) {
	l := newLive(t, nil)
	l.app.Wake(t.Context())
	l.waitUp(t)
	done := make(chan int)
	go func() {
		resp, _ := l.do(t, "GET", "/slow", nil)
		done <- resp.StatusCode
	}()
	deadline := time.Now().Add(time.Second)
	for l.app.Activity().Inflight() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("slow request never counted in flight")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if code := <-done; code != http.StatusOK || l.app.Activity().Inflight() != 0 {
		t.Fatalf("slow = %d, inflight after = %d", code, l.app.Activity().Inflight())
	}
}
