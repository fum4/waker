package service

import (
	"errors"
	"os"
	"strings"
)

// Drift is how the installed service compares with the one a Spec would
// install. The supervisor runs the unit, not waker.yaml, so an edited
// service block (or a waker upgrade that renders units differently) takes
// effect only once `waker service install` rewrites the unit.
type Drift int

const (
	// DriftNone: the installed unit is exactly what install would write.
	DriftNone Drift = iota
	// DriftNotInstalled: no unit in this scope.
	DriftNotInstalled
	// DriftNotOurs: a unit without the Marker; waker never judges it.
	DriftNotOurs
	// DriftStale: a waker-written unit that install would now write
	// differently.
	DriftStale
)

// CheckDrift compares the unit installed for sp's scope with the one
// install would write for sp, and returns the unit's path.
func CheckDrift(h Host, sp Spec) (Drift, string, error) {
	m, err := managerFor(h)
	if err != nil {
		return DriftNotInstalled, "", err
	}
	p, err := m.install(sp)
	if err != nil {
		return DriftNotInstalled, "", err
	}
	unit := p.Files[0]
	b, err := os.ReadFile(unit.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return DriftNotInstalled, unit.Path, nil
	case err != nil:
		return DriftNotInstalled, unit.Path, err
	case !strings.Contains(string(b), Marker):
		return DriftNotOurs, unit.Path, nil
	case string(b) != unit.Content:
		return DriftStale, unit.Path, nil
	}
	return DriftNone, unit.Path, nil
}
