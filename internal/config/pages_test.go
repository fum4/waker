package config

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noopDefault keeps the defaults block a mapping when a case sets nothing.
const noopDefault = "  idle: 30m\n"

func TestPagesParse(t *testing.T) {
	tests := []struct {
		name      string
		defs, web string
		web0, api Pages
	}{
		{"map", "", "    pages: {starting: s.html, down: d.html}\n",
			Pages{Files: map[string]string{"starting": "s.html", "down": "d.html"}}, Pages{}},
		{"bare", "", "    pages: bare\n", Pages{Bare: true}, Pages{}},
		{"defaults merge per key", "  pages: {starting: s.html, offline: o.html}\n", "    pages: {offline: mine.html}\n",
			Pages{Files: map[string]string{"starting": "s.html", "offline": "mine.html"}},
			Pages{Files: map[string]string{"starting": "s.html", "offline": "o.html"}}},
		{"app's bare beats a default map", "  pages: {starting: s.html}\n", "    pages: bare\n",
			Pages{Bare: true}, Pages{Files: map[string]string{"starting": "s.html"}}},
		{"app's map beats a default bare", "  pages: bare\n", "    pages: {failed: f.html}\n",
			Pages{Files: map[string]string{"failed": "f.html"}}, Pages{Bare: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(withDefaults(tt.defs+noopDefault, tt.web)))
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range []Pages{tt.web0, tt.api} {
				if got := cfg.Apps[i].Pages; got.Bare != want.Bare || !maps.Equal(got.Files, want.Files) {
					t.Errorf("apps[%d].pages = %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

func TestPagesParseErrors(t *testing.T) {
	tests := []struct {
		name, defs, web, want string
	}{
		{"bad scalar", "", "    pages: plain\n", `line 9: pages: "plain" must be bare or a mapping of page files`},
		{"unknown key", "", "    pages:\n      starting: s.html\n      landing: l.html\n", "line 11: pages.landing is not a page (want starting, offline, busy, failed, down)"},
		{"unknown key in defaults", "  pages: {splash: x.html}\n", "", "line 2: pages.splash is not a page"},
		{"a list", "", "    pages: [s.html]\n", "pages must be bare or a mapping"},
		{"not a path", "", "    pages: {starting: [a]}\n", "pages.starting must be a file path"},
		{"empty path", "", "    pages: {starting: ''}\n", "pages.starting must be a file path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(withDefaults(tt.defs+noopDefault, tt.web)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPagesValidation(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.html")
	big := filepath.Join(dir, "big.html")
	if err := os.WriteFile(ok, []byte("<p>hi</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, make([]byte, MaxPageBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "nope.html")
	tests := []struct {
		name, defs, web, want, not string
	}{
		{"missing file names the key", "", "    pages: {starting: " + missing + "}\n",
			"apps[0] (web).pages.starting: open " + missing + ": no such file or directory", "defaults"},
		{"inherited names defaults", "  pages: {offline: " + missing + "}\n", "",
			"defaults.pages.offline: open " + missing, "apps[0]"},
		{"merged key from the app", "  pages: {offline: " + ok + "}\n", "    pages: {busy: " + missing + "}\n",
			"apps[0] (web).pages.busy: open", "defaults.pages"},
		{"a directory", "", "    pages: {down: " + dir + "}\n", "apps[0] (web).pages.down: " + dir + " is a directory", ""},
		{"too big", "", "    pages: {failed: " + big + "}\n", "apps[0] (web).pages.failed: " + big + " is 1048577 bytes, over the 1MiB limit", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(withDefaults(tt.defs+noopDefault, tt.web)))
			if err != nil {
				t.Fatal(err)
			}
			verr := cfg.Validate()
			if verr == nil || strings.Count(verr.Error(), tt.want) != 1 || (tt.not != "" && strings.Contains(verr.Error(), tt.not)) {
				t.Fatalf("Validate err = %v, want %q once", verr, tt.want)
			}
		})
	}
	cfg, err := Parse([]byte(withDefaults("  pages: {starting: "+ok+"}\n", "")))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a readable page fails: %v", err)
	}
}

func TestPagesResolveAgainstTheConfigDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "starting.html"), []byte("<p>hi</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "waker.yaml")
	if err := os.WriteFile(path, []byte(withDefaults("  pages: {starting: starting.html}\n", "    pages: bare\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Apps[0].Pages.Bare || cfg.Apps[1].Pages.Files["starting"] != filepath.Join(dir, "starting.html") {
		t.Fatalf("pages = %+v, %+v", cfg.Apps[0].Pages, cfg.Apps[1].Pages)
	}
	out, err := cfg.EffectiveYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "pages: bare") || !strings.Contains(string(out), "starting: "+filepath.Join(dir, "starting.html")) {
		t.Fatalf("effective YAML:\n%s", out)
	}
	if _, err := Parse(out); err != nil {
		t.Fatalf("effective YAML does not parse: %v", err)
	}
}
