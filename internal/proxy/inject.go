package proxy

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// HeartbeatTag is injected into every HTML page so an open tab keeps an
// on-demand app awake while a visitor is reading.
const HeartbeatTag = `<script src="/__waker/heartbeat.js" defer></script>`

// InjectMaxBytes caps how much of a page is buffered for injection; bigger
// pages pass through untouched.
const InjectMaxBytes = 5 << 20

var bodyClose = regexp.MustCompile(`(?i)</body\s*>`)

type pageLoadKey struct{}

// isPageLoad: a GET that accepts HTML, for an app with the heartbeat on.
// Such requests ask the app for an uncompressed body so it can be injected.
func (h *Handler) isPageLoad(r *http.Request) bool {
	return h.app.Config().HeartbeatEnabled() && r.Method == http.MethodGet &&
		strings.Contains(r.Header.Get("Accept"), "text/html")
}

func markPageLoad(ctx context.Context) context.Context {
	return context.WithValue(ctx, pageLoadKey{}, true)
}

// injectHeartbeat is the ReverseProxy's ModifyResponse: a whole, plain
// (uncompressed) 200 text/html page within the cap gets the tag before its
// last </body>, a fixed Content-Length and no ETag (the bytes changed).
// Anything else — HEAD, 304, compressed, too big — passes through as is.
func injectHeartbeat(resp *http.Response) error {
	if v, _ := resp.Request.Context().Value(pageLoadKey{}).(bool); !v || !injectable(resp) {
		return nil
	}
	head, err := io.ReadAll(io.LimitReader(resp.Body, InjectMaxBytes+1))
	if err != nil {
		return err
	}
	if len(head) > InjectMaxBytes {
		resp.Body = readCloser{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
		return nil
	}
	resp.Body.Close()
	body := withTag(head)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Del("ETag")
	resp.Header.Del("Content-MD5")
	resp.TransferEncoding = nil
	return nil
}

func injectable(resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK || resp.Request.Method != http.MethodGet {
		return false
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	return mt == "text/html" && (enc == "" || enc == "identity")
}

// withTag inserts the tag before the last </body>, or appends it.
func withTag(page []byte) []byte {
	locs := bodyClose.FindAllIndex(page, -1)
	if len(locs) == 0 {
		return append(page, HeartbeatTag...)
	}
	at := locs[len(locs)-1][0]
	out := make([]byte, 0, len(page)+len(HeartbeatTag))
	out = append(out, page[:at]...)
	out = append(out, HeartbeatTag...)
	return append(out, page[at:]...)
}

type readCloser struct {
	io.Reader
	io.Closer
}
