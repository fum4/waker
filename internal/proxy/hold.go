package proxy

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/pages"
)

// holds reports whether r waits out a boot instead of getting the starting
// page: programs always do (a page means nothing to them), browsers when
// the app says waiting: hold.
func (h *Handler) holds(r *http.Request) bool {
	return !wantsHTML(r) || h.app.Config().Waiting == config.WaitingHold
}

// hold waits for the app's boot to end, up to hold_timeout. It returns the
// state to answer with, or ok=false when it already answered (timed out) or
// the visitor hung up. The request stays in flight throughout, so the app is
// neither idle-stopped nor evicted under it, and its connection still counts
// against the listener's cap.
func (h *Handler) hold(w http.ResponseWriter, r *http.Request) (app.State, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), h.app.Config().HoldTimeout.D())
	defer cancel()
	st := h.app.AwaitBoot(ctx)
	switch {
	case r.Context().Err() != nil:
		return st, false
	case ctx.Err() != nil:
		// A browser can't poll the way a program can: show it the failure.
		page := pages.Starting
		if wantsHTML(r) {
			page = pages.Failed
		}
		h.page(w, r, http.StatusServiceUnavailable, page)
		return st, false
	}
	return st, true
}

// replayableBody lets a request whose body was never read be forwarded a
// second time: on a refused dial the transport closes the body without
// reading it, so the close is deferred until a read happened.
type replayableBody struct {
	rc   io.ReadCloser
	read atomic.Bool
}

func (b *replayableBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 || err != nil {
		b.read.Store(true)
	}
	return n, err
}

// Close closes the real body only once it was read; an untouched body
// stays open for the retry (net/http closes it when the handler returns).
func (b *replayableBody) Close() error {
	if b.read.Load() {
		return b.rc.Close()
	}
	return nil
}

// replayable reports whether r's body (if any) is still unread.
func replayable(r *http.Request) bool {
	b, ok := r.Body.(*replayableBody)
	return !ok || !b.read.Load()
}
