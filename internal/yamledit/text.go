package yamledit

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// offset is the byte offset of a node's 1-based line and column (the
// parser counts columns in characters).
func offset(src []byte, line, col int) int {
	at := 0
	for l := 1; l < line; l++ {
		i := bytes.IndexByte(src[at:], '\n')
		if i < 0 {
			return len(src)
		}
		at += i + 1
	}
	for c := 1; c < col && at < len(src) && src[at] != '\n'; c++ {
		_, n := utf8.DecodeRune(src[at:])
		at += n
	}
	return at
}

// scalarEnd is where the one-line scalar starting at `at` ends: after its
// closing quote, or (plain) before a comment, the line's end, or in flow
// context a `,` / `}` / `]`.
func scalarEnd(src []byte, at int, flow bool) (int, error) {
	if at >= len(src) {
		return at, nil
	}
	switch src[at] {
	case '"':
		for i := at + 1; i < len(src) && src[i] != '\n'; i++ {
			switch src[i] {
			case '\\':
				i++
			case '"':
				return i + 1, nil
			}
		}
		return 0, errors.New("a quoted value spans lines; change it by hand")
	case '\'':
		for i := at + 1; i < len(src) && src[i] != '\n'; i++ {
			if src[i] == '\'' {
				if i+1 < len(src) && src[i+1] == '\'' {
					i++
					continue
				}
				return i + 1, nil
			}
		}
		return 0, errors.New("a quoted value spans lines; change it by hand")
	}
	end := at
	for i := at; i < len(src) && src[i] != '\n'; i++ {
		c := src[i]
		if c == '#' && i > at && (src[i-1] == ' ' || src[i-1] == '\t') {
			break
		}
		if flow && (c == ',' || c == '}' || c == ']') {
			break
		}
		if c != ' ' && c != '\t' && c != '\r' {
			end = i + 1
		}
	}
	return end, nil
}

// insertFlow adds entry to the flow mapping sec, before its closing brace.
func insertFlow(src []byte, sec *yaml.Node, entry string) ([]byte, error) {
	open := offset(src, sec.Line, sec.Column)
	if open >= len(src) || src[open] != '{' {
		return nil, errors.New("can't find the mapping's opening brace")
	}
	depth, last := 0, open
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '"', '\'':
			end, err := scalarEnd(src, i, true)
			if err != nil {
				return nil, err
			}
			i, last = end-1, end-1
		case '#':
			if src[i-1] == ' ' || src[i-1] == '\t' || src[i-1] == '\n' {
				for i < len(src) && src[i] != '\n' {
					i++
				}
			}
		case '{', '[':
			depth++
			last = i
		case '}', ']':
			depth--
			if depth == 0 {
				if len(sec.Content) == 0 {
					return replace(src, i, i, entry), nil
				}
				return replace(src, last+1, last+1, ", "+entry), nil
			}
			last = i
		default:
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				last = i
			}
		}
	}
	return nil, errors.New("can't find the mapping's closing brace")
}

// insertAfterLine inserts text (whole lines) after the 1-based line.
func insertAfterLine(src []byte, line int, text string) []byte {
	at := offset(src, line+1, 1)
	if at == len(src) && (len(src) == 0 || src[len(src)-1] != '\n') {
		text = "\n" + text
	}
	return replace(src, at, at, text)
}

func replace(src []byte, from, to int, text string) []byte {
	out := make([]byte, 0, len(src)+len(text))
	out = append(out, src[:from]...)
	out = append(out, text...)
	return append(out, src[to:]...)
}

func spaces(n int) string { return strings.Repeat(" ", max(n, 0)) }

// blockScalar is value as a scalar in block context: plain when YAML
// reads it back as the same string, else double-quoted.
func blockScalar(value any) string {
	if b, ok := value.(bool); ok {
		return strconv.FormatBool(b)
	}
	s := value.(string)
	out, err := yaml.Marshal(s)
	if text := strings.TrimSuffix(string(out), "\n"); err == nil && !strings.ContainsAny(text, "\n|>") {
		return text
	}
	return quoted(s)
}

// flowScalar is value inside a flow mapping, where `,`, `:` and brackets
// end a plain scalar: anything but a simple word is quoted.
func flowScalar(value any) string {
	text := blockScalar(value)
	if strings.ContainsAny(text, ",:[]{}#") {
		return quoted(value.(string))
	}
	return text
}

// quoted is a JSON string, which is a valid YAML double-quoted scalar.
func quoted(s string) string {
	b, _ := json.Marshal(s) // a string always marshals
	return string(b)
}
