package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Label is the launchd job label install writes.
const Label = "io.nomadware.waker"

type launchd struct{ h Host }

func (l launchd) unitPath(system bool) string {
	if system {
		return l.h.sys("/Library/LaunchDaemons/" + Label + ".plist")
	}
	return filepath.Join(l.h.Home, "Library", "LaunchAgents", Label+".plist")
}

func (l launchd) logDir(system bool) string {
	if system {
		return l.h.sys("/Library/Logs/waker")
	}
	return filepath.Join(l.h.Home, "Library", "Logs", "waker")
}

// domain is the launchctl domain target: the login session for an agent,
// the system for a daemon.
func (l launchd) domain(system bool) string {
	if system {
		return "system"
	}
	return "gui/" + strconv.Itoa(l.h.UID)
}

func (l launchd) target(system bool) string { return l.domain(system) + "/" + Label }

func (l launchd) install(sp Spec) (Plan, error) {
	plist := l.unitPath(sp.System)
	logs := l.logDir(sp.System)
	p := Plan{
		Unit:   plist,
		MkDirs: []string{logs},
		Files:  []File{{Path: plist, Content: renderPlist(sp, filepath.Join(logs, "waker.log"))}},
	}
	bootstrap := []string{"launchctl", "bootstrap", l.domain(sp.System), plist}
	if sp.NoStart {
		// launchd has no enable without a load: the job loads at the next
		// login (agent) or boot (daemon).
		p.Notes = append(p.Notes, "--no-start: the job loads at the next login or boot; load it now with:\n  "+ShellJoin(bootstrap))
		return p, nil
	}
	// bootout first: bootstrap refuses a job that is already loaded.
	p.Pre = []Command{{Argv: []string{"launchctl", "bootout", l.target(sp.System)}, Ignore: true}}
	p.Post = []Command{{Argv: bootstrap}}
	return p, nil
}

func (l launchd) uninstall(system bool) Plan {
	plist := l.unitPath(system)
	return Plan{
		Unit:    plist,
		Pre:     []Command{{Argv: []string{"launchctl", "bootout", l.target(system)}, Ignore: true}},
		Removes: []string{plist, l.logDir(system)},
	}
}

var (
	launchdState = regexp.MustCompile(`(?m)^\s*state = (\S+)`)
	launchdPID   = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)
)

func (l launchd) status(system bool, r Runner) Status {
	st := Status{
		Manager: "launchd", Unit: l.unitPath(system), Scope: scopeName(system),
		Logs: filepath.Join(l.logDir(system), "waker.log"),
	}
	if _, err := os.Stat(st.Unit); err != nil {
		st.Logs = ""
		return st
	}
	st.Installed = true
	out, err := r.Run("launchctl", "print", l.target(system))
	if err != nil {
		st.State = "not loaded"
		return st
	}
	st.Enabled = true // loaded with RunAtLoad: it starts at login / boot
	if m := launchdState.FindStringSubmatch(out); m != nil {
		st.State = m[1]
		st.Running = m[1] == "running"
	}
	if m := launchdPID.FindStringSubmatch(out); m != nil {
		st.PID, _ = strconv.Atoi(m[1])
	}
	return st
}

// renderPlist writes the job. waker fronts every app, so KeepAlive=true
// relaunches it after any exit; launchctl bootout leaves it stopped.
func renderPlist(sp Spec, logFile string) string {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	fmt.Fprintf(&b, "<!-- %s -->\n", Marker)
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	key(&b, "Label", Label)
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range []string{sp.Binary, "serve", "--config", sp.Config} {
		fmt.Fprintf(&b, "    <string>%s</string>\n", esc(a))
	}
	b.WriteString("  </array>\n")
	key(&b, "WorkingDirectory", sp.WorkDir)
	b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
	fmt.Fprintf(&b, "    <key>PATH</key>\n    <string>%s</string>\n  </dict>\n", esc(sp.PATH))
	if sp.System && sp.RunAs != "" {
		key(&b, "UserName", sp.RunAs)
	}
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	b.WriteString("  <key>ExitTimeOut</key>\n  <integer>60</integer>\n")
	key(&b, "StandardOutPath", logFile)
	key(&b, "StandardErrorPath", logFile)
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func key(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, "  <key>%s</key>\n  <string>%s</string>\n", k, esc(v))
}

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
