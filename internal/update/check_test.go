package update_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fum4/waker/internal/state"
	"github.com/fum4/waker/internal/update"
	"github.com/fum4/waker/internal/update/updatetest"
)

// counting counts the requests a source makes.
type counting struct{ n atomic.Int32 }

func (c *counting) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return http.DefaultTransport.RoundTrip(r)
}

func newCheck(t *testing.T) (*updatetest.Home, *update.Check, *counting, *time.Time) {
	t.Helper()
	h := updatetest.New(t)
	src := h.Source()
	cnt := &counting{}
	src.Client = &http.Client{Transport: cnt}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	c := &update.Check{Source: src, StateDir: t.TempDir(), Now: func() time.Time { return now }}
	return h, c, cnt, &now
}

func TestCheckAsksAtMostOnceADay(t *testing.T) {
	h, c, cnt, now := newCheck(t)
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	if tag, ok := c.Latest(context.Background()); !ok || tag.String() != "v0.4.0" {
		t.Fatalf("first check = %v, %v", tag, ok)
	}
	h.Publish("v0.5.0", updatetest.Script("v0.5.0"))
	*now = now.Add(update.CheckEvery - time.Minute)
	if tag, _ := c.Latest(context.Background()); tag.String() != "v0.4.0" {
		t.Errorf("within a day = %v, want the recorded v0.4.0", tag)
	}
	if n := cnt.n.Load(); n != 1 {
		t.Errorf("%d requests within a day, want 1", n)
	}
	*now = now.Add(time.Minute)
	if tag, _ := c.Latest(context.Background()); tag.String() != "v0.5.0" {
		t.Errorf("a day later = %v, want v0.5.0", tag)
	}
	rec, err := state.LoadUpdate(c.StateDir)
	if err != nil || rec.Latest != "v0.5.0" || !rec.CheckedAt.Equal(*now) {
		t.Errorf("update.json = %+v, %v", rec, err)
	}
}

// A failed request still uses up the day: an offline box isn't slowed
// down by every command.
func TestCheckFailureIsSilentAndCapped(t *testing.T) {
	_, c, cnt, _ := newCheck(t) // nothing published: latest is a 404
	for range 3 {
		if tag, ok := c.Latest(context.Background()); ok {
			t.Fatalf("= %v with nothing published", tag)
		}
	}
	if n := cnt.n.Load(); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

// A failed check keeps the last tag it heard.
func TestCheckKeepsTheLastTagOnFailure(t *testing.T) {
	h, c, _, now := newCheck(t)
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	c.Latest(context.Background())
	if err := os.Remove(filepath.Join(h.Dir, update.Product, "latest")); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(update.CheckEvery)
	if tag, ok := c.Latest(context.Background()); !ok || tag.String() != "v0.4.0" {
		t.Errorf("= %v, %v; want the recorded v0.4.0", tag, ok)
	}
}

// A state dir it can't write means it can't cap itself: it stays offline.
func TestCheckStaysOfflineWithoutItsStateDir(t *testing.T) {
	h, c, cnt, _ := newCheck(t)
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c.StateDir = filepath.Join(blocker, "state") // under a file: never creatable
	if _, ok := c.Latest(context.Background()); ok || cnt.n.Load() != 0 {
		t.Errorf("ok = %v after %d requests; want no tag, no request", ok, cnt.n.Load())
	}
}

// A clock set back (or a record from the future) doesn't block checks.
func TestCheckRecordFromTheFutureIsDue(t *testing.T) {
	h, c, cnt, now := newCheck(t)
	h.Publish("v0.4.0", updatetest.Script("v0.4.0"))
	if err := state.SaveUpdate(c.StateDir, state.Update{CheckedAt: now.Add(time.Hour), Latest: "v0.3.0"}); err != nil {
		t.Fatal(err)
	}
	if tag, _ := c.Latest(context.Background()); tag.String() != "v0.4.0" || cnt.n.Load() != 1 {
		t.Errorf("= %v after %d requests", tag, cnt.n.Load())
	}
}

func TestAvailable(t *testing.T) {
	v4, _ := update.ParseTag("v0.4.0")
	for _, tc := range []struct{ cur, want string }{
		{"0.3.0", "waker v0.4.0 available (running v0.3.0): waker update"},
		{"v0.4.0-0.20261030-abcdef", "waker v0.4.0 available (running v0.4.0-…): waker update"},
		{"0.4.0", ""},
		{"0.5.0", ""},
		{"dev", ""},
	} {
		if got := update.Available(update.ParseVersion(tc.cur), v4); got != tc.want {
			t.Errorf("Available(%s) = %q, want %q", tc.cur, got, tc.want)
		}
	}
}

func TestUnattended(t *testing.T) {
	for v, want := range map[string]bool{"": true, "0": true, "1": false, "yes": false} {
		t.Setenv(update.EnvNoCheck, v)
		if got := update.Unattended(); got != want {
			t.Errorf("%s=%q: Unattended = %v, want %v", update.EnvNoCheck, v, got, want)
		}
	}
}
