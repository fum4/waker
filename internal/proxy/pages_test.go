package proxy

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/pages"
)

func TestBareAnswersAreHeadersOnly(t *testing.T) {
	bare := func(a *config.App) { a.Pages = config.Pages{Bare: true} }
	tests := []struct {
		name   string
		mutate func(*config.App)
		path   string
		hdr    []string
		status int
	}{
		{"browser, starting", bare, "/", []string{"Accept", htmlAccept}, 503},
		{"browser, offline", func(a *config.App) { bare(a); a.Mode = config.ModeOff }, "/", []string{"Sec-Fetch-Mode", "navigate"}, 503},
		{"program, offline", func(a *config.App) { bare(a); a.Mode = config.ModeOff }, "/api", nil, 503},
		{"program, ignored path down", bare, "/poll", nil, 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLive(t, tt.mutate)
			resp, body := l.do(t, "GET", tt.path, nil, tt.hdr...)
			hd := resp.Header
			if resp.StatusCode != tt.status || body != "" || hd.Get("Content-Length") != "0" || hd.Get("Content-Type") != "" ||
				hd.Get("Cache-Control") != "no-store" || hd.Get("X-Robots-Tag") != "noindex" || hd.Get("Retry-After") != "10" {
				t.Fatalf("= %d %q %v", resp.StatusCode, body, hd)
			}
		})
	}
}

func TestBareKeepsTheWakerAssetsAndStatus(t *testing.T) {
	l := newLive(t, func(a *config.App) { a.Pages = config.Pages{Bare: true} })
	if resp, body := l.do(t, "GET", "/__waker/status", nil); resp.StatusCode != 200 || body != `{"state":"down"}` {
		t.Fatalf("status = %d %q", resp.StatusCode, body)
	}
	if resp, body := l.do(t, "GET", "/__waker/wait.js", nil); resp.StatusCode != 200 || body != string(pages.Wait()) {
		t.Fatalf("wait.js = %d", resp.StatusCode)
	}
}

func TestCustomPagesAreServedLikeBuiltInOnes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{pages.Starting: "<body><h1>{{name}} wakes</h1></body>", pages.Offline: "<body>{{name}} closed</body>"}
	custom := func(a *config.App) {
		a.Pages = config.Pages{Files: map[string]string{}}
		for page, html := range files {
			path := filepath.Join(dir, page+".html")
			if err := os.WriteFile(path, []byte(html), 0o600); err != nil {
				t.Fatal(err)
			}
			a.Pages.Files[page] = path
		}
	}
	tests := []struct {
		name   string
		mutate func(*config.App)
		want   string
	}{
		{"starting gets the wait script", custom, "<body><h1>shop wakes</h1>" + pages.WaitTag + "</body>"},
		{"offline does not", func(a *config.App) { custom(a); a.Mode = config.ModeOff }, "<body>shop closed</body>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLive(t, tt.mutate)
			resp, body := l.do(t, "GET", "/", nil, "Accept", htmlAccept)
			hd := resp.Header
			if resp.StatusCode != http.StatusServiceUnavailable || body != tt.want || hd.Get("Content-Type") != "text/html; charset=utf-8" ||
				hd.Get("Cache-Control") != "no-store" || hd.Get("X-Robots-Tag") != "noindex" {
				t.Fatalf("= %d %q %v", resp.StatusCode, body, hd)
			}
		})
	}
}
