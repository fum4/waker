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

// PeakSource names where an app's learned memory peak comes from.
type PeakSource string

const (
	// PeakProcTree samples the RSS of the process tree the waker started.
	PeakProcTree PeakSource = "proctree"
	// PeakSystemd reads MemoryPeak of a systemd unit.
	PeakSystemd PeakSource = "systemd"
	// PeakNone disables learning; only the declared memory counts.
	PeakNone PeakSource = "none"
)

// Config is a whole waker configuration.
type Config struct {
	// MaxHeadroom is the headroom a priority-1 app must leave free.
	MaxHeadroom Size `yaml:"max_headroom"`
	// MinHeadroom is the floor of the headroom formula (priority 10).
	MinHeadroom Size `yaml:"min_headroom"`
	// StateDir holds mode overrides and learned memory peaks.
	StateDir string `yaml:"state_dir"`
	// ControlSocket is the unix socket the CLI talks to.
	ControlSocket string   `yaml:"control_socket"`
	Branding      Branding `yaml:"branding"`
	Apps          []App    `yaml:"apps"`
}

// Branding customises the pages visitors see while an app is not up.
type Branding struct {
	// Logo is an SVG or PNG file served at /__waker/logo; empty = built-in mark.
	Logo string `yaml:"logo"`
	// Accent is a CSS hex colour (#rgb or #rrggbb) for the indicator.
	Accent string `yaml:"accent"`
	// Name replaces the app name under the indicator when set.
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
	Memory      Size              `yaml:"memory"`
	Priority    int               `yaml:"priority"`
	// Heartbeat is a pointer so an omitted key can default to true.
	Heartbeat   *bool    `yaml:"heartbeat"`
	BootTimeout Duration `yaml:"boot_timeout"`
	Peak        Peak     `yaml:"peak"`
}

// HeartbeatEnabled reports whether heartbeat injection is on for the app.
func (a App) HeartbeatEnabled() bool { return a.Heartbeat == nil || *a.Heartbeat }

// Health is how the waker decides an app is up: an HTTP GET of Path on the
// upstream answering below 400 (redirects count as up), or Command exiting 0.
type Health struct {
	Path     string   `yaml:"path"`
	Command  []string `yaml:"command"`
	Interval Duration `yaml:"interval"`
	Timeout  Duration `yaml:"timeout"`
}

// Peak configures learning an app's memory peak.
type Peak struct {
	Source PeakSource `yaml:"source"`
	// Unit is the systemd unit name, required when Source is systemd.
	Unit string `yaml:"unit"`
}

// Defaults applied by Load to keys the file leaves out.
const (
	DefaultMaxHeadroom    = 5120 * MiB
	DefaultMinHeadroom    = 512 * MiB
	DefaultMode           = ModeOnDemand
	DefaultIdle           = 30 * time.Minute
	DefaultPriority       = 5
	DefaultBootTimeout    = 600 * time.Second
	DefaultStopTimeout    = 10 * time.Second
	DefaultHealthPath     = "/"
	DefaultHealthInterval = time.Second
	// DefaultNeed is the memory need of an app with nothing declared or learned.
	DefaultNeed          = 1024 * MiB
	DefaultHealthTimeout = 2 * time.Second
	DefaultPeakSource    = PeakProcTree
	MinPriority          = 1
	MaxPriority          = 10
)
