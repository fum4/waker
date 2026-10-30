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
// inside it (logo, pages, cwd, state_dir, control_socket) resolve against the
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

// file is the YAML as written: the config plus the defaults block, which
// Parse folds into each app.
type file struct {
	Config   `yaml:",inline"`
	Defaults App `yaml:"defaults"`
}

// Parse decodes YAML strictly (unknown keys are errors), merges the
// defaults block into every app (app > defaults > built-in) and applies the
// built-in defaults. It does not validate; Load does.
func Parse(data []byte) (*Config, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, errors.New("config is empty")
	}
	if doc := root.Content[0]; doc.Kind == yaml.MappingNode {
		if b := lookup(doc, "branding"); b != nil {
			return nil, fmt.Errorf("line %d: branding moved: put it under defaults (defaults.branding) or in an app", b.Line)
		}
	}
	// The strict pass catches unknown keys and bad values with their real
	// line numbers, defaults included. The merged tree holds only keys it
	// accepted, so its decode below need not be strict (yaml.Node offers no
	// strict decode).
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file{}); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config is empty")
		}
		return nil, err
	}
	got, err := applyDefaultsBlock(&root)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return nil, err
	}
	cfg.inherited = got
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	c.Resources.applyDefaults()
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
	a.Priority = DefaultPriority
	if a.PriorityRaw != nil {
		a.Priority = *a.PriorityRaw
	}
	if a.BootTimeout == 0 {
		a.BootTimeout = Duration(DefaultBootTimeout)
	}
	if a.Waiting == "" {
		a.Waiting = DefaultWaiting
	}
	if a.HoldTimeout == 0 {
		// Never past the boot itself: a boot_timeout under a minute caps it.
		a.HoldTimeout = Duration(min(DefaultHoldTimeout, a.BootTimeout.D()))
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
	if a.MemorySource == "" {
		a.MemorySource = DefaultMemorySource
	}
}

func (c *Config) resolvePaths(base string) {
	c.StateDir = resolvePath(base, c.StateDir)
	c.ControlSocket = resolvePath(base, c.ControlSocket)
	for i := range c.Apps {
		a := &c.Apps[i]
		if a.Cwd != "" {
			a.Cwd = resolvePath(base, a.Cwd)
		}
		if a.Branding.Logo != "" {
			a.Branding.Logo = resolvePath(base, a.Branding.Logo)
		}
		for k, f := range a.Pages.Files {
			a.Pages.Files[k] = resolvePath(base, f)
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
