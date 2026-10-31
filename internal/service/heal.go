package service

import (
	"fmt"
	"os"
	"strings"
)

// supervisorKeys are the [Service] settings systemd reads when the process
// exits, not when it starts: a daemon-reload applies them to the running
// service, so changing only these needs no restart. A denylist on
// purpose: a key missing here costs one extra restart, while an allowlist
// missing a key would leave the old process running a setting it lacks.
var supervisorKeys = []string{"Restart=", "RestartSec=", "TimeoutStopSec="}

// Heal is how serve brings its stale waker-written unit in line with
// waker.yaml from inside the running service.
type Heal struct {
	// Plan writes the unit, reloads systemd and enables it, the way
	// install does with NoStart: nothing that stops or starts it.
	Plan Plan
	// Old is the unit as installed, for putting it back when Plan fails
	// halfway (a written file that systemd never loaded would look
	// current and never be healed or warned about again).
	Old string
	// Reexec reports whether the running process differs from the one
	// the new unit starts (its command, working directory, environment,
	// user): only a restart applies those.
	Reexec bool
}

// PlanHeal plans the heal of the unit installed for sp's scope. The caller
// has checked the drift is DriftStale; launchd caches the job at
// bootstrap, so it is ErrUnsupported there (`waker restart` applies it).
func PlanHeal(h Host, sp Spec) (Heal, error) {
	if h.OS != "linux" {
		return Heal{}, fmt.Errorf("%w: self-heal on %s", ErrUnsupported, h.OS)
	}
	s := systemd{h}
	sp.NoStart = true
	p, err := s.install(sp)
	if err != nil {
		return Heal{}, err
	}
	p.Notes = nil // linger: install's advice, not a running service's
	old, err := os.ReadFile(p.Unit)
	if err != nil {
		return Heal{}, err
	}
	return Heal{Plan: p, Old: string(old), Reexec: processLines(string(old)) != processLines(p.Files[0].Content)}, nil
}

// IsMainProcess reports whether pid is the main process of the service
// installed in the scope: a `waker serve` run by hand, or started by
// anything under the service (an app's own script), is not.
func IsMainProcess(h Host, system bool, r Runner, pid int) bool {
	m, err := managerFor(h)
	if err != nil {
		return false
	}
	return m.status(system, r).PID == pid
}

// processLines is a unit's [Service] section without comments and the
// settings systemd applies to a running service (supervisorKeys).
func processLines(unit string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			in = line == "[Service]"
			continue
		}
		if !in || line == "" || strings.HasPrefix(line, "#") || hasAnyPrefix(line, supervisorKeys) {
			continue
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
