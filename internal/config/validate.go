package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	namePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	accentPattern = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	// emailPattern is deliberately narrower than RFC 5322: no quotes, spaces
	// or angle brackets, so a contact never needs more than plain escaping.
	emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+$`)
)

// ValidName reports whether s is a valid app name.
func ValidName(s string) bool { return namePattern.MatchString(s) }

// ContactURLPrefix marks a branding.contact that is a URL; any other valid
// contact is an email address.
const ContactURLPrefix = "https://"

// validContact reports whether c is a plain email address or an https URL
// with a host (no credentials, no characters that need quoting).
func validContact(c string) bool {
	if emailPattern.MatchString(c) {
		return true
	}
	if !strings.HasPrefix(c, ContactURLPrefix) || strings.ContainsAny(c, " \t\r\n\"'<>`\\") {
		return false
	}
	u, err := url.Parse(c)
	return err == nil && u.Host != "" && u.User == nil
}

// Validate checks the whole config and reports every problem at once, each
// prefixed with the offending field's path (e.g. "apps[1].priority").
func (c *Config) Validate() error {
	var errs []error
	seen := map[string]bool{}
	add := func(field, format string, args ...any) {
		// A bad inherited value fails in every app the same way: say it once.
		msg := fmt.Sprintf("%s: %s", field, fmt.Sprintf(format, args...))
		if !seen[msg] {
			seen[msg] = true
			errs = append(errs, errors.New(msg))
		}
	}
	if len(c.Apps) == 0 {
		add("apps", "at least one app is required")
	}
	names, listens := map[string]int{}, map[string]int{}
	for i, a := range c.Apps {
		field, appAdd := c.appField(i, add)
		a.validate(field, appAdd)
		if j, dup := names[a.Name]; dup && a.Name != "" {
			add(field+".name", "duplicates apps[%d]", j)
		}
		names[a.Name] = i
		if j, dup := listens[a.Listen]; dup && a.Listen != "" {
			add(field+".listen", "%s is already used by apps[%d]", a.Listen, j)
		}
		listens[a.Listen] = i
	}
	c.validateDependsOn(names, add)
	c.Resources.validate(names, add)
	c.Service.validate(add)
	return errors.Join(errs...)
}

type addFunc func(field, format string, args ...any)

// appField returns app i's field path ("apps[1] (api)") and an add that
// reports an inherited value where it was written: defaults.<key>.
func (c *Config) appField(i int, add addFunc) (string, addFunc) {
	field := fmt.Sprintf("apps[%d]", i)
	if a := c.Apps[i]; a.Name != "" {
		field = fmt.Sprintf("apps[%d] (%s)", i, a.Name)
	}
	return field, func(f, format string, args ...any) {
		if rest, ok := strings.CutPrefix(f, field+"."); ok && c.inherited.fromDefaults(i, rest) {
			f = DefaultsKey + "." + rest
		}
		add(f, format, args...)
	}
}

func (b Branding) validate(field string, add addFunc) {
	if b.Accent != "" && !accentPattern.MatchString(b.Accent) {
		add(field+".accent", "%q is not a #rgb or #rrggbb colour", b.Accent)
	}
	if b.Contact != "" && !validContact(b.Contact) {
		add(field+".contact", "%q is not an email address or an https:// URL", b.Contact)
	}
	if b.Logo == "" {
		return
	}
	switch strings.ToLower(filepath.Ext(b.Logo)) {
	case ".svg", ".png":
	default:
		add(field+".logo", "%s must be an .svg or .png file", b.Logo)
		return
	}
	if st, err := os.Stat(b.Logo); err != nil {
		add(field+".logo", "%v", err)
	} else if st.IsDir() {
		add(field+".logo", "%s is a directory", b.Logo)
	}
}

func (a App) validate(field string, add addFunc) {
	if !namePattern.MatchString(a.Name) {
		add(field+".name", "%q must be lowercase letters, digits and dashes", a.Name)
	}
	if _, port, err := net.SplitHostPort(a.Listen); err != nil || port == "" {
		add(field+".listen", "%q is not a host:port address", a.Listen)
	}
	if u, err := url.Parse(a.Upstream); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		add(field+".upstream", "%q is not an http(s)://host:port URL", a.Upstream)
	}
	if len(a.Start) == 0 || a.Start[0] == "" {
		add(field+".start", "an argv list is required, e.g. [\"./run\", \"--port\", \"3000\"]")
	}
	if len(a.Stop) > 0 && a.Stop[0] == "" {
		add(field+".stop", "the first argv element must be a command")
	}
	if !a.Mode.Valid() {
		add(field+".mode", "%q must be on, off or on-demand%s", a.Mode, boolModeHint(a.Mode))
	}
	if a.Idle <= 0 {
		add(field+".idle", "must be positive")
	}
	for j, p := range a.IdleIgnore {
		if !strings.HasPrefix(p, "/") {
			add(fmt.Sprintf("%s.idle_ignore[%d]", field, j), "%q must start with /", p)
		}
	}
	if a.Memory < 0 {
		add(field+".memory", "must not be negative")
	}
	// The negated range test also rejects NaN, which fails every comparison.
	if !(a.Priority >= MinPriority && a.Priority <= MaxPriority) {
		add(field+".priority", "%g must be a number from %g to %g", a.Priority, MinPriority, MaxPriority)
	}
	if a.Headroom < 0 {
		add(field+".headroom", "must not be negative")
	}
	if a.BootTimeout <= 0 {
		add(field+".boot_timeout", "must be positive")
	}
	if a.Waiting != WaitingPage && a.Waiting != WaitingHold {
		add(field+".waiting", "%q must be page or hold", a.Waiting)
	}
	if a.HoldTimeout <= 0 {
		add(field+".hold_timeout", "must be positive")
	} else if a.HoldTimeout > a.BootTimeout {
		add(field+".hold_timeout", "%s exceeds boot_timeout %s", a.HoldTimeout, a.BootTimeout)
	}
	if a.StopTimeout <= 0 {
		add(field+".stop_timeout", "must be positive")
	}
	a.Health.validate(field+".health", add)
	a.Branding.validate(field+".branding", add)
	a.Pages.validate(field+".pages", add)
	a.validateMemorySource(field, add)
	a.validateSchedule(field, add)
}

func (h Health) validate(field string, add addFunc) {
	switch {
	case h.Path != "" && len(h.Command) > 0:
		add(field, "set path or command, not both")
	case h.Path != "" && !strings.HasPrefix(h.Path, "/"):
		add(field+".path", "%q must start with /", h.Path)
	case len(h.Command) > 0 && h.Command[0] == "":
		add(field+".command", "the first argv element must be a command")
	}
	if h.Interval <= 0 {
		add(field+".interval", "must be positive")
	}
	if h.Timeout <= 0 {
		add(field+".timeout", "must be positive")
	}
}

func (a App) validateMemorySource(field string, add addFunc) {
	if !slices.Contains(host.MemorySources, a.MemorySource) {
		if slices.Contains(allMemorySources, a.MemorySource) {
			add(field+".memory_source", "%s", unsupported(a.MemorySource, host.MemorySources))
		} else {
			add(field+".memory_source", "%q must be proctree, systemd or none", a.MemorySource)
		}
		return
	}
	switch a.MemorySource {
	case MemoryProcTree, MemoryNone:
		if a.SystemdUnit != "" {
			add(field+".systemd_unit", "only applies to memory_source: systemd")
		}
	case MemorySystemd:
		if a.SystemdUnit == "" {
			add(field+".systemd_unit", "is required for memory_source: systemd")
		}
	}
}

// boolModeHint explains a boolean-looking mode: waker reads YAML 1.2,
// where bare on and off are strings, but a YAML 1.1 tool (PyYAML, yq v3)
// that rewrote the file turns them into true and false.
func boolModeHint(m Mode) string {
	switch strings.ToLower(string(m)) {
	case "true", "yes":
		return " (write on; a YAML 1.1 tool may have rewritten on as true)"
	case "false", "no":
		return " (write off; a YAML 1.1 tool may have rewritten off as false)"
	}
	return ""
}
