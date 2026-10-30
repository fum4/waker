package main

import (
	"fmt"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/service"
)

// pathWarnings names every start or stop command that isn't an executable
// on path, a relative name resolved against the app's cwd, else workDir
// (the service's working directory): under a service, a command that only
// the login shell's PATH finds fails at the first wake.
func pathWarnings(cfg *config.Config, path, workDir string) []string {
	var out []string
	for _, a := range cfg.Apps {
		dir := a.Cwd
		if dir == "" {
			dir = workDir
		}
		for _, c := range []struct {
			key  string
			argv []string
		}{{"start", a.Start}, {"stop", a.Stop}} {
			if len(c.argv) == 0 {
				continue
			}
			p, ok := service.LookPath(c.argv[0], path, dir)
			switch {
			case ok:
			case p != "": // a name with a slash: checked as a file, not on PATH
				out = append(out, fmt.Sprintf("%s.%s: %s is not an executable file", a.Name, c.key, p))
			default:
				out = append(out, fmt.Sprintf("%s.%s: %q is not an executable on the service's PATH", a.Name, c.key, c.argv[0]))
			}
		}
	}
	return out
}
