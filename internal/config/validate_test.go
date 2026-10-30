package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		{"priority low", func(c *Config) { c.Apps[0].Priority = -1 }, ".priority: -1 must be a number from 1 to 10"},
		{"priority high", func(c *Config) { c.Apps[0].Priority = 10.5 }, ".priority: 10.5 must be a number from 1 to 10"},
		{"negative memory", func(c *Config) { c.Apps[0].Memory = -1 }, ".memory"},
		{"ignore not a path", func(c *Config) { c.Apps[0].IdleIgnore = []string{"health"} }, ".idle_ignore[0]"},
		{"health both", func(c *Config) { c.Apps[0].Health.Command = []string{"true"} }, ".health: set path or command"},
		{"health path", func(c *Config) { c.Apps[0].Health.Path = "healthz" }, ".health.path"},
		{"systemd no unit", func(c *Config) { c.Apps[0].MemorySource = MemorySystemd }, ".systemd_unit: is required"},
		{"unit without systemd", func(c *Config) { c.Apps[0].SystemdUnit = "x.service" }, ".systemd_unit: only applies"},
		{"bad memory source", func(c *Config) { c.Apps[0].MemorySource = "cgroup" }, ".memory_source"},
		{"headroom order", func(c *Config) { c.Resources.Headroom.Min = 8 * GiB }, "resources.headroom.min: 8GiB exceeds"},
		{"nan priority", func(c *Config) { c.Apps[0].Priority = math.NaN() }, ".priority: NaN must be"},
		{"inf priority", func(c *Config) { c.Apps[0].Priority = math.Inf(1) }, ".priority: +Inf must be"},
		{"bad waiting", func(c *Config) { c.Apps[0].Waiting = "spin" }, `.waiting: "spin" must be page or hold`},
		{"hold past boot", func(c *Config) { c.Apps[0].HoldTimeout = Duration(time.Hour) }, ".hold_timeout: 1h0m0s exceeds boot_timeout 10m0s"},
		{"hold not positive", func(c *Config) { c.Apps[0].HoldTimeout = -1 }, ".hold_timeout: must be positive"},
		{"negative app headroom", func(c *Config) { c.Apps[0].Headroom = -1 }, ".headroom: must not be negative"},
		{"bad curve", func(c *Config) { c.Resources.Headroom.Curve = "cubic" }, "resources.headroom.curve"},
		{"table missing", func(c *Config) {
			c.Resources.Headroom = Headroom{Curve: CurveTable, Table: map[int]Size{1: MiB}}
		}, "resources.headroom.table: needs a value for every priority 1–10 (missing 2)"},
		{"table key out of range", func(c *Config) {
			c.Resources.Headroom = Headroom{Curve: CurveTable, Table: map[int]Size{}}
			for p := 0; p <= 10; p++ {
				c.Resources.Headroom.Table[p] = MiB
			}
		}, "priority 0 is outside 1–10"},
		{"fixed without value", func(c *Config) { c.Resources.Headroom.Curve = CurveFixed }, "resources.headroom.value: is required"},
		{"bad policy", func(c *Config) { c.Resources.Eviction.Policy = "random" }, "resources.eviction.policy"},
		{"bad order", func(c *Config) { c.Resources.Eviction.Order = "random" }, "resources.eviction.order"},
		{"protect unknown", func(c *Config) { c.Resources.Eviction.Protect = []string{"ghost"} }, `resources.eviction.protect: [0] "ghost" is not a configured app`},
		{"other platform's available source", func(c *Config) { c.Resources.AvailableSource = AvailableVMStat },
			`resources.available_source: "vm_stat" is not supported on linux (supported: meminfo)`},
		{"unknown available source", func(c *Config) { c.Resources.AvailableSource = "cgroup" },
			`resources.available_source: "cgroup" is not supported on linux`},
		{"bad accent", func(c *Config) { c.Apps[0].Branding.Accent = "lime" }, "apps[0] (web).branding.accent"},
		{"logo ext", func(c *Config) { c.Apps[0].Branding.Logo = "/tmp/logo.gif" }, "apps[0] (web).branding.logo: /tmp/logo.gif must be"},
		{"logo missing", func(c *Config) { c.Apps[0].Branding.Logo = "/nonexistent/logo.svg" }, "apps[0] (web).branding.logo:"},
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

func TestValidateContact(t *testing.T) {
	tests := []struct {
		contact string
		ok      bool
	}{
		{"hello@example.com", true},
		{"a.b+c_d%e-f@mail.example.co.uk", true},
		{"https://example.com", true},
		{"https://example.com/help?topic=apps&lang=en#top", true},
		{"http://example.com", false},
		{"HTTPS://example.com", false},
		{"https://", false},
		{"https:///path", false},
		{"https://user:pw@example.com", false},
		{"https://example.com/<script>", false},
		{`https://example.com/"x"`, false},
		{"https://example.com/a b", false},
		{"mailto:hello@example.com", false},
		{"Hello <hello@example.com>", false},
		{`"a"@example.com`, false},
		{"a@b@example.com", false},
		{"a&b@example.com", false},
		{"hello@example", false},
		{"hello @example.com", false},
		{"example.com", false},
		{"call us", false},
	}
	for _, tt := range tests {
		t.Run(tt.contact, func(t *testing.T) {
			cfg := validConfig()
			cfg.Apps[0].Branding.Contact = tt.contact
			err := cfg.Validate()
			if tt.ok != (err == nil) {
				t.Fatalf("Validate(%q) = %v", tt.contact, err)
			}
			want := fmt.Sprintf("apps[0] (web).branding.contact: %q is not an email address or an https:// URL", tt.contact)
			if !tt.ok && !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

func TestValidateAcceptsLogoFile(t *testing.T) {
	logo := filepath.Join(t.TempDir(), "logo.svg")
	if err := os.WriteFile(logo, []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := validConfig()
	cfg.Apps[0].Branding = Branding{Logo: logo, Accent: "#c6f432", Name: "Acme"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
