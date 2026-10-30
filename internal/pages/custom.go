package pages

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
)

// WaitTag loads the poll script that opens the app once it is up. Every
// starting page carries it: the built-in one, and an owner's own file.
const WaitTag = `<script src="/__waker/wait.js" defer></script>`

// LogoURL is where every page finds the app's logo.
const LogoURL = "/__waker/logo"

// The placeholders an owner's page may use. Only these are replaced: the
// file is never executed as a template.
const (
	PlaceholderName   = "{{name}}"
	PlaceholderLogo   = "{{logo}}"
	PlaceholderAccent = "{{accent}}"
)

var bodyClose = regexp.MustCompile(`(?i)</body\s*>`)

// loadCustom reads the owner's page files and fills their placeholders; the
// starting page also gets the wait script, unless it loads it itself.
func loadCustom(files map[string]string, name, accent string) (map[string][]byte, error) {
	fill := strings.NewReplacer(
		PlaceholderName, html.EscapeString(name),
		PlaceholderLogo, html.EscapeString(LogoURL),
		PlaceholderAccent, html.EscapeString(accent),
	)
	out := map[string][]byte{}
	for page, path := range files {
		if !Has(page) {
			return nil, fmt.Errorf("pages.%s: no such page", page)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("pages.%s: %w", page, err)
		}
		b := []byte(fill.Replace(string(raw)))
		if page == Starting && !bytes.Contains(b, []byte(`src="/__waker/wait.js"`)) {
			b = WithTag(b, WaitTag)
		}
		out[page] = b
	}
	return out, nil
}

// WithTag inserts tag before page's last </body>, or appends it when there
// is none.
func WithTag(page []byte, tag string) []byte {
	at := len(page)
	if locs := bodyClose.FindAllIndex(page, -1); len(locs) > 0 {
		at = locs[len(locs)-1][0]
	}
	out := make([]byte, 0, len(page)+len(tag))
	out = append(out, page[:at]...)
	out = append(out, tag...)
	return append(out, page[at:]...)
}
