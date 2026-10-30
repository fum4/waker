package pages

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
)

func mustNew(t *testing.T, a config.App) *Pages {
	t.Helper()
	if a.Name == "" {
		a.Name = "shop"
	}
	p, err := New(a)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestThePagesAreTheConfigsPageKeys(t *testing.T) {
	for _, k := range config.PageKeys {
		if !Has(k) {
			t.Errorf("config.PageKeys has %q, pages doesn't", k)
		}
	}
	for page := range copyText {
		if !slices.Contains(config.PageKeys, page) {
			t.Errorf("page %q missing from config.PageKeys", page)
		}
	}
}

func TestEveryPageSharesTheBase(t *testing.T) {
	p := mustNew(t, config.App{})
	for page, c := range copyText {
		html, err := p.Render(page)
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

func TestOnlyTheStartingPageLoadsTheWaitScript(t *testing.T) {
	p := mustNew(t, config.App{})
	for page := range copyText {
		html, _ := p.Render(page)
		if got := bytes.Count(html, []byte(WaitTag)); got != map[bool]int{true: 1}[page == Starting] {
			t.Errorf("%s: wait tag %d times", page, got)
		}
		if page != Starting && bytes.Contains(html, []byte("<script")) {
			t.Errorf("%s page has a script", page)
		}
	}
	html, _ := p.Render(Starting)
	if bytes.Contains(html, []byte("fetch(")) || bytes.Contains(html, []byte("setInterval")) {
		t.Fatal("the starting page still polls inline")
	}
	if !bytes.Contains(Wait(), []byte(`fetch("/__waker/status"`)) || !bytes.Contains(Wait(), []byte(`getElementById("waker-ends")`)) {
		t.Fatal("wait.js does not poll the status or read the end copy")
	}
}

func TestTheStartingPageCarriesTheFailedAndBusyCopy(t *testing.T) {
	html, _ := mustNew(t, config.App{}).Render(Starting)
	const open = `<script type="application/json" id="waker-ends">`
	_, rest, ok := bytes.Cut(html, []byte(open))
	data, _, _ := bytes.Cut(rest, []byte("</script>"))
	var ends map[string][2]string
	if err := json.Unmarshal(data, &ends); !ok || err != nil {
		t.Fatalf("end copy is not JSON (%v): %s", err, data)
	}
	for _, e := range startingEnds {
		if ends[e] != copyText[e] {
			t.Errorf("%s end copy = %q", e, ends[e])
		}
	}
}

func TestTheNameIsEscaped(t *testing.T) {
	html, _ := mustNew(t, config.App{Name: "<b>x</b>"}).Render(Offline)
	if bytes.Contains(html, []byte("<b>x")) || !bytes.Contains(html, []byte("&lt;b&gt;x")) {
		t.Fatalf("name not escaped: %s", html)
	}
}

func TestBranding(t *testing.T) {
	logo := filepath.Join(t.TempDir(), "acme.png")
	if err := os.WriteFile(logo, []byte("\x89PNG fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := mustNew(t, config.App{Branding: config.Branding{Logo: logo, Accent: "#ff3366", Name: "Acme"}})
	b, ct := p.Logo()
	if ct != "image/png" || !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatalf("logo = %q, %s", b, ct)
	}
	html, _ := p.Render(Starting)
	if !bytes.Contains(html, []byte("--sig:#ff3366")) || !bytes.Contains(html, []byte(`alt="Acme"`)) {
		t.Fatalf("branding not applied: %s", html)
	}
	if _, ct := mustNew(t, config.App{}).Logo(); ct != "image/svg+xml" {
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
	if _, err := New(config.App{Branding: config.Branding{Logo: "/nonexistent.svg"}}); err == nil {
		t.Fatal("missing logo accepted")
	}
}
