package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fum4/waker/internal/yamledit"
)

// managedPattern is a `# managed: <tool>` line in the file's header: a
// generator owns the file, so waker must not write to it.
var managedPattern = regexp.MustCompile(`^#\s*managed:\s*(\S+)`)

// ManagedBy is the tool a `# managed: <tool>` line in src's header
// comment (the comment lines before the first key; a `---` or `%YAML`
// line may come first) names, or "".
func ManagedBy(src []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(src))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "---" || strings.HasPrefix(line, "%") {
			continue
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			return ""
		}
		if m := managedPattern.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// ManagedError refuses to write a file another tool generates.
type ManagedError struct{ Path, Tool string }

func (e *ManagedError) Error() string {
	return fmt.Sprintf("%s is managed by %s (its header says `# managed: %s`), so waker won't save flags to it: set them in %s's source, or pass --no-save to use them for this run only",
		e.Path, e.Tool, e.Tool, e.Tool)
}

// ServiceKey is one service: key to save, with its YAML value (a string
// or a bool).
type ServiceKey struct {
	Key   string
	Value any
}

// EditService returns src (the file at path) with each key set in its
// service: block, and nothing else changed: comments, order and spacing
// stay as written.
func EditService(path string, src []byte, keys []ServiceKey) ([]byte, error) {
	if tool := ManagedBy(src); tool != "" {
		return nil, &ManagedError{Path: path, Tool: tool}
	}
	for _, k := range keys {
		var err error
		if src, err = yamledit.Set(src, "service", k.Key, k.Value); err != nil {
			return nil, fmt.Errorf("save to %s: %w", path, err)
		}
	}
	return src, nil
}

// WriteFile rewrites the existing file at path in place. In place, not a
// temp file renamed over it: the file keeps its inode, so its owner, mode,
// ACLs, hard links and a symlink to it all stay, and its directory needs
// no write permission. A config is a few KiB, written in one call.
func WriteFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
