package pages

import (
	"strings"

	"github.com/fum4/waker/internal/config"
)

// pageCopy is one page's words. Ask is its line when branding.contact is
// set: the text before and after the contact link. A page without an Ask
// shows Line either way.
type pageCopy struct {
	Title, Line string
	Ask         [2]string
}

// copyText is the single home of every page's copy. Visitor copy never
// names the waker or calls the app a demo.
var copyText = map[string]pageCopy{
	Starting: {Title: "Powering up the servers", Line: "This takes a moment — the app will open by itself."},
	Offline: {Title: "This app is offline", Line: "It isn't running right now.",
		Ask: [2]string{"Need it running? Drop us a line at ", "."}},
	Busy: {Title: "Running out of room", Line: "Too much is running on this server right now. Try again in a few minutes."},
	Failed: {Title: "That didn't go as planned", Line: "It didn't start this time. Try again in a moment.",
		Ask: [2]string{"It didn't start this time. Try again in a moment, or drop us a line at ", "."}},
	Down: {Title: "Something's off", Line: "It should be running but isn't. Try again in a moment.",
		Ask: [2]string{"It should be running but isn't. We're on it — or drop us a line at ", "."}},
}

// startingEnds are the states the starting page swaps to without a reload.
var startingEnds = []string{Failed, Busy}

// contact is branding.contact as a link.
type contact struct{ Href, Label string }

// newContact links c, which config validated as an email address or an
// https URL; the label drops the scheme to stay readable.
func newContact(c string) *contact {
	if c == "" {
		return nil
	}
	if rest, ok := strings.CutPrefix(c, config.ContactURLPrefix); ok {
		return &contact{Href: c, Label: strings.TrimSuffix(rest, "/")}
	}
	return &contact{Href: "mailto:" + c, Label: c}
}

// line is a page's line as the template renders it: Text alone, or Before,
// the contact link, After. html/template escapes every part, the href too.
type line struct {
	Text, Before, After string
	Contact             *contact
}

// lineFor picks c's line: the Ask variant only when there is a contact.
func lineFor(c pageCopy, link *contact) line {
	if link == nil || c.Ask == [2]string{} {
		return line{Text: c.Line}
	}
	return line{Before: c.Ask[0], After: c.Ask[1], Contact: link}
}

// end is a page the starting page swaps to in place (see wait.js).
type end struct {
	Page, Title string
	Line        line
}
