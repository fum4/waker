// Package config loads, defaults and validates a waker configuration file.
package config

import "time"

// Mode is how the waker treats an app.
type Mode string

const (
	// ModeOn keeps the app running: it is booted at start and never idle-stopped.
	ModeOn Mode = "on"
	// ModeOff keeps the app stopped: visitors get the offline page.
	ModeOff Mode = "off"
	// ModeOnDemand boots the app on the first request and stops it after idle.
	ModeOnDemand Mode = "on-demand"
)

// Valid reports whether m is one of the known modes.
func (m Mode) Valid() bool {
	return m == ModeOn || m == ModeOff || m == ModeOnDemand
}

// MemorySource names where an app's learned memory peak comes from.
type MemorySource string

const (
	// MemoryProcTree samples the RSS of the process tree the waker started.
	MemoryProcTree MemorySource = "proctree"
	// MemorySystemd reads MemoryPeak of a systemd unit.
	MemorySystemd MemorySource = "systemd"
	// MemoryNone disables learning; only the declared memory counts.
	MemoryNone MemorySource = "none"
)

// Config is a whole waker configuration.
type Config struct {
	// StateDir holds mode overrides and learned memory peaks.
	StateDir string `yaml:"state_dir"`
	// ControlSocket is the unix socket the CLI talks to.
	ControlSocket string    `yaml:"control_socket"`
	Resources     Resources `yaml:"resources"`
	Branding      Branding  `yaml:"branding"`
	Apps          []App     `yaml:"apps"`
}

// Branding customises the pages visitors see while an app is not up.
type Branding struct {
	// Logo is an SVG or PNG file served at /__waker/logo; empty = built-in mark.
	Logo string `yaml:"logo"`
	// Accent is a CSS hex colour (#rgb or #rrggbb) for the indicator.
	Accent string `yaml:"accent"`
	// Name is the brand behind the logo (its alt text); the line under the
	// indicator is always the app name.
	Name string `yaml:"name"`
}

// App is one application the waker fronts.
type App struct {
	Name     string `yaml:"name"`
	Listen   string `yaml:"listen"`
	Upstream string `yaml:"upstream"`
	// Start and Stop are argv lists, run without a shell. An empty Stop means
	// SIGTERM to the started process group (SIGKILL after StopTimeout).
	Start       []string          `yaml:"start"`
	Stop        []string          `yaml:"stop"`
	StopTimeout Duration          `yaml:"stop_timeout"`
	Cwd         string            `yaml:"cwd"`
	Env         map[string]string `yaml:"env"`
	Health      Health            `yaml:"health"`
	Mode        Mode              `yaml:"mode"`
	Idle        Duration          `yaml:"idle"`
	IdleIgnore  []string          `yaml:"idle_ignore"`
	// Memory is the declared need; the learned peak raises it.
	Memory Size `yaml:"memory"`
	// MemorySource is where the learned peak comes from; SystemdUnit names
	// the unit when it is systemd.
	MemorySource MemorySource `yaml:"memory_source"`
	SystemdUnit  string       `yaml:"systemd_unit"`
	// Headroom, when set, replaces the curve's headroom for this app.
	Headroom Size `yaml:"headroom"`
	// PriorityRaw is the file's value (nil = omitted); read Priority.
	PriorityRaw *float64 `yaml:"priority"`
	Priority    float64  `yaml:"-"`
	// Evictable false protects the app from eviction (default true).
	Evictable *bool `yaml:"evictable"`
	// Heartbeat is a pointer so an omitted key can default to true.
	Heartbeat   *bool    `yaml:"heartbeat"`
	BootTimeout Duration `yaml:"boot_timeout"`
}

// HeartbeatEnabled reports whether heartbeat injection is on for the app.
func (a App) HeartbeatEnabled() bool { return a.Heartbeat == nil || *a.Heartbeat }

// IsEvictable reports whether the app's own config allows evicting it.
func (a App) IsEvictable() bool { return a.Evictable == nil || *a.Evictable }

// Health is how the waker decides an app is up: an HTTP GET of Path on the
// upstream answering below 400 (redirects count as up), or Command exiting 0.
type Health struct {
	Path     string   `yaml:"path"`
	Command  []string `yaml:"command"`
	Interval Duration `yaml:"interval"`
	Timeout  Duration `yaml:"timeout"`
}

// Defaults applied by Load to keys the file leaves out.
const (
	DefaultMode           = ModeOnDemand
	DefaultIdle           = 30 * time.Minute
	DefaultPriority       = 5.0
	DefaultBootTimeout    = 600 * time.Second
	DefaultStopTimeout    = 10 * time.Second
	DefaultHealthPath     = "/"
	DefaultHealthInterval = time.Second
	DefaultHealthTimeout  = 2 * time.Second
	DefaultMemorySource   = MemoryProcTree
	// DefaultNeed is the memory need of an app with nothing declared or learned.
	DefaultNeed = 1024 * MiB
	MinPriority = 1.0
	MaxPriority = 10.0
)
