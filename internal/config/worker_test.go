package config

import (
	"strings"
	"testing"
)

func TestWorkerConfiguration(t *testing.T) {
	tests := []struct {
		name, defaults, fields, problem string
		worker                          bool
	}{
		{name: "command worker", fields: "mode: on, health: {command: [true]}", worker: true},
		{name: "missing mode", fields: "health: {command: [true]}", problem: "workers require explicit mode: on"},
		{name: "declared off", fields: "mode: off, health: {command: [true]}", problem: "workers require explicit mode: on"},
		{name: "on demand", fields: "mode: on-demand, health: {command: [true]}", problem: "workers require explicit mode: on"},
		{name: "missing probe", fields: "mode: on", problem: "health.command: workers require"},
		{name: "HTTP probe", fields: "mode: on, health: {path: /ready}", problem: "workers have no HTTP upstream"},
		{name: "both probes", fields: "mode: on, health: {path: /ready, command: [true]}", problem: "workers have no HTTP upstream"},
		{name: "empty command", fields: "mode: on, health: {command: ['']}", problem: "first argv element must be a command"},
		{name: "listen alone", fields: "listen: '127.0.0.1:1'", problem: "set listen and upstream together"},
		{name: "upstream alone", fields: "upstream: 'http://127.0.0.1:2'", problem: "set listen and upstream together"},
		{name: "HTTP unchanged", fields: "listen: '127.0.0.1:1', upstream: 'http://127.0.0.1:2'"},
		{name: "override inherited HTTP probe", defaults: "defaults: {health: {path: /ready}}\n", fields: "mode: on, health: {command: [true]}", worker: true},
		{name: "inherited on", defaults: "defaults: {mode: on, health: {command: [true]}}\n", fields: "env: {}", worker: true},
		{name: "inherited HTTP probe rejected", defaults: "defaults: {health: {path: /ready}}\n", fields: "mode: on", problem: "defaults.health.path: workers have no HTTP upstream"},
		{name: "presentation unused", fields: "mode: on, health: {command: [true]}, pages: {offline: /missing-worker-page.html}, branding: {logo: /missing-worker-logo.svg}", worker: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := Parse([]byte(test.defaults + "apps: [{name: jobs, start: [jobs], " + test.fields + "}]"))
			if err != nil {
				t.Fatal(err)
			}
			err = cfg.Validate()
			if test.problem != "" {
				if err == nil || !strings.Contains(err.Error(), test.problem) {
					t.Fatalf("validation = %v, want %q", err, test.problem)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ac := cfg.Apps[0]
			if ac.Worker() != test.worker {
				t.Fatalf("worker = %t", ac.Worker())
			}
			if test.worker && (ac.Health.Path != "" || ac.HeartbeatEnabled()) {
				t.Fatalf("worker inherited HTTP behavior: %+v", ac)
			}
			if !test.worker && (ac.Health.Path != DefaultHealthPath || ac.Mode != DefaultMode || !ac.HeartbeatEnabled()) {
				t.Fatalf("HTTP defaults changed: %+v", ac)
			}
		})
	}
}

func TestWorkerMultipleWithoutListeners(t *testing.T) {
	cfg, err := Parse([]byte("defaults: {mode: on, health: {command: [true]}}\napps: [{name: mail, start: [mail]}, {name: jobs, start: [jobs]}]"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	resolved, err := cfg.EffectiveYAML()
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := Parse(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if err := roundTrip.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerMissingHealthHasNoHTTPDefault(t *testing.T) {
	cfg, err := Parse([]byte("apps: [{name: jobs, start: [jobs], mode: on}]"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Apps[0].Health.Path != "" {
		t.Fatal("worker received a default HTTP health path")
	}
}

func TestWorkerDoesNotIntroduceAKindKey(t *testing.T) {
	_, err := Parse([]byte("apps: [{name: jobs, start: [jobs], mode: on, health: {command: [true]}, kind: worker}]"))
	if err == nil || !strings.Contains(err.Error(), "field kind not found") {
		t.Fatalf("kind was not rejected: %v", err)
	}
}
