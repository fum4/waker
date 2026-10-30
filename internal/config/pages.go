package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// PagesBare is the pages value that strips every waker page and JSON answer
// down to its status code and headers.
const PagesBare = "bare"

// MaxPageBytes caps an owner page file: it is held in memory for the
// daemon's life.
const MaxPageBytes = 1 * MiB

// PageKeys are the pages an owner may replace, one per visitor state.
var PageKeys = []string{"starting", "offline", "busy", "failed", "down"}

// Pages replaces an app's visitor pages: Files maps a page key to the
// owner's own HTML file, or Bare drops every body. Zero = built-in pages.
type Pages struct {
	Bare  bool
	Files map[string]string
}

// IsZero lets omitempty drop an unset pages key from the effective YAML.
func (p Pages) IsZero() bool { return !p.Bare && len(p.Files) == 0 }

// UnmarshalYAML accepts exactly "bare" or a mapping of known page keys to
// file paths. The mapping is checked by hand: a custom unmarshaler escapes
// the strict decoder's unknown-key check.
func (p *Pages) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value != PagesBare {
			return fmt.Errorf("line %d: pages: %q must be %s or a mapping of page files", n.Line, n.Value, PagesBare)
		}
		*p = Pages{Bare: true}
		return nil
	case yaml.MappingNode:
	default:
		return fmt.Errorf("line %d: pages must be %s or a mapping of page files", n.Line, PagesBare)
	}
	files := map[string]string{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], resolve(n.Content[i+1])
		if !slices.Contains(PageKeys, k.Value) {
			return fmt.Errorf("line %d: pages.%s is not a page (want %s)", k.Line, k.Value, strings.Join(PageKeys, ", "))
		}
		if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" || v.Value == "" {
			return fmt.Errorf("line %d: pages.%s must be a file path", v.Line, k.Value)
		}
		files[k.Value] = v.Value
	}
	*p = Pages{Files: files}
	return nil
}

// MarshalYAML renders pages the way they are written.
func (p Pages) MarshalYAML() (any, error) {
	if p.Bare {
		return PagesBare, nil
	}
	return p.Files, nil
}

// validate opens every page file (missing, unreadable, a directory or too
// big fails here, at load, not on a visitor's request).
func (p Pages) validate(field string, add addFunc) {
	for _, k := range PageKeys {
		path, ok := p.Files[k]
		if !ok {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			add(field+"."+k, "%v", err)
			continue
		}
		st, err := f.Stat()
		f.Close()
		switch {
		case err != nil:
			add(field+"."+k, "%v", err)
		case st.IsDir():
			add(field+"."+k, "%s is a directory", path)
		case st.Size() > int64(MaxPageBytes):
			add(field+"."+k, "%s is %d bytes, over the %s limit", path, st.Size(), MaxPageBytes)
		}
	}
}
