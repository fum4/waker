package app

import (
	"context"
	"testing"
	"time"
)

func TestAwaitBoot(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, f *fixture)
		timeout time.Duration
		want    State
	}{
		{"no boot: current state", func(t *testing.T, f *fixture) {}, time.Second, Down},
		{"boot ends up", func(t *testing.T, f *fixture) { f.app.Wake(ctx) }, 5 * time.Second, Up},
		{"boot fails", func(t *testing.T, f *fixture) { f.proc.exitErr = errExit; f.app.Wake(ctx) }, 5 * time.Second, Failed},
		{"timeout: still starting", func(t *testing.T, f *fixture) {
			f.proc.gate = make(chan struct{})
			t.Cleanup(func() { close(f.proc.gate) })
			f.app.Wake(ctx)
		}, 50 * time.Millisecond, Starting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			tt.setup(t, f)
			c, cancel := context.WithTimeout(ctx, tt.timeout)
			defer cancel()
			if got := f.app.AwaitBoot(c); got != tt.want {
				t.Fatalf("AwaitBoot = %s, want %s", got, tt.want)
			}
		})
	}
}
