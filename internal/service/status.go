package service

import (
	"fmt"
	"io"
)

// Status is what one scope's service looks like right now.
type Status struct {
	Manager   string // systemd | launchd
	Scope     string // user | system
	Unit      string // the unit or plist path
	Installed bool
	Running   bool
	Enabled   bool
	State     string
	PID       int
	// Logs is a command (journalctl …) or a log file path.
	Logs  string
	Notes []string
}

// Statuses reports the user and the system service, in that order.
func Statuses(h Host, r Runner) ([]Status, error) {
	m, err := managerFor(h)
	if err != nil {
		return nil, err
	}
	return []Status{m.status(false, r), m.status(true, r)}, nil
}

// Print writes the status as aligned lines.
func (s Status) Print(w io.Writer) {
	if !s.Installed {
		fmt.Fprintf(w, "%s service (%s): not installed\n", s.Scope, s.Manager)
		return
	}
	fmt.Fprintf(w, "%s service (%s)\n", s.Scope, s.Manager)
	fmt.Fprintf(w, "  unit     %s\n", s.Unit)
	state := s.State
	if s.PID > 0 {
		state = fmt.Sprintf("%s, pid %d", state, s.PID)
	}
	fmt.Fprintf(w, "  state    %s\n", state)
	fmt.Fprintf(w, "  enabled  %t\n", s.Enabled)
	if s.Logs != "" {
		fmt.Fprintf(w, "  logs     %s\n", s.Logs)
	}
	for _, n := range s.Notes {
		fmt.Fprintf(w, "  note     %s\n", n)
	}
}
