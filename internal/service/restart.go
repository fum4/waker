package service

import (
	"errors"
	"fmt"
)

// ErrNotInstalled is returned by PlanRestart when no unit is installed in
// the spec's scope.
var ErrNotInstalled = errors.New("no waker service is installed")

// PlanRestart returns the plan that restarts the installed service, and
// how it compared with sp. A waker-written unit that sp would write
// differently (DriftStale) is rewritten first, the way install writes it,
// so the restart runs what waker.yaml says now. A unit waker didn't write
// is restarted as it is.
func PlanRestart(h Host, sp Spec) (Plan, Drift, error) {
	m, err := managerFor(h)
	if err != nil {
		return Plan{}, DriftNotInstalled, err
	}
	d, unit, err := CheckDrift(h, sp)
	if err != nil {
		return Plan{}, d, err
	}
	switch d {
	case DriftNotInstalled:
		return Plan{}, d, fmt.Errorf("%w (%s): install it with `waker service install`", ErrNotInstalled, unit)
	case DriftStale:
		sp.NoStart = false // a restart always restarts; start: false is install's
		p, err := m.install(sp)
		return p, d, err
	}
	return m.restart(sp.System), d, nil
}

// restart restarts the unit as installed: systemctl restart starts a
// stopped one too.
func (s systemd) restart(system bool) Plan {
	return Plan{
		Unit: s.unitPath(system),
		Post: []Command{{Argv: s.ctl(system, "restart", UnitName)}},
	}
}

// restart reloads the job: bootout (ignored when it isn't loaded, e.g.
// after --no-start) then bootstrap, so it also starts a job that isn't
// loaded, which `launchctl kickstart -k` can't.
func (l launchd) restart(system bool) Plan {
	return Plan{
		Unit: l.unitPath(system),
		Pre:  []Command{{Argv: []string{"launchctl", "bootout", l.target(system)}, Ignore: true}},
		Post: []Command{{Argv: []string{"launchctl", "bootstrap", l.domain(system), l.unitPath(system)}}},
	}
}
