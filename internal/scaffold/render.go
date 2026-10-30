package scaffold

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// DocsURL is where the header comment sends the owner for every other key.
const DocsURL = "https://github.com/fum4/waker#every-key-with-its-default"

// line is one `key: value  # comment` row of a block.
type line struct{ key, value, comment string }

// Render writes the answers as waker.yaml in the house style: a header,
// then one block per mapping whose values (and trailing comments) align.
func (a Answers) Render() []byte {
	var b strings.Builder
	b.WriteString("# waker.yaml, written by `waker init`.\n")
	b.WriteString("# Validate with `waker check`; every other key, with its default:\n")
	b.WriteString("# " + DocsURL + "\n\n")
	b.WriteString("apps:\n")
	writeBlock(&b, "  - ", "    ", []line{
		{"name", scalar(a.Name), ""},
		{"listen", scalar(a.Listen), listenComment(a.Listen)},
		{"upstream", scalar(fmt.Sprintf("http://127.0.0.1:%d", a.UpstreamPort)), "where the app itself serves"},
		{"start", flowSeq(a.Start), ""},
		{"idle", scalar(a.Idle), "stop after this long without a request"},
	})
	b.WriteString("    health:\n")
	writeBlock(&b, "      ", "      ", []line{
		{"path", scalar(a.HealthPath), "up = a GET below 400"},
	})
	return []byte(b.String())
}

// writeBlock pads keys to the block's longest, and comments to the longest
// commented `key: value` (a long uncommented start can't push them out).
func writeBlock(b *strings.Builder, first, rest string, lines []line) {
	keyW, rowW := 0, 0
	for _, l := range lines {
		keyW = max(keyW, len(l.key)+1)
	}
	rows := make([]string, len(lines))
	for i, l := range lines {
		rows[i] = fmt.Sprintf("%-*s %s", keyW, l.key+":", l.value)
		if l.comment != "" {
			rowW = max(rowW, len(rows[i]))
		}
	}
	for i, l := range lines {
		indent := rest
		if i == 0 {
			indent = first
		}
		row := rows[i]
		if l.comment != "" {
			row = fmt.Sprintf("%-*s  # %s", rowW, row, l.comment)
		}
		b.WriteString(indent + row + "\n")
	}
}

// plain is a string YAML reads back as itself without quotes: no
// indicator first (% @ are), no trailing colon, nothing a flow sequence
// or a comment would split on.
var plain = regexp.MustCompile(`^[A-Za-z0-9_./+=~-]([A-Za-z0-9_./:@%+=~-]*[A-Za-z0-9_./@%+=~-])?$`)

// yaml11Words are read as booleans or null by YAML 1.1 tools; quoting them
// keeps the file meaning one thing to every reader.
var yaml11Words = map[string]bool{
	"y": true, "n": true, "yes": true, "no": true, "on": true, "off": true,
	"true": true, "false": true, "null": true, "~": true,
}

// scalar renders s plain when that reads back as the same string, else
// double-quoted (JSON escapes are valid YAML).
func scalar(s string) string {
	_, numErr := strconv.ParseFloat(s, 64)
	if plain.MatchString(s) && !yaml11Words[strings.ToLower(s)] && numErr != nil && (s[0] != '-' || isFlag(s)) {
		return s
	}
	q, _ := json.Marshal(s)
	return string(q)
}

// isFlag lets --bind and -m stay plain: a dash then a letter (or a second
// dash and more) is a plain scalar; a lone "-" is a sequence entry.
func isFlag(s string) bool {
	c := strings.TrimPrefix(s[1:], "-")
	return c != "" && (c[0] >= 'a' && c[0] <= 'z' || c[0] >= 'A' && c[0] <= 'Z')
}

func flowSeq(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = scalar(a)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// Loopback reports whether a listen address only serves this machine.
func Loopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}

// listenComment says who can reach the address, and how to change that.
func listenComment(listen string) string {
	if Loopback(listen) {
		return "this machine only: point your proxy at it"
	}
	return "open to other machines; 127.0.0.1 behind a proxy"
}
