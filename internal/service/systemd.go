package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UnitName is the systemd unit install writes.
const UnitName = "waker.service"

type systemd struct{ h Host }

func (s systemd) unitPath(system bool) string {
	if system {
		return s.h.sys("/etc/systemd/system/" + UnitName)
	}
	return filepath.Join(s.h.ConfigHome, "systemd", "user", UnitName)
}

// ctl is systemctl for the scope.
func (s systemd) ctl(system bool, args ...string) []string {
	if system {
		return append([]string{"systemctl"}, args...)
	}
	return append([]string{"systemctl", "--user"}, args...)
}

func (s systemd) install(sp Spec) (Plan, error) {
	unit := s.unitPath(sp.System)
	p := Plan{
		Unit:  unit,
		Files: []File{{Path: unit, Content: renderUnit(sp)}},
		Post: []Command{
			{Argv: s.ctl(sp.System, "daemon-reload")},
			{Argv: s.ctl(sp.System, "enable", UnitName)},
			// restart, not start: a reinstall applies the new unit.
			{Argv: s.ctl(sp.System, "restart", UnitName)},
		},
	}
	if !sp.System && !s.lingering() {
		p.Notes = append(p.Notes, "linger is off: the service stops when your last session ends. Keep it running with:\n  "+s.lingerLine())
	}
	return p, nil
}

func (s systemd) uninstall(system bool) Plan {
	unit := s.unitPath(system)
	return Plan{
		Unit:    unit,
		Pre:     []Command{{Argv: s.ctl(system, "disable", "--now", UnitName), Ignore: true}},
		Removes: []string{unit},
		Post:    []Command{{Argv: s.ctl(system, "daemon-reload")}},
	}
}

func (s systemd) status(system bool, r Runner) Status {
	st := Status{Manager: "systemd", Unit: s.unitPath(system), Scope: scopeName(system)}
	if _, err := os.Stat(st.Unit); err != nil {
		return st
	}
	st.Installed = true
	out, _ := r.Run(s.ctl(system, "show", UnitName, "--property=ActiveState,SubState,MainPID,UnitFileState")...)
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	st.State = props["ActiveState"]
	if sub := props["SubState"]; sub != "" {
		st.State += " (" + sub + ")"
	}
	st.Running = props["ActiveState"] == "active"
	st.PID, _ = strconv.Atoi(props["MainPID"])
	st.Enabled = props["UnitFileState"] == "enabled"
	journal := []string{"journalctl", "--user", "-u", UnitName, "-f"}
	if system {
		journal = []string{"journalctl", "-u", UnitName, "-f"}
	}
	st.Logs = ShellJoin(journal)
	if !system && !s.lingering() {
		st.Notes = append(st.Notes, "linger is off (the service stops at logout): "+s.lingerLine())
	}
	return st
}

// lingering reads logind's linger flag file, the same one loginctl writes.
func (s systemd) lingering() bool {
	_, err := os.Stat(s.h.sys(filepath.Join("/var/lib/systemd/linger", s.h.User)))
	return err == nil
}

func (s systemd) lingerLine() string {
	return ShellJoin([]string{"sudo", "loginctl", "enable-linger", s.h.User})
}

// renderUnit writes the unit. Restart=on-failure brings waker back after a
// crash; a clean stop (systemctl stop, SIGTERM) stays stopped.
func renderUnit(sp Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", Marker)
	b.WriteString("[Unit]\nDescription=waker: scale-to-zero front for self-hosted apps\n")
	b.WriteString("After=network.target\n\n[Service]\n")
	fmt.Fprintf(&b, "ExecStart=%s\n", execLine(sp.Binary, "serve", "--config", sp.Config))
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", unitQuote(sp.WorkDir, false))
	// The supervisor's PATH is minimal; the installer's PATH is what the
	// start commands (npm, python3, …) were found on.
	fmt.Fprintf(&b, "Environment=%s\n", unitQuote("PATH="+sp.PATH, false))
	if sp.System && sp.RunAs != "" {
		fmt.Fprintf(&b, "User=%s\n", sp.RunAs)
	}
	b.WriteString("Restart=on-failure\nRestartSec=2\n")
	b.WriteString("# SIGTERM lets waker drain connections and stop the apps it started.\n")
	b.WriteString("TimeoutStopSec=60\nNoNewPrivileges=true\n\n[Install]\n")
	if sp.System {
		b.WriteString("WantedBy=multi-user.target\n")
	} else {
		b.WriteString("WantedBy=default.target\n")
	}
	return b.String()
}

func execLine(argv ...string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = unitQuote(a, true)
	}
	return strings.Join(q, " ")
}

// unitQuote double-quotes a value for a unit file: backslash and quote are
// escaped, % is a specifier (%%), and in ExecStart $ expands ($$).
func unitQuote(s string, exec bool) string {
	r := []string{`\`, `\\`, `"`, `\"`, "%", "%%"}
	if exec {
		r = append(r, "$", "$$")
	}
	return `"` + strings.NewReplacer(r...).Replace(s) + `"`
}

func scopeName(system bool) string {
	if system {
		return "system"
	}
	return "user"
}
