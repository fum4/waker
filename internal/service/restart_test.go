package service

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// commands is every command a plan runs, in order.
func commands(p Plan) []string {
	var out []string
	for _, c := range append(append([]Command{}, p.Pre...), p.Post...) {
		out = append(out, c.String())
	}
	return out
}

func TestPlanRestart(t *testing.T) {
	for _, h := range []Host{linuxHost(t), darwinHost(t)} {
		t.Run(h.OS, func(t *testing.T) {
			sp := spec()
			if _, _, err := PlanRestart(h, sp); !errors.Is(err, ErrNotInstalled) {
				t.Fatalf("nothing installed: err = %v", err)
			}
			p, err := PlanInstall(h, sp)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.Apply(&fakeRunner{}, io.Discard, false); err != nil {
				t.Fatal(err)
			}

			// Current: only the restart, no file.
			cur, d, err := PlanRestart(h, sp)
			if err != nil || d != DriftNone || len(cur.Files) != 0 {
				t.Fatalf("current = %+v, %v, %v", cur, d, err)
			}
			want := map[string][]string{
				"linux":  {"systemctl --user daemon-reload", "systemctl --user restart waker.service"},
				"darwin": {"launchctl bootout gui/501/io.nomadware.waker", "launchctl bootstrap gui/501 " + cur.Unit},
			}[h.OS]
			if got := commands(cur); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("current runs %q, want %q", got, want)
			}

			// Stale, with NoStart: rewritten, and restarted anyway.
			sp.Restart, sp.NoStart = "never", true
			stale, d, err := PlanRestart(h, sp)
			if err != nil || d != DriftStale || len(stale.Files) != 1 {
				t.Fatalf("stale = %+v, %v, %v", stale, d, err)
			}
			if got := commands(stale); got[len(got)-1] != want[len(want)-1] {
				t.Errorf("stale ends with %q, want %q", got[len(got)-1], want[len(want)-1])
			}

			// Not ours: restarted as it is.
			if err := os.WriteFile(cur.Unit, []byte("mine\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if p, d, err := PlanRestart(h, sp); err != nil || d != DriftNotOurs || len(p.Files) != 0 {
				t.Errorf("not ours = %+v, %v, %v", p, d, err)
			}
		})
	}
}
