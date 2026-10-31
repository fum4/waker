package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/prompt"
	"github.com/fum4/waker/internal/scaffold"
)

// stdin and interactive are the prompt's input and the TTY test; tests
// swap both.
var (
	stdin       io.Reader = os.Stdin
	interactive           = func() bool { return prompt.IsTerminal(os.Stdin) && prompt.IsTerminal(os.Stdout) }
)

// initFlags are the answers as flags, shared by init and setup.
type initFlags struct {
	config, name, listen, start, health, idle string
	upstreamPort                              int
	yes, force                                bool
	// service is the service: keys as flags, written into the new file.
	service *serviceFlags
}

func addInitFlags(fs *flag.FlagSet) *initFlags {
	f := &initFlags{service: addServiceFlags(fs)}
	fs.StringVar(&f.config, "config", "", "file to write (default $WAKER_CONFIG, else ./waker.yaml)")
	fs.StringVar(&f.name, "name", "", "app name (default: the config directory's name)")
	fs.StringVar(&f.listen, "listen", scaffold.DefaultListen, "waker's host:port: 0.0.0.0 for other machines, 127.0.0.1 behind a proxy")
	fs.IntVar(&f.upstreamPort, "upstream-port", scaffold.DefaultUpstreamPort, "the app's own port, on 127.0.0.1")
	fs.StringVar(&f.start, "start", "", "the command that starts the app, e.g. 'npm run start' (required with --yes)")
	fs.StringVar(&f.health, "health", scaffold.DefaultHealthPath, "health check path")
	fs.StringVar(&f.idle, "idle", scaffold.DefaultIdle, "stop after this long without a request")
	fs.BoolVar(&f.yes, "yes", false, "don't prompt: take the flags and defaults (for agents and CI)")
	fs.BoolVar(&f.force, "force", false, "overwrite an existing config")
	return f
}

func runInit(args []string, stdout, stderr io.Writer) error {
	fs := newBareFlagSet("init", stderr)
	f := addInitFlags(fs)
	if err := parseNoArgs(fs, args); err != nil {
		return err
	}
	if *f.service.noSave {
		return fmt.Errorf("%w: --no-save: init only writes the file, so its flags are what it saves", errUsage)
	}
	p, err := f.prompter(stdout)
	if err != nil {
		return err
	}
	path, err := writeConfig(f, p, stdout, stderr)
	if err != nil {
		return err
	}
	if p == nil {
		fmt.Fprintf(stdout, "next: waker service install --config %s\n", path)
		return nil
	}
	return offerInstall(p, path, nil, stdout, stderr)
}

func runSetup(args []string, stdout, stderr io.Writer) error {
	fs := newBareFlagSet("setup", stderr)
	f := addInitFlags(fs)
	if err := parseNoArgs(fs, args); err != nil {
		return err
	}
	p, err := f.prompter(stdout)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(config.ResolvePath(f.config))
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !f.force {
		if err := report(p, "using", path, stdout, stderr); err != nil {
			return err
		}
	} else if path, err = writeConfig(f, p, stdout, stderr); err != nil {
		return err
	}
	// install gets the flags too: it saves them to an existing config
	// (writeConfig already did to a new one) and honours --no-save.
	install := f.service.args()
	if p == nil {
		return runServiceInstall(append([]string{"--config", path}, install...), stdout, stderr)
	}
	return offerInstall(p, path, install, stdout, stderr)
}

// prompter is nil with --yes; an error without --yes and no terminal,
// since there is nobody to answer.
func (f *initFlags) prompter(stdout io.Writer) (*prompt.Prompter, error) {
	if f.yes {
		return nil, nil
	}
	if !interactive() {
		return nil, fmt.Errorf("%w: not a terminal, so nothing to prompt: pass --yes and the answers as flags (--start is required)", errUsage)
	}
	p := prompt.New(stdin, stdout, prompt.ColorWanted())
	p.AlignLabels(initLabels...)
	return p, nil
}

// writeConfig collects the answers (prompting when p is set), writes the
// file and runs waker check on it. It returns the file's absolute path.
func writeConfig(f *initFlags, p *prompt.Prompter, stdout, stderr io.Writer) (string, error) {
	// The service flags are checked before any question, like install's.
	svc, err := f.service.apply(config.Service{})
	if err != nil {
		return "", fmt.Errorf("%w: %v", errUsage, err)
	}
	path, err := filepath.Abs(config.ResolvePath(f.config))
	if err != nil {
		return "", err
	}
	var a scaffold.Answers
	if p != nil {
		p.Title("waker init", "one app behind waker · Ctrl-D cancels")
		if f.config == "" {
			if path, err = askPath(p, path); err != nil {
				return "", err
			}
		}
		if err := refuseOverwrite(path, f.force); err != nil {
			return "", err
		}
		if a, err = ask(p, f, path); err != nil {
			return "", err
		}
	} else {
		if err := refuseOverwrite(path, f.force); err != nil {
			return "", err
		}
		if a, err = f.answers(path); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	a.ServicePath = config.DefaultServicePath(svc.System)
	data := a.Render()
	if !*f.service.noSave {
		if data, err = config.EditService(path, data, f.service.givenKeys()); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, report(p, "wrote", path, stdout, stderr)
}

func refuseOverwrite(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite it", path)
	}
	return nil
}

// answers builds the answers from flags and defaults (--yes).
func (f *initFlags) answers(path string) (scaffold.Answers, error) {
	start, err := scaffold.SplitArgs(f.start)
	if err != nil {
		return scaffold.Answers{}, fmt.Errorf("%w: --start: %v", errUsage, err)
	}
	a := scaffold.Answers{
		Name: f.name, Listen: f.listen, UpstreamPort: f.upstreamPort,
		Start: start, HealthPath: f.health, Idle: f.idle,
	}
	if a.Name == "" {
		a.Name = scaffold.DefaultName(filepath.Dir(path))
	}
	if err := a.Validate(); err != nil {
		return a, fmt.Errorf("%w: %v", errUsage, strings.ReplaceAll(err.Error(), "\n", "; "))
	}
	return a, nil
}

func offerInstall(p *prompt.Prompter, path string, install []string, stdout, stderr io.Writer) error {
	fmt.Fprintln(stdout)
	yes, err := p.Confirm("Install as a service now?", true)
	if err != nil || !yes {
		if err == nil {
			p.Note("later: waker service install --config " + displayPath(path))
		}
		return err
	}
	return runServiceInstall(append([]string{"--config", path}, install...), stdout, stderr)
}

func parseNoArgs(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	return nil
}

// aborted maps the prompt's Ctrl-D to a quiet exit 130 (like Ctrl-C).
func aborted(err error) error {
	if errors.Is(err, prompt.ErrAborted) {
		return exitCode(130)
	}
	return err
}
