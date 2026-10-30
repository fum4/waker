// Package proxy is the HTTP front of one app: it forwards to the upstream
// when the app is up, wakes it on a refused connection, and answers with a
// page (browsers) or {"state": …} JSON (everything else) while it is not up.
//
// Forwarding is httputil.ReverseProxy: streaming both ways, hop-by-hop and
// Connection-named headers stripped, multi-value headers kept, chunked and
// Content-Length bodies handled by net/http, WebSocket upgrades tunnelled.
package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/pages"
)

// ReservedPrefix is never forwarded: the waker answers these paths itself,
// in every mode and state.
const ReservedPrefix = "/__waker/"

// UpstreamDialTimeout bounds connecting to the app.
const UpstreamDialTimeout = 5 * time.Second

// Handler fronts one app.
type Handler struct {
	app    *app.App
	pages  *pages.Pages
	rp     *httputil.ReverseProxy
	ignore map[string]bool
	log    *slog.Logger
}

type retriedKey struct{}

// New builds the handler for a.
func New(a *app.App, p *pages.Pages, log *slog.Logger) (*Handler, error) {
	cfg := a.Config()
	target, err := url.Parse(cfg.Upstream)
	if err != nil {
		return nil, err
	}
	h := &Handler{app: a, pages: p, ignore: map[string]bool{}, log: log.With("app", cfg.Name)}
	for _, path := range cfg.IdleIgnore {
		h.ignore[path] = true
	}
	h.rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host // the app sees the visitor's Host
			pr.SetXForwarded()
			if v, _ := pr.In.Context().Value(pageLoadKey{}).(bool); v {
				pr.Out.Header.Set("Accept-Encoding", "identity") // so the page can be injected
			}
		},
		Transport: &http.Transport{
			Proxy:               nil, // never through an env proxy: the upstream is local
			DialContext:         (&net.Dialer{Timeout: UpstreamDialTimeout}).DialContext,
			DisableCompression:  true, // pass the app's encoding through untouched
			MaxIdleConnsPerHost: 16,
			IdleConnTimeout:     90 * time.Second,
		},
		FlushInterval:  -1, // stream: flush every write
		ModifyResponse: injectHeartbeat,
		ErrorHandler:   h.upstreamError,
	}
	return h, nil
}

// ServeHTTP routes a visitor request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, ReservedPrefix) {
		h.reserved(w, r)
		return
	}
	if h.app.Mode() == config.ModeOff {
		h.page(w, r, http.StatusServiceUnavailable, pages.Offline)
		return
	}
	h.forward(w, r, false)
}

// forward proxies r, counting it as activity unless its path is ignored.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, retried bool) {
	// Stream both ways: without this, HTTP/1 stops reading the request body
	// once the response starts, truncating uploads to echo-style apps.
	_ = http.NewResponseController(w).EnableFullDuplex()
	if !h.ignore[r.URL.Path] {
		act := h.app.Activity()
		act.Begin()
		defer act.End()
	}
	ctx := context.WithValue(r.Context(), retriedKey{}, retried)
	if h.isPageLoad(r) {
		ctx = markPageLoad(ctx)
	}
	h.rp.ServeHTTP(w, r.WithContext(ctx))
}

// upstreamError runs before anything was written to w. A refused connection
// means the app is down: wake it (unless the path is ignored) and answer
// with its state; it may have just come up, then forward once more.
func (h *Handler) upstreamError(w http.ResponseWriter, r *http.Request, err error) {
	var op *net.OpError
	if !errors.As(err, &op) || op.Op != "dial" {
		if !errors.Is(err, context.Canceled) {
			h.log.Warn("upstream error", "method", r.Method, "path", r.URL.Path, "err", err)
		}
		h.page(w, r, http.StatusBadGateway, pages.Down)
		return
	}
	// The request may have waited out a mode-off stop: never wake an off app.
	if h.app.Mode() == config.ModeOff {
		h.page(w, r, http.StatusServiceUnavailable, pages.Offline)
		return
	}
	if h.ignore[r.URL.Path] {
		h.page(w, r, http.StatusServiceUnavailable, pages.Down)
		return
	}
	st := h.app.Wake(r.Context())
	if st != app.Up {
		h.page(w, r, http.StatusServiceUnavailable, string(st))
		return
	}
	// A body may already be partly consumed, so only bodiless requests retry.
	if retried, _ := r.Context().Value(retriedKey{}).(bool); retried || r.ContentLength != 0 {
		h.page(w, r, http.StatusBadGateway, pages.Down)
		return
	}
	h.forward(w, r, true)
}
