// Package control is the waker's admin API: HTTP over a unix socket that
// only the waker's user can open (dir 0700, socket 0600). `waker status`,
// `waker mode` and `waker reset` are its clients.
//
//	GET  /status  → [Snapshot, …]
//	GET  /health  → {"listeners": [...], "skipped": {name: reason}}
//	POST /mode    {app, mode, idle?} | {app, reset: true} → Snapshot
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

// Backend is what the control API drives (the daemon).
type Backend interface {
	Status(ctx context.Context) []app.Snapshot
	Health() Health
	// SetMode applies or (mode nil) resets an override. It returns an
	// *app.InputError for bad input and a *NotFoundError for an unknown app.
	SetMode(name string, mode *config.Mode, idle time.Duration) (app.Snapshot, error)
}

// Health lists which apps got their listener.
type Health struct {
	Listeners []string          `json:"listeners"`
	Skipped   map[string]string `json:"skipped"`
}

// ModeRequest is the body of POST /mode.
type ModeRequest struct {
	App   string          `json:"app"`
	Mode  config.Mode     `json:"mode,omitempty"`
	Idle  config.Duration `json:"idle,omitempty"`
	Reset bool            `json:"reset,omitempty"`
}

// NotFoundError is an unknown app name.
type NotFoundError struct{ Msg string }

func (e *NotFoundError) Error() string { return e.Msg }

// maxBody bounds a control request body.
const maxBody = 1 << 16

// Listen creates the socket: its directory 0700, the socket 0600, a stale
// socket replaced — but never one another waker still answers on.
func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, fmt.Errorf("%s: another waker is already listening", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	old := syscall.Umask(0o177) // no window where the socket is group/world-accessible
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	return ln, os.Chmod(path, 0o600)
}

// Handler returns the control API over b.
func Handler(b Backend, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, b.Status(r.Context()))
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, b.Health())
	})
	mux.HandleFunc("POST /mode", func(w http.ResponseWriter, r *http.Request) {
		var req ModeRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || req.App == "" || (req.Reset == (req.Mode != "")) {
			replyError(w, http.StatusBadRequest, "expected JSON {app, mode, idle?} or {app, reset: true}")
			return
		}
		var mode *config.Mode
		if !req.Reset {
			mode = &req.Mode
		}
		snap, err := b.SetMode(req.App, mode, req.Idle.D())
		var input *app.InputError
		var missing *NotFoundError
		switch {
		case errors.As(err, &input):
			replyError(w, http.StatusBadRequest, err.Error())
		case errors.As(err, &missing):
			replyError(w, http.StatusNotFound, err.Error())
		case err != nil:
			log.Error("control: set mode", "err", err)
			replyError(w, http.StatusInternalServerError, err.Error())
		default:
			reply(w, http.StatusOK, snap)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		replyError(w, http.StatusNotFound, "no "+r.Method+" "+r.URL.Path)
	})
	return mux
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func replyError(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
