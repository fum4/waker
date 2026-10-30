package config

import (
	"os"
	"strings"
	"testing"
)

// TestMain pins the host to Linux, so every test reads the same defaults
// whichever OS runs it; onHost swaps it per test.
func TestMain(m *testing.M) {
	host = PlatformFor("linux")
	os.Exit(m.Run())
}

func onHost(t *testing.T, goos string) {
	t.Helper()
	prev := host
	host = PlatformFor(goos)
	t.Cleanup(func() { host = prev })
}

func TestPlatformDefaults(t *testing.T) {
	tests := []struct {
		goos   string
		avail  string
		memory MemorySource
	}{
		{"linux", AvailableMeminfo, MemoryProcTree},
		{"darwin", AvailableVMStat, MemoryNone},
		{"freebsd", AvailableMeminfo, MemoryProcTree}, // anything else is treated as Linux
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			onHost(t, tt.goos)
			cfg := validConfig()
			if got := cfg.Resources.AvailableSource; got != tt.avail {
				t.Errorf("available_source = %q, want %q", got, tt.avail)
			}
			if got := cfg.Apps[0].MemorySource; got != tt.memory {
				t.Errorf("memory_source = %q, want %q", got, tt.memory)
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("defaults fail validation: %v", err)
			}
		})
	}
}

func TestDarwinRejectsLinuxSources(t *testing.T) {
	onHost(t, "darwin")
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"meminfo", func(c *Config) { c.Resources.AvailableSource = AvailableMeminfo },
			`resources.available_source: "meminfo" is not supported on darwin (supported: vm_stat)`},
		{"proctree", func(c *Config) { c.Apps[0].MemorySource = MemoryProcTree },
			`apps[0] (web).memory_source: "proctree" is not supported on darwin (supported: none)`},
		{"systemd", func(c *Config) { c.Apps[0].MemorySource, c.Apps[0].SystemdUnit = MemorySystemd, "web.service" },
			`apps[0] (web).memory_source: "systemd" is not supported on darwin (supported: none)`},
		{"unknown", func(c *Config) { c.Apps[0].MemorySource = "cgroup" },
			`.memory_source: "cgroup" must be proctree, systemd or none`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}
