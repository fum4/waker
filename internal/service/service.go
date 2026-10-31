// Package service installs, removes and inspects waker as a supervised
// service: a systemd unit on Linux, a launchd job on macOS. It plans every
// change as files plus commands, so --dry-run prints exactly what install
// would do, and tests run against a fake host and command runner.
package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/fum4/waker/internal/config"
)

// Marker opens every file install writes; uninstall refuses to remove a
// file without it (it isn't ours) unless forced.
const Marker = "Written by `waker service install`; `waker service uninstall` removes it."

// Spec is the service to install.
type Spec struct {
	// Binary and Config are absolute paths; the service runs
	// `<Binary> serve --config <Config>`.
	Binary string
	Config string
	// WorkDir is the service's working directory: the config file's, so a
	// relative start command works as it does from that directory.
	WorkDir string
	// PATH is the service's PATH: the installer's own, so the start
	// commands that work in the shell work under the supervisor too.
	PATH string
	// System installs a system-wide service (root) instead of a user one.
	System bool
	// RunAs is the account a system service runs as.
	RunAs string
	// NoStart installs and enables the service without starting or
	// restarting it: the caller decides when the new unit takes over.
	NoStart bool
	// Restart is the supervisor's restart policy (config.Restart*).
	Restart config.Restart
	// Env is extra environment for the waker process.
	Env map[string]string
}

// envPairs returns Env as sorted NAME=value pairs, so the rendered unit
// (and the drift check comparing it) is stable.
func (s Spec) envPairs() []string {
	pairs := make([]string, 0, len(s.Env))
	for k, v := range s.Env {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return pairs
}

// Host is what the planner knows about the machine: the real one in
// production (CurrentHost), a fake in tests.
type Host struct {
	OS   string // runtime.GOOS
	Home string
	User string
	UID  int
	EUID int
	// ConfigHome is $XDG_CONFIG_HOME, else ~/.config.
	ConfigHome string
	// Root prefixes every system path (/etc, /run, /var, /Library); empty
	// in production, a temp dir in tests.
	Root string
}

// CurrentHost describes the machine waker runs on.
func CurrentHost() (Host, error) {
	u, err := user.Current()
	if err != nil {
		return Host{}, fmt.Errorf("current user: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Host{}, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	cfgHome := os.Getenv("XDG_CONFIG_HOME")
	if cfgHome == "" {
		cfgHome = filepath.Join(home, ".config")
	}
	return Host{
		OS: runtime.GOOS, Home: home, User: u.Username, UID: uid, EUID: os.Geteuid(),
		ConfigHome: cfgHome,
	}, nil
}

// sys is a system path under the host's root.
func (h Host) sys(p string) string { return filepath.Join(h.Root, p) }

// Command is one command a plan runs. Ignore tolerates its failure (a
// stop of a service that isn't loaded).
type Command struct {
	Argv   []string
	Ignore bool
}

func (c Command) String() string { return ShellJoin(c.Argv) }

// File is one file a plan writes.
type File struct {
	Path    string
	Content string
}

// Plan is every change one install or uninstall makes, in order: Pre
// commands, Files written (MkDirs first) or Removes deleted, Post commands.
type Plan struct {
	Unit    string
	Pre     []Command
	MkDirs  []string
	Files   []File
	Removes []string
	Post    []Command
	// Notes are what the owner should know or do next (a sudo line).
	Notes []string
}

// Runner runs a command and returns its combined output.
type Runner interface {
	Run(argv ...string) (string, error)
}

// ExecRunner runs commands for real.
type ExecRunner struct{}

// Run executes argv and returns its combined output.
func (ExecRunner) Run(argv ...string) (string, error) {
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	return string(out), err
}

// ErrUnsupported is returned on a host with no supported service manager.
var ErrUnsupported = errors.New("no supported service manager")

// manager is one service manager: systemd or launchd.
type manager interface {
	unitPath(system bool) string
	install(Spec) (Plan, error)
	uninstall(system bool) Plan
	status(system bool, r Runner) Status
}

// ManualSteps is what to do by hand where no manager is supported.
func ManualSteps(s Spec) string {
	return fmt.Sprintf(`Run waker under your init system or process supervisor:
  command:  %s
  env:      %s
  restart:  %s, with no start limit
  logs:     stderr`, ShellJoin([]string{s.Binary, "serve", "--config", s.Config}),
		strings.Join(append([]string{"PATH=" + s.PATH}, s.envPairs()...), " "), s.Restart)
}

func managerFor(h Host) (manager, error) {
	switch h.OS {
	case "linux":
		if st, err := os.Stat(h.sys("/run/systemd/system")); err != nil || !st.IsDir() {
			return nil, fmt.Errorf("%w: this Linux host doesn't run systemd", ErrUnsupported)
		}
		return systemd{h}, nil
	case "darwin":
		return launchd{h}, nil
	}
	return nil, fmt.Errorf("%w on %s", ErrUnsupported, h.OS)
}

// ShellJoin renders argv as one copy-pasteable shell line.
func ShellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@%+,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
