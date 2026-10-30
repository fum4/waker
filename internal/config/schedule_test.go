package config

import (
	"fmt"
	"strings"
	"testing"
)

// schedConfig builds apps web and db; extra holds each one's extra flow
// keys (", mode: off"), defs a defaults body.
func schedConfig(defs string, extra ...string) string {
	var b strings.Builder
	if defs != "" {
		b.WriteString("defaults:\n" + defs)
	}
	b.WriteString("apps:\n")
	for i, name := range []string{"web", "db"} {
		fmt.Fprintf(&b, "  - {name: %s, listen: '127.0.0.1:%d', upstream: 'http://127.0.0.1:%d', start: [x]", name, 8080+i, 3000+i)
		if i < len(extra) {
			b.WriteString(extra[i])
		}
		b.WriteString("}\n")
	}
	return b.String()
}

func TestScheduleValid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []int // schedule length per app
	}{
		{"own", schedConfig("", ", schedule: [{cron: '0 9 * * 1-5', for: 9h}]"), []int{1, 0}},
		{"inherited", schedConfig("  schedule: [{cron: '@daily', for: 1h}]\n"), []int{1, 1}},
		{"app's list replaces defaults", schedConfig("  schedule: [{cron: '@daily', for: 1h}]\n",
			", schedule: [{cron: '0 9 * * *', for: 1h}, {cron: '0 20 * * *', for: 1h}]", ", schedule: []"), []int{2, 0}},
		{"inherited is dropped in mode on", schedConfig("  schedule: [{cron: '@daily', for: 1h}]\n", "", ", mode: 'on'"), []int{1, 0}},
		{"CRON_TZ", schedConfig("", ", schedule: [{cron: 'CRON_TZ=Europe/Bucharest 0 9 * * *', for: 168h}]"), []int{1, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
			for i, n := range tt.want {
				if got := len(cfg.Apps[i].Schedule); got != n {
					t.Errorf("%s schedule = %v, want %d entries", cfg.Apps[i].Name, cfg.Apps[i].Schedule, n)
				}
			}
		})
	}
}

func TestScheduleInvalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"bad cron", schedConfig("", ", schedule: [{cron: '0 9 * *', for: 1h}]"),
			`apps[0] (web).schedule[0].cron: "0 9 * *": expected exactly 5 fields`},
		{"bad inherited cron is reported once, in defaults", schedConfig("  schedule: [{cron: '61 * * * *', for: 1h}]\n"),
			`defaults.schedule[0].cron: "61 * * * *":`},
		{"@every", schedConfig("", ", schedule: [{cron: '@every 1h', for: 1h}]"), "@every has no fixed fire times"},
		{"zero for", schedConfig("", ", schedule: [{cron: '@daily', for: 0s}]"),
			"apps[0] (web).schedule[0].for: 0s must be positive and at most 168h0m0s"},
		{"for over a week", schedConfig("", ", schedule: [{cron: '@daily', for: 169h}]"), "schedule[0].for: 169h0m0s must be positive"},
		{"own schedule in mode on", schedConfig("", ", mode: 'on', schedule: [{cron: '@daily', for: 1h}]"),
			"apps[0] (web).schedule: has no effect in mode on"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}
			err = cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate = %v; want %q", err, tt.want)
			}
			if n := strings.Count(err.Error(), "schedule"); n != 1 {
				t.Fatalf("%d schedule errors, want 1: %v", n, err)
			}
		})
	}
}

func TestScheduleUnknownKeyFails(t *testing.T) {
	_, err := Parse([]byte(schedConfig("", ", schedule: [{cron: '@daily', for: 1h, tz: UTC}]")))
	if err == nil || !strings.Contains(err.Error(), "field tz not found") {
		t.Fatalf("Parse = %v", err)
	}
}
