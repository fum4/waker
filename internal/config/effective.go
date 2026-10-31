package config

import "go.yaml.in/yaml/v3"

// Effective returns the app as waker runs it: every inherited and built-in
// default filled in, the pointer-optional keys resolved.
func (a App) Effective() App {
	prio, evict, hb := a.Priority, a.IsEvictable(), a.HeartbeatEnabled()
	a.PriorityRaw, a.Evictable, a.Heartbeat = &prio, &evict, &hb
	return a
}

// Effective returns the service block as install uses it: start resolved.
func (s Service) Effective() Service {
	start := s.Starts()
	s.Start = &start
	return s
}

// EffectiveYAML renders the resolved service block and every app's
// resolved config, in config order.
func (c *Config) EffectiveYAML() ([]byte, error) {
	apps := make([]App, len(c.Apps))
	for i, a := range c.Apps {
		apps[i] = a.Effective()
	}
	return yaml.Marshal(struct {
		Service Service `yaml:"service"`
		Apps    []App   `yaml:"apps"`
	}{c.Service.Effective(), apps})
}

// MarshalYAML renders the duration the way it is written ("10s").
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

// MarshalYAML renders the size the way it is written ("512MiB").
func (s Size) MarshalYAML() (any, error) { return s.String(), nil }
