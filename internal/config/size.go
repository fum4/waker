package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Size is a byte count written in config as "512MiB", "2GiB" or "768M".
// Single-letter units are binary too: "1G" == "1GiB".
type Size int64

// Binary size units.
const (
	KiB Size = 1 << 10
	MiB Size = 1 << 20
	GiB Size = 1 << 30
	TiB Size = 1 << 40
)

var sizePattern = regexp.MustCompile(`^(\d+)\s*([KMGT](?:iB)?|B)$`)

var sizeUnits = map[string]Size{
	"B": 1, "K": KiB, "KiB": KiB, "M": MiB, "MiB": MiB,
	"G": GiB, "GiB": GiB, "T": TiB, "TiB": TiB,
}

// ParseSize parses a size string. A unit is required, except for "0".
func ParseSize(s string) (Size, error) {
	s = strings.TrimSpace(s)
	if s == "0" {
		return 0, nil
	}
	m := sizePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid size %q (want e.g. 512MiB, 2GiB)", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	unit := sizeUnits[m[2]]
	if n > int64(^uint64(0)>>1)/int64(unit) {
		return 0, fmt.Errorf("size %q overflows", s)
	}
	return Size(n) * unit, nil
}

// MiBs returns the size in whole MiB, rounded down.
func (s Size) MiBs() int64 { return int64(s / MiB) }

// String renders the size in the largest unit that divides it exactly.
func (s Size) String() string {
	for _, u := range []struct {
		name string
		size Size
	}{{"TiB", TiB}, {"GiB", GiB}, {"MiB", MiB}, {"KiB", KiB}} {
		if s != 0 && s%u.size == 0 {
			return fmt.Sprintf("%d%s", s/u.size, u.name)
		}
	}
	return fmt.Sprintf("%dB", int64(s))
}

// UnmarshalYAML decodes a size string.
func (s *Size) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: size must be a string like 512MiB", node.Line)
	}
	v, err := ParseSize(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", node.Line, err)
	}
	*s = v
	return nil
}
