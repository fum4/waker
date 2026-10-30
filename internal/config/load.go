package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// EnvConfig names the environment variable that points at the config file.
const EnvConfig = "WAKER_CONFIG"

// DefaultPath is the config file used when neither flag nor env names one.
const DefaultPath = "waker.yaml"

// ResolvePath picks the config path: an explicit flag, then $WAKER_CONFIG,
// then ./waker.yaml.
func ResolvePath(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv(EnvConfig); env != "" {
		return env
	}
	return DefaultPath
}

// Load reads, defaults and validates the config file at path. Relative paths
// inside it (logo, cwd, state_dir, control_socket) resolve against the
// file's directory.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	cfg.resolvePaths(filepath.Dir(abs))
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes YAML strictly (unknown keys are errors) and applies defaults.
// It does not validate; Load does.
func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config is empty")
		}
		return nil, err
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.MaxHeadroom == 0 {
		c.MaxHeadroom = DefaultMaxHeadroom
	}
	if c.MinHeadroom == 0 {
		c.MinHeadroom = DefaultMinHeadroom
	}
	if c.StateDir == "" {
		c.StateDir = defaultStateDir()
	}
	if c.ControlSocket == "" {
		c.ControlSocket = defaultControlSocket(c.StateDir)
	}
	for i := range c.Apps {
		c.Apps[i].applyDefaults()
	}
}

func (a *App) applyDefaults() {
	if a.Mode == "" {
		a.Mode = DefaultMode
	}
	if a.Idle == 0 {
		a.Idle = Duration(DefaultIdle)
	}
	if a.Priority == 0 {
		a.Priority = DefaultPriority
	}
	if a.BootTimeout == 0 {
		a.BootTimeout = Duration(DefaultBootTimeout)
	}
	if a.StopTimeout == 0 {
		a.StopTimeout = Duration(DefaultStopTimeout)
	}
	if a.Health.Path == "" && len(a.Health.Command) == 0 {
		a.Health.Path = DefaultHealthPath
	}
	if a.Health.Interval == 0 {
		a.Health.Interval = Duration(DefaultHealthInterval)
	}
	if a.Health.Timeout == 0 {
		a.Health.Timeout = Duration(DefaultHealthTimeout)
	}
	if a.Peak.Source == "" {
		a.Peak.Source = DefaultPeakSource
	}
}

func (c *Config) resolvePaths(base string) {
	c.StateDir = resolvePath(base, c.StateDir)
	c.ControlSocket = resolvePath(base, c.ControlSocket)
	if c.Branding.Logo != "" {
		c.Branding.Logo = resolvePath(base, c.Branding.Logo)
	}
	for i := range c.Apps {
		if c.Apps[i].Cwd != "" {
			c.Apps[i].Cwd = resolvePath(base, c.Apps[i].Cwd)
		}
	}
}

// resolvePath expands a leading ~/ and anchors relative paths at base.
func resolvePath(base, p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, rest)
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

func defaultStateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "waker")
	}
	return "~/.local/state/waker"
}

func defaultControlSocket(stateDir string) string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "waker", "control.sock")
	}
	return filepath.Join(stateDir, "control.sock")
}
