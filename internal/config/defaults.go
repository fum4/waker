package config

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// DefaultsKey is the top-level block whose keys every app inherits.
const DefaultsKey = "defaults"

// PerAppOnly are the app keys a defaults block may not set: they identify
// one app, so sharing them is always a mistake.
var PerAppOnly = []string{"name", "listen", "upstream", "start", "stop"}

// mergedMaps are the keys whose maps merge per key (the app's keys win);
// every other key, lists included, is replaced whole by the app's.
var mergedMaps = map[string]bool{"health": true, "branding": true, "env": true, "pages": true}

// healthProbes are alternatives: an app that picks one inherits neither,
// or a default path plus an app command would be "both".
var healthProbes = []string{"path", "command"}

// inherited records, per app index, the key paths ("idle",
// "health.interval") an app took from defaults rather than setting itself.
type inherited []map[string]bool

// applyDefaultsBlock merges root's defaults mapping into every app mapping
// (app > defaults), removes the block, and returns what each app inherited.
// root is a document node already decoded strictly, so its keys are known.
func applyDefaultsBlock(root *yaml.Node) (inherited, error) {
	doc := resolve(root)
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = resolve(doc.Content[0])
	}
	if doc.Kind != yaml.MappingNode {
		return nil, nil
	}
	defs := lookup(doc, DefaultsKey)
	apps := lookup(doc, "apps")
	if defs == nil {
		return nil, nil
	}
	if defs.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: %s must be a mapping of app keys", defs.Line, DefaultsKey)
	}
	for i := 0; i < len(defs.Content); i += 2 {
		if k := defs.Content[i]; slices.Contains(PerAppOnly, k.Value) {
			return nil, fmt.Errorf("line %d: %s.%s is per-app only (it identifies one app)", k.Line, DefaultsKey, k.Value)
		}
	}
	remove(doc, DefaultsKey)
	if apps == nil || apps.Kind != yaml.SequenceNode {
		return nil, nil
	}
	got := make(inherited, len(apps.Content))
	for i, a := range apps.Content {
		got[i] = map[string]bool{}
		if a = resolve(a); a.Kind == yaml.MappingNode {
			mergeMapping(a, defs, "", got[i])
		}
	}
	return got, nil
}

// mergeMapping adds def's keys that dst lacks; at the top level (prefix ""),
// the mergedMaps keys present on both sides merge one level down instead.
func mergeMapping(dst, def *yaml.Node, prefix string, got map[string]bool) {
	for i := 0; i < len(def.Content); i += 2 {
		k, dv := def.Content[i], resolve(def.Content[i+1])
		av := lookup(dst, k.Value)
		switch {
		case av == nil:
			if prefix == "health." && slices.Contains(healthProbes, k.Value) && hasAny(dst, healthProbes) {
				continue
			}
			dst.Content = append(dst.Content, k, def.Content[i+1])
			got[prefix+k.Value] = true
		case prefix == "" && mergedMaps[k.Value] && av.Kind == yaml.MappingNode && dv.Kind == yaml.MappingNode:
			mergeMapping(av, dv, k.Value+".", got)
		}
	}
}

func hasAny(m *yaml.Node, keys []string) bool {
	for _, k := range keys {
		if lookup(m, k) != nil {
			return true
		}
	}
	return false
}

// lookup returns the (alias-resolved) value of key in mapping m, or nil.
func lookup(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return resolve(m.Content[i+1])
		}
	}
	return nil
}

func remove(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = slices.Delete(m.Content, i, i+2)
			return
		}
	}
}

func resolve(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// fromDefaults reports whether app i's field path (".health.interval",
// ".idle_ignore[2]") names a value inherited from defaults.
func (in inherited) fromDefaults(i int, path string) bool {
	if i >= len(in) {
		return false
	}
	key := ""
	for _, seg := range strings.Split(path, ".") {
		seg, _, _ = strings.Cut(seg, "[")
		if key += seg; in[i][key] {
			return true
		}
		key += "."
	}
	return false
}
