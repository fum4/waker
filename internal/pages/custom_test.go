package pages

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/fum4/waker/internal/config"
)

// customApp writes each page's HTML to a file and returns an app using them.
func customApp(t *testing.T, name, accent string, files map[string]string) config.App {
	t.Helper()
	dir := t.TempDir()
	a := config.App{Name: name, Branding: config.Branding{Accent: accent}, Pages: config.Pages{Files: map[string]string{}}}
	for page, html := range files {
		path := filepath.Join(dir, page+".html")
		if err := os.WriteFile(path, []byte(html), 0o600); err != nil {
			t.Fatal(err)
		}
		a.Pages.Files[page] = path
	}
	return a
}

func TestCustomPages(t *testing.T) {
	const tpl = `<html><body style="--c:{{accent}}"><img src="{{logo}}"><h1>{{name}} {{name}}</h1>{{.Name}}</body></html>`
	tests := []struct {
		name, app, accent, page, file, want string
	}{
		{"placeholders filled, wait injected", "shop", "#ff3366", Starting, tpl,
			`<html><body style="--c:#ff3366"><img src="/__waker/logo"><h1>shop shop</h1>{{.Name}}` + WaitTag + `</body></html>`},
		{"default accent", "shop", "", Offline, `<i>{{accent}}</i>`, `<i>` + DefaultAccent + `</i>`},
		{"values escaped", `<b>"x"</b>`, "", Offline, `<p title="{{name}}">{{name}}</p>`,
			`<p title="&lt;b&gt;&#34;x&#34;&lt;/b&gt;">&lt;b&gt;&#34;x&#34;&lt;/b&gt;</p>`},
		{"no </body>: appended", "shop", "", Starting, `<p>wait`, `<p>wait` + WaitTag},
		{"last </body> wins", "shop", "", Starting, `<pre></body></pre></BODY >`, `<pre></body></pre>` + WaitTag + `</BODY >`},
		{"its own wait tag is kept, not doubled", "shop", "", Starting, `<p>x</p>` + WaitTag, `<p>x</p>` + WaitTag},
		{"other pages get no wait script", "shop", "", Failed, `<body>oops</body>`, `<body>oops</body>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustNew(t, customApp(t, tt.app, tt.accent, map[string]string{tt.page: tt.file}))
			got, err := p.Render(tt.page)
			if err != nil || string(got) != tt.want {
				t.Fatalf("Render = %q, %v\nwant     %q", got, err, tt.want)
			}
		})
	}
}

func TestCustomPagesLeaveTheOthersBuiltIn(t *testing.T) {
	p := mustNew(t, customApp(t, "shop", "", map[string]string{Offline: "<p>closed</p>"}))
	html, _ := p.Render(Starting)
	if !bytes.Contains(html, []byte("Starting the servers")) || bytes.Count(html, []byte(WaitTag)) != 1 {
		t.Fatalf("built-in starting page changed: %s", html)
	}
	if html, _ = p.Render(Offline); string(html) != "<p>closed</p>" {
		t.Fatalf("offline = %q", html)
	}
}

func TestCustomPageErrors(t *testing.T) {
	a := config.App{Name: "shop", Pages: config.Pages{Files: map[string]string{Starting: "/nonexistent/starting.html"}}}
	if _, err := New(a); err == nil || !bytes.Contains([]byte(err.Error()), []byte("pages.starting: open /nonexistent")) {
		t.Fatalf("New err = %v", err)
	}
}

func TestBare(t *testing.T) {
	if mustNew(t, config.App{}).Bare() || !mustNew(t, config.App{Pages: config.Pages{Bare: true}}).Bare() {
		t.Fatal("Bare does not follow pages: bare")
	}
}

func TestWithTag(t *testing.T) {
	const tag = "<x>"
	tests := map[string]string{
		"<body><pre></body></pre></BODY >": "<body><pre></body></pre><x></BODY >",
		"<p>hi":                            "<p>hi<x>",
		"":                                 "<x>",
	}
	for in, want := range tests {
		if got := string(WithTag([]byte(in), tag)); got != want {
			t.Errorf("WithTag(%q) = %q, want %q", in, got, want)
		}
	}
}
