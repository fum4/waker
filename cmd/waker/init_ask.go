package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fum4/waker/internal/prompt"
	"github.com/fum4/waker/internal/scaffold"
)

// askPath asks where to write the config, def offered; ~/ expands.
func askPath(p *prompt.Prompter, def string) (string, error) {
	ans, err := p.Ask(prompt.Question{Label: "Config file", Default: tildify(def)})
	if err != nil {
		return "", err
	}
	if rest, ok := strings.CutPrefix(ans, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			ans = filepath.Join(home, rest)
		}
	}
	return filepath.Abs(ans)
}

// ask collects the answers, the flags' values offered as defaults.
func ask(p *prompt.Prompter, f *initFlags, path string) (scaffold.Answers, error) {
	var a scaffold.Answers
	name := f.name
	if name == "" {
		name = scaffold.DefaultName(filepath.Dir(path))
	}
	var err error
	if a.Name, err = p.Ask(prompt.Question{Label: "App name", Default: name, Validate: scaffold.ValidateName}); err != nil {
		return a, err
	}
	if a.Port, err = askPort(p, "Public port", "waker answers here", f.port, 0); err != nil {
		return a, err
	}
	if a.UpstreamPort, err = askPort(p, "App port", "where the app itself listens", f.upstreamPort, a.Port); err != nil {
		return a, err
	}
	start, err := p.Ask(prompt.Question{
		Label: "Start command", Hint: "e.g. npm run start", Default: f.start,
		Validate: func(s string) error { _, err := scaffold.SplitArgs(s); return err },
	})
	if err != nil {
		return a, err
	}
	a.Start, _ = scaffold.SplitArgs(start)
	if a.HealthPath, err = p.Ask(prompt.Question{Label: "Health path", Hint: "up = a GET below 400", Default: f.health, Validate: scaffold.ValidateHealthPath}); err != nil {
		return a, err
	}
	a.Idle, err = p.Ask(prompt.Question{Label: "Sleep after", Hint: "idle time before stopping it", Default: f.idle, Validate: scaffold.ValidateIdle})
	return a, err
}

// askPort asks for a port other than taken (0: none taken).
func askPort(p *prompt.Prompter, label, hint string, def, taken int) (int, error) {
	if def == taken {
		def = 0
	}
	d := ""
	if def > 0 {
		d = strconv.Itoa(def)
	}
	ans, err := p.Ask(prompt.Question{Label: label, Hint: hint, Default: d, Validate: func(s string) error {
		n, err := scaffold.ParsePort(s)
		if err == nil && n == taken {
			return errTaken
		}
		return err
	}})
	if err != nil {
		return 0, err
	}
	n, _ := scaffold.ParsePort(ans)
	return n, nil
}

var errTaken = portTakenError{}

type portTakenError struct{}

func (portTakenError) Error() string {
	return "waker already answers on that port; the app needs its own"
}

// tildify shows a path under the home directory as ~/….
func tildify(p string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
			return "~/" + rest
		}
	}
	return p
}
