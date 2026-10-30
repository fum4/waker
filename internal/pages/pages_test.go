package pages

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
)

func mustNew(t *testing.T, b config.Branding) *Pages {
	t.Helper()
	p, err := New(b)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEveryPageSharesTheBase(t *testing.T) {
	p := mustNew(t, config.Branding{})
	for page, c := range copyText {
		html, err := p.Render(page, "shop")
		if err != nil {
			t.Fatal(err)
		}
		s := string(html)
		for _, want := range []string{
			`<meta name="robots" content="noindex">`,
			`data-page="` + page + `"`,
			`class="logo"`, `src="/__waker/logo"`, `class="decode"`,
			`<p class="name">shop</p>`,
			"--sig:" + DefaultAccent,
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s page lacks %q", page, want)
			}
		}
		if !strings.Contains(s, "<title>"+escaped(c[0])+"</title>") {
			t.Errorf("%s page title wrong", page)
		}
		if strings.Contains(s, "http://") || strings.Contains(s, "https://") {
			t.Errorf("%s page makes an external reference", page)
		}
		if strings.Contains(strings.ToLower(s), "demo") {
			t.Errorf("%s page says demo", page)
		}
	}
}

// escaped escapes like html/template does for the copy we use.
func escaped(s string) string { return strings.ReplaceAll(s, "'", "&#39;") }

func TestOnlyTheStartingPagePolls(t *testing.T) {
	p := mustNew(t, config.Branding{})
	for page := range copyText {
		html, _ := p.Render(page, "shop")
		hasScript := bytes.Contains(html, []byte("<script"))
		if (page == Starting) != hasScript {
			t.Errorf("%s: script present = %v", page, hasScript)
		}
	}
	html, _ := p.Render(Starting, "shop")
	if !bytes.Contains(html, []byte(`fetch("/__waker/status"`)) {
		t.Fatal("starting page does not poll the status")
	}
}

func TestTheStartingPageCarriesTheFailedAndBusyCopy(t *testing.T) {
	p := mustNew(t, config.Branding{})
	html, _ := p.Render(Starting, "shop")
	for _, e := range startingEnds {
		title, _ := json.Marshal(copyText[e][0])
		if !bytes.Contains(html, bytes.Trim(title, `"`)) {
			t.Errorf("starting page lacks the %s title", e)
		}
	}
}

func TestTheNameIsEscaped(t *testing.T) {
	html, _ := mustNew(t, config.Branding{}).Render(Offline, "<b>x</b>")
	if bytes.Contains(html, []byte("<b>x")) || !bytes.Contains(html, []byte("&lt;b&gt;x")) {
		t.Fatalf("name not escaped: %s", html)
	}
}

func TestBranding(t *testing.T) {
	logo := filepath.Join(t.TempDir(), "acme.png")
	if err := os.WriteFile(logo, []byte("\x89PNG fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := mustNew(t, config.Branding{Logo: logo, Accent: "#ff3366", Name: "Acme"})
	b, ct := p.Logo()
	if ct != "image/png" || !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatalf("logo = %q, %s", b, ct)
	}
	html, _ := p.Render(Starting, "shop")
	if !bytes.Contains(html, []byte("--sig:#ff3366")) || !bytes.Contains(html, []byte(`alt="Acme"`)) {
		t.Fatalf("branding not applied: %s", html)
	}
	if _, ct := mustNew(t, config.Branding{}).Logo(); ct != "image/svg+xml" {
		t.Fatalf("default logo type = %s", ct)
	}
}

func TestAssets(t *testing.T) {
	if !bytes.HasPrefix(Font(), []byte("wOF2")) {
		t.Fatal("font is not woff2")
	}
	if !bytes.Contains(Heartbeat(), []byte(`navigator.sendBeacon("/__waker/active")`)) {
		t.Fatal("heartbeat does not beacon")
	}
	if _, err := New(config.Branding{Logo: "/nonexistent.svg"}); err == nil {
		t.Fatal("missing logo accepted")
	}
}
