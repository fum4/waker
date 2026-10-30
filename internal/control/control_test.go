package control

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
)

type fakeBackend struct{ last *config.Mode }

func (f *fakeBackend) Status(context.Context) Report {
	return Report{Policy: Policy{Eviction: "off"}, Apps: []app.Snapshot{{Name: "shop", State: app.Up}}}
}

func (f *fakeBackend) Health() Health {
	return Health{Listeners: []string{"shop"}, Skipped: map[string]string{"held": "address already in use"}}
}

func (f *fakeBackend) SetMode(name string, mode *config.Mode, idle time.Duration) (app.Snapshot, error) {
	switch {
	case name != "shop":
		return app.Snapshot{}, &NotFoundError{"no app '" + name + "'; apps: shop"}
	case mode != nil && *mode == config.ModeOn && idle > 0:
		return app.Snapshot{}, &app.InputError{Msg: "idle applies to on-demand only"}
	}
	f.last = mode
	return app.Snapshot{Name: name, Mode: config.ModeOff}, nil
}

func serve(t *testing.T) (*Client, *fakeBackend, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run", "control.sock")
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	b := &fakeBackend{}
	srv := &http.Server{Handler: Handler(b, slog.New(slog.NewTextHandler(io.Discard, nil)))}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	return NewClient(path), b, path
}

func TestSocketIsPrivate(t *testing.T) {
	_, _, path := serve(t)
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, %v", st.Mode().Perm(), err)
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", dir.Mode().Perm())
	}
}

func TestASecondWakerIsRefusedAStaleSocketReplaced(t *testing.T) {
	_, _, path := serve(t)
	if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "another waker") {
		t.Fatalf("second Listen = %v", err)
	}
	stale := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := Listen(stale)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	ln.Close()
}

func TestRoundTrips(t *testing.T) {
	c, b, _ := serve(t)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil || len(st.Apps) != 1 || st.Apps[0].State != app.Up || st.Policy.Eviction != "off" {
		t.Fatalf("status = %+v, %v", st, err)
	}
	h, err := c.Health(ctx)
	if err != nil || h.Listeners[0] != "shop" || h.Skipped["held"] == "" {
		t.Fatalf("health = %+v, %v", h, err)
	}
	if _, err := c.SetMode(ctx, ModeRequest{App: "shop", Mode: config.ModeOff}); err != nil || *b.last != config.ModeOff {
		t.Fatalf("set mode: %v", err)
	}
	if _, err := c.SetMode(ctx, ModeRequest{App: "shop", Reset: true}); err != nil || b.last != nil {
		t.Fatalf("reset: %v, last %v", err, b.last)
	}
}

func TestErrorsComeBackAsMessages(t *testing.T) {
	c, _, _ := serve(t)
	ctx := context.Background()
	tests := []struct {
		req  ModeRequest
		want string
	}{
		{ModeRequest{App: "ghost", Mode: config.ModeOn}, "no app 'ghost'; apps: shop"},
		{ModeRequest{App: "shop", Mode: config.ModeOn, Idle: config.Duration(time.Second)}, "on-demand only"},
		{ModeRequest{App: "shop"}, "expected JSON"},
		{ModeRequest{App: "shop", Mode: config.ModeOn, Reset: true}, "expected JSON"},
		{ModeRequest{Mode: config.ModeOn}, "expected JSON"},
	}
	for _, tt := range tests {
		if _, err := c.SetMode(ctx, tt.req); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("SetMode(%+v) = %v, want %q", tt.req, err, tt.want)
		}
	}
}

func TestAMissingSocketSaysTheWakerIsNotRunning(t *testing.T) {
	c := NewClient(filepath.Join(t.TempDir(), "none.sock"))
	if _, err := c.Status(context.Background()); err == nil || !strings.Contains(err.Error(), "isn't running") {
		t.Fatalf("= %v", err)
	}
}
