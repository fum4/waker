// Package scaffold writes a starter waker.yaml for one app: the answers
// `waker init` collects, their validation, and the file in the house
// style (a header comment, a fixed key order, aligned values).
package scaffold

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fum4/waker/internal/config"
)

// Answers are the questions `waker init` asks, one app's worth.
type Answers struct {
	Name         string
	Port         int // waker's public port, on 127.0.0.1
	UpstreamPort int // the app's own port, on 127.0.0.1
	Start        []string
	HealthPath   string
	Idle         string // a Go duration
}

// Defaults for every answer but Start, which has none. Idle is the
// built-in default, written out so the owner sees the knob.
const (
	DefaultPort         = 8080
	DefaultUpstreamPort = 3000
	DefaultHealthPath   = config.DefaultHealthPath
)

// DefaultIdle is written as "30m", not time.Duration's "30m0s".
var DefaultIdle = strings.TrimSuffix(config.Duration(config.DefaultIdle).String(), "0s")

// DefaultName derives an app name from a directory: lowercased, runs of
// anything else turned into dashes; "app" if nothing is left.
func DefaultName(dir string) string {
	n := strings.Trim(nonName.ReplaceAllString(strings.ToLower(filepath.Base(dir)), "-"), "-")
	if len(n) > 63 {
		n = strings.TrimRight(n[:63], "-")
	}
	if !config.ValidName(n) {
		return "app"
	}
	return n
}

var nonName = regexp.MustCompile(`[^a-z0-9]+`)

// ValidateName accepts an app name waker check accepts.
func ValidateName(s string) error {
	if !config.ValidName(s) {
		return errors.New("lowercase letters, digits and dashes, starting with a letter or digit")
	}
	return nil
}

// ParsePort reads a TCP port, 1–65535.
func ParsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q is not a port (1–65535)", s)
	}
	return n, nil
}

// ValidateHealthPath accepts an absolute URL path.
func ValidateHealthPath(s string) error {
	if !strings.HasPrefix(s, "/") || strings.ContainsAny(s, " \t") {
		return errors.New("an absolute path like / or /healthz")
	}
	return nil
}

// ValidateIdle accepts a positive Go duration.
func ValidateIdle(s string) error {
	if d, err := time.ParseDuration(s); err != nil || d <= 0 {
		return errors.New("a duration like 30s, 10m or 1h")
	}
	return nil
}

// Validate checks the answers as a whole.
func (a Answers) Validate() error {
	var errs []error
	if err := ValidateName(a.Name); err != nil {
		errs = append(errs, fmt.Errorf("name: %w", err))
	}
	for _, p := range []struct {
		key string
		n   int
	}{{"port", a.Port}, {"upstream port", a.UpstreamPort}} {
		if p.n < 1 || p.n > 65535 {
			errs = append(errs, fmt.Errorf("%s: %d is not a port (1–65535)", p.key, p.n))
		}
	}
	if a.Port == a.UpstreamPort {
		errs = append(errs, fmt.Errorf("port: waker and the app can't both listen on %d", a.Port))
	}
	if len(a.Start) == 0 {
		errs = append(errs, errors.New("start: the command that starts the app is required"))
	}
	if err := ValidateHealthPath(a.HealthPath); err != nil {
		errs = append(errs, fmt.Errorf("health path: %w", err))
	}
	if err := ValidateIdle(a.Idle); err != nil {
		errs = append(errs, fmt.Errorf("idle: %w", err))
	}
	return errors.Join(errs...)
}
