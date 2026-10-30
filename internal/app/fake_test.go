package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/fum4/waker/internal/clock"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/state"
)

// fakeProcess is a scripted app: Start optionally blocks on gate, then the
// app is up if comesUp; exitErr is what the start command "exits" with.
type fakeProcess struct {
	mu        sync.Mutex
	up        bool
	comesUp   bool
	exitErr   error
	panicked  bool // Healthy panics once the start ran
	gate      chan struct{}
	runs      []string
	startedCh chan struct{}
}

func newFake() *fakeProcess { return &fakeProcess{comesUp: true} }

func (f *fakeProcess) Start() (<-chan error, error) {
	f.mu.Lock()
	f.runs = append(f.runs, "start")
	gate, started := f.gate, f.startedCh
	f.mu.Unlock()
	if started != nil {
		close(started)
	}
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	exit := make(chan error, 1)
	if f.exitErr != nil {
		exit <- f.exitErr
	} else {
		f.up = f.comesUp
	}
	return exit, nil
}

func (f *fakeProcess) Healthy(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panicked && len(f.runs) > 0 {
		panic("probe exploded")
	}
	return f.up
}

func (f *fakeProcess) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, "stop")
	f.up = false
	return nil
}

func (f *fakeProcess) setUp(up bool) { f.mu.Lock(); f.up = up; f.mu.Unlock() }

func (f *fakeProcess) Runs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.runs...)
}

type fixture struct {
	app      *App
	proc     *fakeProcess
	clock    *clock.Fake
	notified chan struct{}
	saved    map[string]*state.Override
}

func newFixture(t *testing.T, mutate func(*config.App)) *fixture {
	t.Helper()
	cfg, err := config.Parse([]byte("apps: [{name: shop, listen: '127.0.0.1:1', upstream: 'http://127.0.0.1:2', start: [x]}]"))
	if err != nil {
		t.Fatal(err)
	}
	ac := cfg.Apps[0]
	if mutate != nil {
		mutate(&ac)
	}
	f := &fixture{proc: newFake(), clock: clock.NewFake(), notified: make(chan struct{}, 100), saved: map[string]*state.Override{}}
	f.app = New(ac, Deps{
		Clock:   f.clock,
		Process: f.proc,
		Notify: func() {
			select {
			case f.notified <- struct{}{}:
			default:
			}
		},
		Persist: func(name string, o *state.Override) error { f.saved[name] = o; return nil },
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, nil)
	t.Cleanup(f.app.Wait)
	return f
}

func (f *fixture) drainNotified() {
	for {
		select {
		case <-f.notified:
		default:
			return
		}
	}
}

func assertRuns(t *testing.T, f *fixture, want ...string) {
	t.Helper()
	got := f.proc.Runs()
	if len(got) != len(want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("runs = %v, want %v", got, want)
		}
	}
}

var errExit = errors.New("exit status 1")
