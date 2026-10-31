package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Restart is when the supervisor brings waker back after it exits.
type Restart string

const (
	// RestartAlways restarts after any exit the supervisor didn't ask for.
	RestartAlways Restart = "always"
	// RestartOnFailure restarts after a crash; a clean exit stays down.
	RestartOnFailure Restart = "on-failure"
	// RestartNever leaves restarts to you (your own supervisor).
	RestartNever Restart = "never"
)

// DefaultRestart is service.restart's default: waker fronts every app, so
// it stays up.
const DefaultRestart = RestartAlways

// UserBinDir is the per-user binary directory a user service's default
// PATH starts with (the installer puts waker there).
const UserBinDir = "~/.local/bin"

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Service is how `waker service install` runs waker. The supervisor reads
// the unit it writes, not this block: a change applies on the next
// `waker service install`.
type Service struct {
	// Restart: always, on-failure or never.
	Restart Restart `yaml:"restart"`
	// Path is the service's PATH, where start and stop commands are found.
	Path string `yaml:"path"`
	// System installs a system-wide service (root) instead of a user one.
	System bool `yaml:"system"`
	// Start (re)starts the service on install; false only writes and
	// enables it.
	Start *bool `yaml:"start"`
	// Env is extra environment for the waker process (and the apps it starts).
	Env map[string]string `yaml:"env,omitempty"`
	// User is the account a system service runs as; empty = the installing
	// user ($SUDO_USER under sudo).
	User string `yaml:"user,omitempty"`

	// pathSet records a path written in the file (or SetPath): the default
	// follows system, a written path doesn't.
	pathSet bool
}

// SetPath sets an explicit service PATH (a --path flag).
func (s *Service) SetPath(p string) { s.Path, s.pathSet = p, true }

// Normalize re-derives what depends on other keys after a flag changed
// one (the default path follows system), then validates the block.
func (s Service) Normalize() (Service, error) {
	if !s.pathSet {
		s.Path = ""
	}
	s.applyDefaults()
	var errs []error
	s.validate(func(field, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", field, fmt.Sprintf(format, args...)))
	})
	return s, errors.Join(errs...)
}

// Starts reports whether install (re)starts the service.
func (s Service) Starts() bool { return s.Start == nil || *s.Start }

// DefaultServicePath is service.path's default: for a user service,
// ~/.local/bin then the system PATH; for a system service, the system
// PATH alone (whose ~ would that be?).
func DefaultServicePath(system bool) string {
	dirs := host.ServicePATH
	if !system {
		dirs = append([]string{UserBinDir}, dirs...)
	}
	return strings.Join(dirs, ":")
}

func (s *Service) applyDefaults() {
	if s.Restart == "" {
		s.Restart = DefaultRestart
	}
	if s.Path == "" {
		s.Path = DefaultServicePath(s.System)
	} else {
		s.pathSet = true
	}
	s.expandHome()
}

// expandHome expands ~ and a leading ~/ in each PATH entry of a user
// service (a system service's ~ is refused by validate). Defaults do it,
// so Parse and Load agree.
func (s *Service) expandHome() {
	if s.System {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return // validate then names the entry that isn't absolute
	}
	dirs := strings.Split(s.Path, ":")
	for i, d := range dirs {
		if d == "~" {
			dirs[i] = home
		} else if rest, ok := strings.CutPrefix(d, "~/"); ok {
			dirs[i] = filepath.Join(home, rest)
		}
	}
	s.Path = strings.Join(dirs, ":")
}

func (s Service) validate(add addFunc) {
	switch s.Restart {
	case RestartAlways, RestartOnFailure, RestartNever:
	default:
		add("service.restart", "%q must be always, on-failure or never", s.Restart)
	}
	for _, d := range strings.Split(s.Path, ":") {
		switch {
		case d == "":
			add("service.path", "has an empty entry (it would mean the working directory)")
		case s.System && strings.HasPrefix(d, "~"):
			add("service.path", "%q: a system service has no ~; write the absolute path", d)
		case !filepath.IsAbs(d):
			add("service.path", "%q is not an absolute directory", d)
		}
	}
	for k := range s.Env {
		switch {
		case !envNamePattern.MatchString(k):
			add("service.env."+k, "is not a valid environment variable name")
		case k == "PATH":
			add("service.env.PATH", "set service.path instead")
		}
	}
	if s.User != "" && !s.System {
		add("service.user", "only applies with system: true (a user service runs as you)")
	}
}
