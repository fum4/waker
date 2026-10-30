package config

import (
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

// Duration is a time.Duration written in config as "10s", "5m" or "1h30m".
type Duration time.Duration

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// String renders the duration the way time.Duration does.
func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalYAML decodes a Go duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: duration must be a string like 30s or 5m", node.Line)
	}
	v, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q (want e.g. 10s, 5m, 1h)", node.Line, node.Value)
	}
	*d = Duration(v)
	return nil
}
