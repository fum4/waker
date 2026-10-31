package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fum4/waker/internal/config"
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
		},
	}
	if !sp.NoStart {
		// restart, not start: a reinstall applies the new unit.
		p.Post = append(p.Post, Command{Argv: s.ctl(sp.System, "restart", UnitName)})
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

// renderUnit writes the unit. waker fronts every app, so by default it
// must stay up: Restart=always (service.restart) brings it back after any
// exit systemd didn't ask for, and StartLimitIntervalSec=0 keeps a crash
// loop (a port still held) from leaving it dead for good; RestartSec
// bounds the churn. A stop through systemctl stays stopped.
func renderUnit(sp Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", Marker)
	b.WriteString("[Unit]\nDescription=waker: scale-to-zero front for self-hosted apps\n")
	b.WriteString("After=network.target\nStartLimitIntervalSec=0\n\n[Service]\n")
	// exec, not simple: a binary that can't be executed fails the start.
	b.WriteString("Type=exec\n")
	fmt.Fprintf(&b, "ExecStart=%s\n", execLine(sp.Binary, "serve", "--config", sp.Config))
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", unitQuote(sp.WorkDir, false))
	// The supervisor's PATH is minimal; service.path is where the start
	// commands (npm, python3, …) are found.
	fmt.Fprintf(&b, "Environment=%s\n", unitQuote("PATH="+sp.PATH, false))
	for _, kv := range sp.envPairs() {
		fmt.Fprintf(&b, "Environment=%s\n", unitQuote(kv, false))
	}
	if sp.System {
		if sp.RunAs != "" {
			fmt.Fprintf(&b, "User=%s\n", sp.RunAs)
		}
		// No XDG_RUNTIME_DIR here: a fixed socket the CLI falls back to.
		b.WriteString("RuntimeDirectory=waker\nRuntimeDirectoryMode=0750\n")
		fmt.Fprintf(&b, "Environment=%s\n", unitQuote(config.EnvControlSocket+"="+config.SystemControlSocket, false))
	}
	fmt.Fprintf(&b, "Restart=%s\nRestartSec=2\n", systemdRestart(sp.Restart))
	b.WriteString("# SIGTERM lets waker drain connections and stop the apps it started.\n")
	b.WriteString("TimeoutStopSec=60\nNoNewPrivileges=true\n\n[Install]\n")
	if sp.System {
		b.WriteString("WantedBy=multi-user.target\n")
	} else {
		b.WriteString("WantedBy=default.target\n")
	}
	return b.String()
}

// systemdRestart maps service.restart to systemd's Restart=; empty (a Spec
// built without a config) is the default.
func systemdRestart(r config.Restart) string {
	switch r {
	case config.RestartOnFailure:
		return "on-failure"
	case config.RestartNever:
		return "no"
	}
	return "always"
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
