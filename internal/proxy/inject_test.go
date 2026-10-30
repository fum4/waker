package proxy

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/testapp"
)

var pageHeaders = []string{"Accept", htmlAccept, "Accept-Encoding", "gzip, br"}

func upLive(t *testing.T, mutate func(*config.App)) *live {
	t.Helper()
	l := newLive(t, mutate)
	l.app.Wake(t.Context())
	l.waitUp(t)
	return l
}

func TestAPageGetsTheTagOnceBeforeItsBodyEnds(t *testing.T) {
	l := upLive(t, nil)
	resp, body := l.do(t, "GET", "/html", nil, pageHeaders...)
	want := strings.Replace(testapp.PageHTML, "</body>", HeartbeatTag+"</body>", 1)
	if body != want {
		t.Fatalf("body = %q", body)
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(want)) || resp.Header.Get("ETag") != "" {
		t.Fatalf("headers = %v", resp.Header)
	}
	if got := resp.Header.Get("X-Got-Accept-Encoding"); got != "identity" {
		t.Fatalf("the app was asked for %q, want identity", got)
	}
}

func TestHeadAnd304AreUntouched(t *testing.T) {
	l := upLive(t, nil)
	resp, body := l.do(t, "HEAD", "/html", nil, pageHeaders...)
	if resp.StatusCode != 200 || body != "" || resp.Header.Get("Content-Length") != strconv.Itoa(len(testapp.PageHTML)) || resp.Header.Get("ETag") != `"v1"` {
		t.Fatalf("HEAD = %d %v", resp.StatusCode, resp.Header)
	}
	resp, body = l.do(t, "GET", "/html", nil, append(pageHeaders, "If-None-Match", `"v1"`)...)
	if resp.StatusCode != http.StatusNotModified || body != "" {
		t.Fatalf("304 = %d %q", resp.StatusCode, body)
	}
}

func TestNonPagesKeepTheirBytesAndCompression(t *testing.T) {
	l := upLive(t, nil)
	resp, body := l.do(t, "GET", "/html", nil, "Accept", "*/*", "Accept-Encoding", "gzip")
	if body != testapp.PageHTML || resp.Header.Get("ETag") != `"v1"` || resp.Header.Get("X-Got-Accept-Encoding") != "gzip" {
		t.Fatalf("non-page = %q %v", body, resp.Header)
	}
	if _, body = l.do(t, "GET", "/x", nil, pageHeaders...); body != "app:/x" {
		t.Fatalf("non-HTML response = %q", body)
	}
}

func TestACompressedPagePassesThrough(t *testing.T) {
	l := upLive(t, nil)
	resp, body := l.do(t, "GET", "/html-gz", nil, pageHeaders...)
	if body != testapp.PageHTML || resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("= %q %v", body, resp.Header)
	}
}

func TestAPagePastTheCapPassesThroughWhole(t *testing.T) {
	l := upLive(t, nil)
	resp, body := l.do(t, "GET", "/html-big", nil, pageHeaders...)
	if len(body) <= InjectMaxBytes || strings.Contains(body, HeartbeatTag) || resp.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatalf("len %d, Content-Length %s", len(body), resp.Header.Get("Content-Length"))
	}
	if !strings.HasPrefix(body, testapp.PageHTML) || resp.Header.Get("ETag") != `"v1"` {
		t.Fatal("bytes or ETag changed")
	}
}

func TestOptOut(t *testing.T) {
	off := false
	l := upLive(t, func(a *config.App) { a.Heartbeat = &off })
	resp, body := l.do(t, "GET", "/html", nil, pageHeaders...)
	if body != testapp.PageHTML || resp.Header.Get("ETag") != `"v1"` || resp.Header.Get("X-Got-Accept-Encoding") != "gzip, br" {
		t.Fatalf("= %q %v", body, resp.Header)
	}
}

func TestWithTag(t *testing.T) {
	tests := map[string]string{
		"<body><pre></body></pre></BODY >": "<body><pre></body></pre>" + HeartbeatTag + "</BODY >",
		"<p>hi":                            "<p>hi" + HeartbeatTag,
	}
	for in, want := range tests {
		if got := string(withTag([]byte(in))); got != want {
			t.Errorf("withTag(%q) = %q, want %q", in, got, want)
		}
	}
}
