// Package testapp is a fake upstream app for waker's tests: proxy tests
// serve it in-process, and the end-to-end test runs it as a binary.
package testapp

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// PageHTML is the body of /html.
const PageHTML = "<!doctype html><html><body><p>app</p></body></html>"

// InjectCap mirrors the heartbeat buffer cap so /html-big is just past it.
const InjectCap = 5 << 20

// Handler serves the fake app's endpoints:
//
//	/html, /html-gz, /html-big   HTML (ETag "v1", 304 on If-None-Match), gzip-labelled, past the cap
//	/stream                      HTTP/1.0 close-delimited body "streamed-body"
//	/headers                     JSON of the request's headers and body
//	POST *                       echoes the body
//	GET *                        "app:<uri>" with two Set-Cookie headers
func Handler() http.Handler {
	return http.HandlerFunc(serve)
}

func serve(w http.ResponseWriter, r *http.Request) {
	switch p := r.URL.Path; {
	case strings.HasPrefix(p, "/html"):
		html(w, r)
	case p == "/stream":
		stream(w)
	case p == "/headers":
		headers(w, r)
	case r.Method == http.MethodPost:
		_ = http.NewResponseController(w).EnableFullDuplex()
		_, _ = io.Copy(w, r.Body)
	default:
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		_, _ = io.WriteString(w, "app:"+r.URL.RequestURI())
	}
}

func html(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Got-Accept-Encoding", r.Header.Get("Accept-Encoding"))
	h.Set("ETag", `"v1"`)
	if r.Header.Get("If-None-Match") == `"v1"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := PageHTML
	if r.URL.Path == "/html-big" {
		body = strings.Repeat(PageHTML, InjectCap/len(PageHTML)+1)
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	if r.URL.Path == "/html-gz" {
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", itoa(len(body)))
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, body)
	}
}

// stream answers like an HTTP/1.0 server: no length, body ends at close.
func stream(w http.ResponseWriter) {
	conn, buf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = buf.WriteString("HTTP/1.0 200 OK\r\nContent-Type: text/plain\r\n\r\nstreamed-body")
	_ = buf.Flush()
}

func headers(w http.ResponseWriter, r *http.Request) {
	var pairs [][2]string
	for k, vs := range r.Header {
		for _, v := range vs {
			pairs = append(pairs, [2]string{strings.ToLower(k), v})
		}
	}
	// net/http moves framing out of the header map; report it explicitly.
	framing := map[string]any{"content_length": r.ContentLength, "transfer_encoding": r.TransferEncoding}
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"headers": pairs, "body": string(body), "framing": framing})
}

func itoa(n int) string { return strconv.Itoa(n) }
