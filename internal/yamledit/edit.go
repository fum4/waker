// Package yamledit sets one key of a top-level mapping in a YAML document
// by editing only the text that holds it: every other byte (comments, key
// order, blank lines, alignment) stays as written.
package yamledit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"

	"go.yaml.in/yaml/v3"
)

// Set returns src with section.key set to value (a string or a bool). An
// existing value is replaced in place; a missing key is added as the
// section's first entry; a missing section is appended at the end. The
// result is re-parsed and checked: it must decode to src's data with only
// that one value changed.
func Set(src []byte, section, key string, value any) ([]byte, error) {
	switch value.(type) {
	case string, bool:
	default:
		return nil, fmt.Errorf("yamledit: unsupported value type %T", value)
	}
	root, err := rootMapping(src)
	if err != nil {
		return nil, err
	}
	out, err := splice(src, root, section, key, value)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", section, key, err)
	}
	if err := verify(src, out, section, key, value); err != nil {
		return nil, fmt.Errorf("%s.%s: %w", section, key, err)
	}
	return out, nil
}

// rootMapping is the document's top-level block mapping.
func rootMapping(src []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	var doc, next yaml.Node
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		return nil, errors.New("the file holds more than one YAML document")
	}
	if len(doc.Content) == 0 {
		return nil, errors.New("the file is empty")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode || root.Style&yaml.FlowStyle != 0 {
		return nil, errors.New("the file's top level is not a block mapping")
	}
	return root, nil
}

func splice(src []byte, root *yaml.Node, section, key string, value any) ([]byte, error) {
	secKey, sec := lookup(root, section)
	if sec == nil {
		return appendSection(src, section, key, value), nil
	}
	if sec.Kind == yaml.ScalarNode && sec.Tag == "!!null" {
		if sec.Value == "" { // `section:` with nothing after it
			return insertAfterLine(src, secKey.Line, "  "+key+": "+blockScalar(value)+"\n"), nil
		}
		at := offset(src, sec.Line, sec.Column) // `section: ~` / `null`
		end, err := scalarEnd(src, at, false)
		if err != nil {
			return nil, err
		}
		return replace(src, at, end, "{"+key+": "+flowScalar(value)+"}"), nil
	}
	if sec.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s is not a mapping", section)
	}
	flow := sec.Style&yaml.FlowStyle != 0
	if k, v := lookup(sec, key); v != nil {
		if v.Kind != yaml.ScalarNode || v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || v.Alias != nil || k.Line != v.Line {
			return nil, errors.New("only a one-line value can be edited; change it by hand")
		}
		at := offset(src, v.Line, v.Column)
		end, err := scalarEnd(src, at, flow)
		if err != nil {
			return nil, err
		}
		text := blockScalar(value)
		if flow {
			text = flowScalar(value)
		}
		return replace(src, at, end, text), nil
	}
	if flow {
		return insertFlow(src, sec, key+": "+flowScalar(value))
	}
	first := sec.Content[0]
	if first.Line == secKey.Line {
		return nil, fmt.Errorf("%s's first key shares its line; edit it by hand", section)
	}
	indent := spaces(first.Column - 1)
	return insertAfterLine(src, secKey.Line, indent+key+": "+blockScalar(value)+"\n"), nil
}

// lookup finds key in a mapping node: the key node and its value.
func lookup(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

func appendSection(src []byte, section, key string, value any) []byte {
	out := append([]byte{}, src...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	if len(out) > 1 && out[len(out)-2] != '\n' {
		out = append(out, '\n') // a blank line before the new section
	}
	return append(out, section+":\n  "+key+": "+blockScalar(value)+"\n"...)
}

// verify decodes before and after: after must be before with only
// section.key set to value.
func verify(before, after []byte, section, key string, value any) error {
	var want, got map[string]any
	if err := yaml.Unmarshal(before, &want); err != nil {
		return err
	}
	if err := yaml.Unmarshal(after, &got); err != nil {
		return fmt.Errorf("the edit would not parse (%v); change it by hand", err)
	}
	sec, _ := want[section].(map[string]any)
	if sec == nil {
		sec = map[string]any{}
	}
	sec[key] = value
	want[section] = sec
	if !reflect.DeepEqual(want, got) {
		return errors.New("the edit would change more than this key; change it by hand")
	}
	return nil
}
