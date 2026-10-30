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
	contact  *contact          // branding.contact as a link; nil when unset
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
		logoType: "image/svg+xml", bare: a.Pages.Bare, contact: newContact(b.Contact)}
	if b.Accent != "" {
		p.accent = template.CSS(b.Accent) // validated by config as #rgb / #rrggbb
	}
	if p.custom, err = loadCustom(a.Pages.Files, a.Name, string(p.accent), b.Contact); err != nil {
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
		"Page": page, "Title": c.Title, "Line": lineFor(c, p.contact), "Name": p.name,
		"Brand": p.brand, "Style": p.style, "Accent": p.accent,
	}
	if page == Starting {
		var ends []end
		for _, e := range startingEnds {
			ends = append(ends, end{Page: e, Title: copyText[e].Title, Line: lineFor(copyText[e], p.contact)})
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
