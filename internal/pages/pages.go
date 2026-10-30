// Package pages renders what visitors see while an app is not up, and the
// static assets those pages use (font, logo, heartbeat and wait scripts).
// Pages make no external requests and never say more than the app's state.
package pages

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/fum4/waker/internal/config"
)

//go:embed assets
var assets embed.FS

// DefaultAccent is the indicator colour when branding.accent is unset.
const DefaultAccent = "#c6ff4a"

// Page names; each matches the app state that shows it, and config.PageKeys.
const (
	Starting = "starting"
	Offline  = "offline"
	Busy     = "busy"
	Failed   = "failed"
	Down     = "down"
)

// copyText is each page's title and line. Visitor copy never names the
// waker or calls the app a demo.
var copyText = map[string][2]string{
	Starting: {"Powering up", "This takes a moment — the app will open by itself."},
	Offline:  {"This app is offline", "It isn't running right now."},
	Busy:     {"The server is busy", "It can't start the app right now. Try again in a few minutes."},
	Failed:   {"Couldn't start the app", "Something went wrong while starting it. Try again in a moment."},
	Down:     {"This app isn't running", "Try again in a few minutes."},
}

// startingEnds are the states the starting page swaps to without a reload.
var startingEnds = []string{Failed, Busy}

// Pages renders one app's pages, with its branding and page files.
type Pages struct {
	tmpl     *template.Template
	style    template.CSS
	accent   template.CSS
	brand    string
	name     string
	logo     []byte
	logoType string
	bare     bool
	custom   map[string][]byte // page → the owner's file, rendered once
}

// New loads the templates, the app's logo and its page files (each read
// once, here).
func New(a config.App) (*Pages, error) {
	tmpl, err := template.ParseFS(assets, "assets/page.html")
	if err != nil {
		return nil, err
	}
	css, err := assets.ReadFile("assets/page.css")
	if err != nil {
		return nil, err
	}
	b := a.Branding
	p := &Pages{tmpl: tmpl, style: template.CSS(css), accent: DefaultAccent, brand: b.Name, name: a.Name,
		logoType: "image/svg+xml", bare: a.Pages.Bare}
	if b.Accent != "" {
		p.accent = template.CSS(b.Accent) // validated by config as #rgb / #rrggbb
	}
	if p.custom, err = loadCustom(a.Pages.Files, a.Name, string(p.accent)); err != nil {
		return nil, err
	}
	if b.Logo == "" {
		p.logo, err = assets.ReadFile("assets/logo.svg")
		return p, err
	}
	if p.logo, err = os.ReadFile(b.Logo); err != nil {
		return nil, fmt.Errorf("branding.logo: %w", err)
	}
	if strings.EqualFold(filepath.Ext(b.Logo), ".png") {
		p.logoType = "image/png"
	}
	return p, nil
}

// Has reports whether name is a page.
func Has(name string) bool {
	_, ok := copyText[name]
	return ok
}

// Bare reports whether every page answer is headers only (pages: bare).
func (p *Pages) Bare() bool { return p.bare }

// Render returns the HTML of page: the owner's file when there is one,
// else the built-in page.
func (p *Pages) Render(page string) ([]byte, error) {
	if b, ok := p.custom[page]; ok {
		return b, nil
	}
	c, ok := copyText[page]
	if !ok {
		return nil, fmt.Errorf("no page %q", page)
	}
	data := map[string]any{
		"Page": page, "Title": c[0], "Text": c[1], "Name": p.name,
		"Brand": p.brand, "Style": p.style, "Accent": p.accent,
	}
	if page == Starting {
		ends := map[string][2]string{}
		for _, e := range startingEnds {
			ends[e] = copyText[e]
		}
		data["Ends"] = ends
		data["Wait"] = template.HTML(WaitTag)
	}
	var buf bytes.Buffer
	if err := p.tmpl.ExecuteTemplate(&buf, "page.html", data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Logo returns the logo bytes and content type.
func (p *Pages) Logo() ([]byte, string) { return p.logo, p.logoType }

// Font returns the Geist woff2 bytes.
func Font() []byte { return mustAsset("assets/geist.woff2") }

// Heartbeat returns the heartbeat script.
func Heartbeat() []byte { return mustAsset("assets/heartbeat.js") }

// Wait returns the starting pages' poll script.
func Wait() []byte { return mustAsset("assets/wait.js") }

func mustAsset(name string) []byte {
	b, err := assets.ReadFile(name)
	if err != nil {
		panic(err) // embedded at build time; missing means a broken build
	}
	return b
}
