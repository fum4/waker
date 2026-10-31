package service

import (
	"fmt"
	"os"
	"strings"
)

// supervisorKeys are the [Service] settings systemd reads when the process
// exits, not when it starts: a daemon-reload applies them to the running
// service, so changing only these needs no restart.
var supervisorKeys = []string{"Restart=", "RestartSec=", "TimeoutStopSec="}

// PlanHeal returns the plan that brings a stale waker-written unit in line
// with sp from inside the running service: the unit and a daemon-reload,
// nothing that stops, starts or enables it. reexec reports whether the
// running process differs from the one the new unit starts (its command,
// working directory, environment, user): only a restart applies those.
// The caller has checked the drift is DriftStale; launchd caches the job
// at bootstrap, so it is ErrUnsupported there (`waker restart` applies it).
func PlanHeal(h Host, sp Spec) (plan Plan, reexec bool, err error) {
	if h.OS != "linux" {
		return Plan{}, false, fmt.Errorf("%w: self-heal on %s", ErrUnsupported, h.OS)
	}
	s := systemd{h}
	unit := s.unitPath(sp.System)
	old, err := os.ReadFile(unit)
	if err != nil {
		return Plan{}, false, err
	}
	content := renderUnit(sp)
	return Plan{
		Unit:  unit,
		Files: []File{{Path: unit, Content: content}},
		Post:  []Command{{Argv: s.ctl(sp.System, "daemon-reload")}},
	}, processLines(string(old)) != processLines(content), nil
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

// InUnit reports whether a process whose /proc/<pid>/cgroup reads cgroup
// is the service install writes, in the scope given: systemd puts a
// service's processes in a cgroup named after the unit, a user service's
// under user@<uid>.service. A `waker serve` run by hand (in a shell, or
// under another unit) is not, and never rewrites the unit.
func InUnit(cgroup string, system bool) bool {
	for _, line := range strings.Split(strings.TrimSpace(cgroup), "\n") {
		// cgroup v2 is "0::<path>"; v1 lists one line per controller.
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if !strings.HasSuffix(path, "/"+UnitName) {
			continue
		}
		return strings.Contains(path, "/user@") != system
	}
	return false
}
