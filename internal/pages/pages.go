// Package pages renders what visitors see while an app is not up, and the
// static assets those pages use (font, logo, heartbeat script). Pages make
// no external requests and never say more than the app's state.
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

// Page names; each matches the app state that shows it.
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
	Starting: {"Starting the servers", "This takes a moment — the app will open by itself."},
	Offline:  {"This app is offline", "It isn't running right now."},
	Busy:     {"The server is busy", "It can't start the app right now. Try again in a few minutes."},
	Failed:   {"Couldn't start the app", "Something went wrong while starting it. Try again in a moment."},
	Down:     {"This app isn't running", "Try again in a few minutes."},
}

// startingEnds are the states the starting page swaps to without a reload.
var startingEnds = []string{Failed, Busy}

// Pages renders pages with one branding.
type Pages struct {
	tmpl     *template.Template
	style    template.CSS
	accent   template.CSS
	brand    string
	logo     []byte
	logoType string
}

// New loads the templates and the branding's logo file (read once, here).
func New(b config.Branding) (*Pages, error) {
	tmpl, err := template.ParseFS(assets, "assets/page.html")
	if err != nil {
		return nil, err
	}
	css, err := assets.ReadFile("assets/page.css")
	if err != nil {
		return nil, err
	}
	p := &Pages{tmpl: tmpl, style: template.CSS(css), accent: DefaultAccent, brand: b.Name, logoType: "image/svg+xml"}
	if b.Accent != "" {
		p.accent = template.CSS(b.Accent) // validated by config as #rgb / #rrggbb
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

// Render returns the HTML of page for the app called name.
func (p *Pages) Render(page, name string) ([]byte, error) {
	c, ok := copyText[page]
	if !ok {
		return nil, fmt.Errorf("no page %q", page)
	}
	data := map[string]any{
		"Page": page, "Title": c[0], "Text": c[1], "Name": name,
		"Brand": p.brand, "Style": p.style, "Accent": p.accent,
	}
	if page == Starting {
		ends := map[string][2]string{}
		for _, e := range startingEnds {
			ends[e] = copyText[e]
		}
		data["Ends"] = ends
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

func mustAsset(name string) []byte {
	b, err := assets.ReadFile(name)
	if err != nil {
		panic(err) // embedded at build time; missing means a broken build
	}
	return b
}
