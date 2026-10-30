package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/pages"
)

// Cache policies of the waker's own responses.
const (
	cacheNone  = "no-store"
	cacheAsset = "private, max-age=3600"
	retryAfter = "10"
)

// reserved answers /__waker/*; it never wakes the app.
func (h *Handler) reserved(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, ReservedPrefix)
	get := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case name == "active" && r.Method == http.MethodPost:
		h.app.Activity().Touch()
		w.Header().Set("Cache-Control", cacheNone)
		w.Header().Set("X-Robots-Tag", "noindex")
		w.WriteHeader(http.StatusNoContent)
	case name == "status":
		// Visitors learn their app's state and nothing else.
		writeJSON(w, r, http.StatusOK, map[string]string{"state": string(h.app.VisitorState(r.Context()))})
	case name == "font.woff2" && get:
		write(w, r, http.StatusOK, "font/woff2", pages.Font(), cacheAsset)
	case name == "heartbeat.js" && get:
		write(w, r, http.StatusOK, "text/javascript; charset=utf-8", pages.Heartbeat(), cacheAsset)
	case name == "wait.js" && get:
		write(w, r, http.StatusOK, "text/javascript; charset=utf-8", pages.Wait(), cacheAsset)
	case name == "logo" && get:
		logo, ct := h.pages.Logo()
		write(w, r, http.StatusOK, ct, logo, cacheAsset)
	default:
		writeJSON(w, r, http.StatusNotFound, map[string]string{"error": ReservedPrefix + "* is reserved by waker"})
	}
}

// page answers with the named page for a browser navigation, else JSON;
// with pages: bare, with the status and headers alone.
func (h *Handler) page(w http.ResponseWriter, r *http.Request, status int, page string) {
	if h.pages.Bare() {
		write(w, r, status, "", nil, cacheNone)
		return
	}
	if wantsHTML(r) {
		html := page
		if page == string(app.Asleep) {
			// A visitor reaches an asleep app only on an idle_ignore path,
			// which never wakes it: the down page fits.
			html = pages.Down
		}
		body, err := h.pages.Render(html)
		if err == nil {
			write(w, r, status, "text/html; charset=utf-8", body, cacheNone)
			return
		}
		h.log.Error("render page", "page", page, "err", err)
	}
	writeJSON(w, r, status, map[string]string{"state": page})
}

// wantsHTML: a navigation, or a GET that accepts HTML.
func wantsHTML(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Mode") == "navigate" ||
		(r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html"))
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, _ := json.Marshal(v) // maps of strings always marshal
	write(w, r, status, "application/json", body, cacheNone)
}

// write answers with body; an empty ctype sends no Content-Type (a bare
// answer has no body to type).
func write(w http.ResponseWriter, r *http.Request, status int, ctype string, body []byte, cache string) {
	hd := w.Header()
	if ctype != "" {
		hd.Set("Content-Type", ctype)
	}
	hd.Set("Content-Length", strconv.Itoa(len(body)))
	hd.Set("Cache-Control", cache)
	hd.Set("X-Robots-Tag", "noindex")
	if status == http.StatusServiceUnavailable {
		hd.Set("Retry-After", retryAfter)
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
