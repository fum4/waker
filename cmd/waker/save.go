package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fum4/waker/internal/config"
)

// pendingSave is the waker.yaml a command writes once it has succeeded:
// its flags, saved to the service: block.
type pendingSave struct {
	path       string
	orig, data []byte
	keys       []config.ServiceKey
}

// givenKeys is every service flag on the command line, as its key.
func (f *serviceFlags) givenKeys() []config.ServiceKey {
	var keys []config.ServiceKey
	for _, r := range f.given() {
		keys = append(keys, config.ServiceKey{Key: r.key, Value: r.saved(f)})
	}
	return keys
}

// changedKeys is each given flag whose key final (the block with the
// flags applied) resolves differently from file, as its key. A flag that
// repeats the file or a fixed default isn't saved; one that pins what the
// environment would otherwise decide (--run-as under sudo) is.
func (f *serviceFlags) changedKeys(file, final config.Service) []config.ServiceKey {
	var keys []config.ServiceKey
	for _, r := range f.given() {
		if r.resolved(final) != r.resolved(file) {
			keys = append(keys, config.ServiceKey{Key: r.key, Value: r.saved(f)})
		}
	}
	return keys
}

// args is the given service flags (and --no-save) as command-line
// arguments, for setup to hand them to service install.
func (f *serviceFlags) args() []string {
	var args []string
	for _, r := range f.given() {
		args = append(args, "--"+r.flag+"="+f.fs.Lookup(r.flag).Value.String())
	}
	if *f.noSave {
		args = append(args, "--no-save")
	}
	return args
}

// prepareSave edits the config at path with keys, or is nil when there
// is nothing to save. It refuses a file that says `# managed: <tool>`.
func prepareSave(path string, keys []config.ServiceKey) (*pendingSave, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data, err := config.EditService(path, src, keys)
	if err != nil {
		return nil, err
	}
	return &pendingSave{path: path, orig: src, data: data, keys: keys}, nil
}

// write saves the file and says what changed. A file edited since
// prepareSave read it is left alone: the edit wins over the flags.
func (s *pendingSave) write(stdout io.Writer) error {
	if s == nil {
		return nil
	}
	now, err := os.ReadFile(s.path)
	if err == nil && !bytes.Equal(now, s.orig) {
		err = fmt.Errorf("it changed while waker installed")
	}
	if err == nil {
		err = config.WriteFile(s.path, s.data)
	}
	if err != nil {
		return fmt.Errorf("installed, but %s isn't saved to %s (%v); the file and the unit differ until you add it or install again", s.describe(), s.path, err)
	}
	fmt.Fprintf(stdout, "saved %s to %s (--no-save: this run only)\n", s.describe(), s.path)
	return nil
}

// describe is the keys as `service.restart: never, service.start: false`.
func (s *pendingSave) describe() string {
	parts := make([]string, len(s.keys))
	for i, k := range s.keys {
		parts[i] = fmt.Sprintf("service.%s: %v", k.Key, k.Value)
	}
	return strings.Join(parts, ", ")
}
