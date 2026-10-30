package pages

import (
	"regexp"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
)

// escaped escapes like html/template does for the copy we use.
func escaped(s string) string { return strings.ReplaceAll(s, "'", "&#39;") }

// contacts are the three ways an app can be branded, with the link each
// renders (empty: none).
var contacts = []struct{ name, contact, link string }{
	{"email", "hello@example.com", `<a href="mailto:hello@example.com">hello@example.com</a>`},
	{"https URL", "https://example.com/help/", `<a href="https://example.com/help/">example.com/help</a>`},
	{"no contact", "", ""},
}

var mainText = regexp.MustCompile(`<p id="text">(.*?)</p>`)

func TestEveryPageCopyWithAndWithoutContact(t *testing.T) {
	want := map[string]struct{ title, with, without string }{
		Starting: {"Powering up the servers", "", "This takes a moment — the app will open by itself."},
		Offline:  {"This app is offline", "Need it running? Drop us a line at %s.", "It isn't running right now."},
		Busy:     {"Running out of room", "", "Too much is running on this server right now. Try again in a few minutes."},
		Failed: {"That didn't go as planned", "It didn't start this time. Try again in a moment, or drop us a line at %s.",
			"It didn't start this time. Try again in a moment."},
		Down: {"Something's off", "It should be running but isn't. We're on it — or drop us a line at %s.",
			"It should be running but isn't. Try again in a moment."},
	}
	if len(want) != len(copyText) {
		t.Fatalf("want covers %d pages, copyText has %d", len(want), len(copyText))
	}
	for page, w := range want {
		for _, c := range contacts {
			t.Run(page+"/"+c.name, func(t *testing.T) {
				html, err := mustNew(t, config.App{Branding: config.Branding{Contact: c.contact}}).Render(page)
				if err != nil {
					t.Fatal(err)
				}
				s := string(html)
				if !strings.Contains(s, "<title>"+escaped(w.title)+"</title>") || !strings.Contains(s, `<h1 id="title">`+escaped(w.title)+"</h1>") {
					t.Errorf("title is not %q", w.title)
				}
				line := escaped(w.without)
				if c.link != "" && w.with != "" {
					line = strings.Replace(escaped(w.with), "%s", c.link, 1)
				}
				m := mainText.FindStringSubmatch(s)
				if m == nil || m[1] != line {
					t.Errorf("line = %q\nwant   %q", m, line)
				}
				if got := strings.Count(s, "<a "); page != Starting && got != map[bool]int{true: 1}[line != escaped(w.without)] {
					t.Errorf("%d links", got)
				}
				if strings.Contains(strings.ToLower(s), "demo") {
					t.Error("page says demo")
				}
			})
		}
	}
}

func TestTheContactIsEscaped(t *testing.T) {
	// Validation rejects anything that would need it; rendering escapes anyway.
	p := mustNew(t, config.App{Branding: config.Branding{Contact: `https://example.com/?a=1&b="<x>"`}})
	html, _ := p.Render(Offline)
	want := `<a href="https://example.com/?a=1&amp;b=%22%3cx%3e%22">example.com/?a=1&amp;b=&#34;&lt;x&gt;&#34;</a>`
	if m := mainText.FindStringSubmatch(string(html)); m == nil || !strings.Contains(m[1], want) {
		t.Fatalf("contact not escaped: %q\nwant %q", m, want)
	}
}

func TestNewContact(t *testing.T) {
	tests := []struct{ in, href, label string }{
		{"a.b+c@example.co.uk", "mailto:a.b+c@example.co.uk", "a.b+c@example.co.uk"},
		{"https://example.com", "https://example.com", "example.com"},
		{"https://example.com/", "https://example.com/", "example.com"},
		{"https://example.com/contact?x=1", "https://example.com/contact?x=1", "example.com/contact?x=1"},
	}
	for _, tt := range tests {
		if c := newContact(tt.in); c == nil || c.Href != tt.href || c.Label != tt.label {
			t.Errorf("newContact(%q) = %+v", tt.in, c)
		}
	}
	if newContact("") != nil {
		t.Error("empty contact links")
	}
}
