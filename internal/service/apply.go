package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotOurs is returned when uninstall finds a file install didn't write.
var ErrNotOurs = errors.New("not written by waker service install")

// PlanInstall returns the plan that installs sp on h.
func PlanInstall(h Host, sp Spec) (Plan, error) {
	m, err := managerFor(h)
	if err != nil {
		return Plan{}, err
	}
	return m.install(sp)
}

// PlanUninstall returns the plan that removes the service install wrote.
func PlanUninstall(h Host, system bool) (Plan, error) {
	m, err := managerFor(h)
	if err != nil {
		return Plan{}, err
	}
	return m.uninstall(system), nil
}

// Print writes the plan as the files and commands it would apply.
func (p Plan) Print(w io.Writer) {
	for _, c := range p.Pre {
		fmt.Fprintf(w, "$ %s\n", c)
	}
	for _, d := range p.MkDirs {
		fmt.Fprintf(w, "$ mkdir -p %s\n", shellQuote(d))
	}
	for _, f := range p.Files {
		fmt.Fprintf(w, "--- %s\n%s---\n", f.Path, f.Content)
	}
	for _, r := range p.Removes {
		fmt.Fprintf(w, "$ rm -rf %s\n", shellQuote(r))
	}
	for _, c := range p.Post {
		fmt.Fprintf(w, "$ %s\n", c)
	}
}

// Apply makes the plan's changes, echoing each step to w. A file it
// overwrites or removes must carry Marker unless force (a hand-written unit
// is never clobbered); a failing command stops the plan unless it is
// marked Ignore.
func (p Plan) Apply(r Runner, w io.Writer, force bool) error {
	if !force {
		for _, f := range p.Files {
			if err := checkOurs(f.Path); err != nil {
				return err
			}
		}
		for _, path := range p.Removes {
			if err := checkOurs(path); err != nil {
				return err
			}
		}
	}
	if err := run(r, w, p.Pre); err != nil {
		return err
	}
	for _, d := range p.MkDirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	for _, f := range p.Files {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.Path, []byte(f.Content), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(w, "wrote %s\n", f.Path)
	}
	for _, path := range p.Removes {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		fmt.Fprintf(w, "removed %s\n", path)
	}
	return run(r, w, p.Post)
}

// checkOurs refuses a regular file without Marker; directories (the log
// directory) and missing paths pass.
func checkOurs(path string) error {
	st, err := os.Lstat(path)
	if err != nil || st.IsDir() {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), Marker) {
		return fmt.Errorf("%s: %w (remove it by hand, or pass --force)", path, ErrNotOurs)
	}
	return nil
}

func run(r Runner, w io.Writer, cmds []Command) error {
	for _, c := range cmds {
		fmt.Fprintf(w, "$ %s\n", c)
		out, err := r.Run(c.Argv...)
		if err != nil && !c.Ignore {
			return fmt.Errorf("%s: %w\n%s", c, err, strings.TrimSpace(out))
		}
	}
	return nil
}
