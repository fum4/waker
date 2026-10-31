package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
)

// pendingSave is the waker.yaml a command will write once it has
// succeeded: its flags, saved to the service: block.
type pendingSave struct {
	path string
	data []byte
	keys []config.ServiceKey
}

// keyFor is the service: key and value a service flag sets.
func (f *serviceFlags) keyFor(name string) (string, any, bool) {
	switch name {
	case "system":
		return "system", *f.system, true
	case "no-start":
		return "start", !*f.noStart, true
	case "path":
		return "path", *f.path, true
	case "run-as":
		return "user", *f.runAs, true
	case "restart":
		return "restart", *f.restart, true
	}
	return "", nil, false
}

// givenKeys is every service flag on the command line, as its key.
func (f *serviceFlags) givenKeys() []config.ServiceKey {
	var keys []config.ServiceKey
	f.fs.Visit(func(fl *flag.Flag) {
		if k, v, ok := f.keyFor(fl.Name); ok {
			keys = append(keys, config.ServiceKey{Key: k, Value: v})
		}
	})
	return keys
}

// changedKeys is each given flag that changes the service, as its key: a
// flag that only repeats the file or a default (the service would be the
// same without it) isn't saved.
func (f *serviceFlags) changedKeys(file config.Service, cfgPath string, h service.Host, final service.Spec) []config.ServiceKey {
	var keys []config.ServiceKey
	for _, k := range f.givenKeys() {
		name := flagFor(k.Key)
		if without, err := f.applyExcept(file, name); err == nil {
			if sp, err := serviceSpec(without, cfgPath, h); err == nil && reflect.DeepEqual(sp, final) {
				continue
			}
		}
		keys = append(keys, k)
	}
	return keys
}

// flagFor is the flag that sets a service: key.
func flagFor(key string) string {
	switch key {
	case "start":
		return "no-start"
	case "user":
		return "run-as"
	}
	return key
}

// args is the given service flags (and --no-save) as command-line
// arguments, for setup to hand them to service install.
func (f *serviceFlags) args() []string {
	var args []string
	f.fs.Visit(func(fl *flag.Flag) {
		if _, _, ok := f.keyFor(fl.Name); ok || fl.Name == "no-save" {
			args = append(args, "--"+fl.Name+"="+fl.Value.String())
		}
	})
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
	return &pendingSave{path: path, data: data, keys: keys}, nil
}

// write saves the file and says what changed.
func (s *pendingSave) write(stdout io.Writer) error {
	if s == nil {
		return nil
	}
	if err := config.WriteFile(s.path, s.data); err != nil {
		return fmt.Errorf("save the flags to %s: %w", s.path, err)
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
