package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() *Config {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		panic(err)
	}
	return cfg
}

func TestValidateAcceptsMinimal(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateNamesTheField(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"no apps", func(c *Config) { c.Apps = nil }, "apps: at least one"},
		{"bad name", func(c *Config) { c.Apps[0].Name = "Web App" }, `apps[0] (Web App).name`},
		{"bad listen", func(c *Config) { c.Apps[0].Listen = "8080" }, "apps[0] (web).listen"},
		{"bad upstream", func(c *Config) { c.Apps[0].Upstream = "127.0.0.1:3000" }, ".upstream"},
		{"no start", func(c *Config) { c.Apps[0].Start = nil }, ".start"},
		{"empty stop", func(c *Config) { c.Apps[0].Stop = []string{""} }, ".stop"},
		{"bad mode", func(c *Config) { c.Apps[0].Mode = "sometimes" }, ".mode"},
		{"priority low", func(c *Config) { c.Apps[0].Priority = -1 }, ".priority: -1 must be 1–10"},
		{"priority high", func(c *Config) { c.Apps[0].Priority = 11 }, ".priority: 11 must be 1–10"},
		{"negative memory", func(c *Config) { c.Apps[0].Memory = -1 }, ".memory"},
		{"ignore not a path", func(c *Config) { c.Apps[0].IdleIgnore = []string{"health"} }, ".idle_ignore[0]"},
		{"health both", func(c *Config) { c.Apps[0].Health.Command = []string{"true"} }, ".health: set path or command"},
		{"health path", func(c *Config) { c.Apps[0].Health.Path = "healthz" }, ".health.path"},
		{"systemd no unit", func(c *Config) { c.Apps[0].Peak.Source = PeakSystemd }, ".peak.unit: is required"},
		{"unit without systemd", func(c *Config) { c.Apps[0].Peak.Unit = "x.service" }, ".peak.unit: only applies"},
		{"bad peak source", func(c *Config) { c.Apps[0].Peak.Source = "cgroup" }, ".peak.source"},
		{"headroom order", func(c *Config) { c.MinHeadroom = 8 * GiB }, "min_headroom: 8GiB exceeds"},
		{"bad accent", func(c *Config) { c.Branding.Accent = "lime" }, "branding.accent"},
		{"logo ext", func(c *Config) { c.Branding.Logo = "/tmp/logo.gif" }, "branding.logo: /tmp/logo.gif must be"},
		{"logo missing", func(c *Config) { c.Branding.Logo = "/nonexistent/logo.svg" }, "branding.logo:"},
		{"dup name", func(c *Config) {
			b := c.Apps[0]
			b.Listen = "127.0.0.1:8081"
			c.Apps = append(c.Apps, b)
		}, "apps[1] (web).name: duplicates apps[0]"},
		{"dup listen", func(c *Config) {
			b := c.Apps[0]
			b.Name = "api"
			c.Apps = append(c.Apps, b)
		}, "apps[1] (api).listen: 127.0.0.1:8080 is already used by apps[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	cfg := validConfig()
	cfg.Apps[0].Priority = 42
	cfg.Apps[0].Mode = "maybe"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), ".priority") || !strings.Contains(err.Error(), ".mode") {
		t.Fatalf("want both problems, got %v", err)
	}
}

func TestValidateAcceptsLogoFile(t *testing.T) {
	logo := filepath.Join(t.TempDir(), "logo.svg")
	if err := os.WriteFile(logo, []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := validConfig()
	cfg.Branding = Branding{Logo: logo, Accent: "#c6f432", Name: "Acme"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
