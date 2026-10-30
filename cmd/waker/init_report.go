package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/prompt"
)

// report says which config init or setup is using (verb: "wrote" or
// "using") and checks it. Without a prompt it is `waker check`'s own
// output; on a terminal, the same verdict as ✔ lines in the prompt's style.
func report(p *prompt.Prompter, verb, path string, stdout, stderr io.Writer) error {
	if p == nil {
		fmt.Fprintf(stdout, "%s %s\n", verb, path)
		return runCheck([]string{"--config", path}, stdout, stderr)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout)
	p.Done(verb + " " + displayPath(path))
	p.Done("waker check: ok")
	for _, a := range cfg.Apps {
		p.Note(fmt.Sprintf("%s  %s → %s · %s · sleeps after %s", a.Name, a.Listen, a.Upstream, a.Mode, shortDuration(a.Idle)))
	}
	warnings, source, err := servicePATHWarnings(cfg, path)
	for _, w := range warnings {
		p.Warn(w)
	}
	if len(warnings) > 0 {
		p.Note("(checked against " + source + ")")
	}
	return err
}

// displayPath shows a path relative to the working directory when it is
// inside it, else under ~/ when it can.
func displayPath(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return "./" + rel
		}
	}
	return tildify(path)
}

// shortDuration drops the zero units Go prints ("30m0s" → "30m").
func shortDuration(d config.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
