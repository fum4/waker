// Package prompt asks questions on a terminal: a label, a dim default,
// inline validation, and a ✔ line once answered. It is line-based (no raw
// mode), so it needs no dependency and works over any reader and writer,
// which is how the tests drive it.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI styles; Prompter.style drops them when colour is off.
const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	cyan   = "\x1b[36m"
	accent = "\x1b[38;5;155m" // waker's lime (#c6ff4a), as near as 256 colours get
	// up moves the cursor to the line above and clears it.
	up = "\x1b[1A\x1b[2K"
)

// ErrAborted is returned when the input ends before an answer (Ctrl-D).
var ErrAborted = errors.New("aborted")

// Prompter asks questions on in/out.
type Prompter struct {
	in    *bufio.Reader
	out   io.Writer
	color bool
	// labelWidth pads answered labels so their values share a column.
	labelWidth int
}

// New returns a prompter; color adds ANSI styles and redraws answered
// lines in place.
func New(in io.Reader, out io.Writer, color bool) *Prompter {
	return &Prompter{in: bufio.NewReader(in), out: out, color: color}
}

// IsTerminal reports whether f is a character device (a TTY).
func IsTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// ColorWanted is the colour default for a terminal: on unless NO_COLOR
// (https://no-color.org) is set or TERM is dumb.
func ColorWanted() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

// Dim greys s out when colour is wanted (a line printed outside a prompt).
func Dim(s string) string {
	if !ColorWanted() || s == "" {
		return s
	}
	return dim + s + reset
}

func (p *Prompter) style(s string, codes ...string) string {
	if !p.color || s == "" {
		return s
	}
	return strings.Join(codes, "") + s + reset
}

// Title prints the heading that opens a run.
func (p *Prompter) Title(name, sub string) {
	fmt.Fprintf(p.out, "\n  %s  %s\n\n", p.style("◆ "+name, bold, accent), p.style(sub, dim))
}

// Question is one thing to ask.
type Question struct {
	Label   string
	Hint    string // a short dim explanation after the label
	Default string // used on an empty answer; empty = an answer is required
	// Validate checks an answer (the default included); nil accepts any.
	Validate func(string) error
}

// Ask asks q until it gets a valid answer.
func (p *Prompter) Ask(q Question) (string, error) {
	for lines := 1; ; lines += 2 {
		line := "  " + p.style("❯", accent, bold) + " " + p.style(q.Label, bold)
		if q.Hint != "" {
			line += " " + p.style("· "+q.Hint, dim)
		}
		if q.Default != "" {
			line += " " + p.style("("+q.Default+")", dim)
		}
		fmt.Fprint(p.out, line+" ")
		ans, err := p.readLine()
		if err != nil {
			return "", err
		}
		if ans == "" {
			ans = q.Default
		}
		if ans == "" {
			err = errors.New("an answer is required")
		} else if q.Validate != nil {
			err = q.Validate(ans)
		}
		if err != nil {
			fmt.Fprintf(p.out, "    %s\n", p.style("✖ "+err.Error(), red))
			continue
		}
		p.answered(lines, q.Label, ans)
		return ans, nil
	}
}

// Confirm asks a yes/no question; an empty answer takes def.
func (p *Prompter) Confirm(label string, def bool) (bool, error) {
	choices := "y/N"
	if def {
		choices = "Y/n"
	}
	for lines := 1; ; lines += 2 {
		fmt.Fprintf(p.out, "  %s %s %s ", p.style("❯", accent, bold), p.style(label, bold), p.style("["+choices+"]", dim))
		ans, err := p.readLine()
		if err != nil {
			return false, err
		}
		yes := def
		switch strings.ToLower(ans) {
		case "":
		case "y", "yes":
			yes = true
		case "n", "no":
			yes = false
		default:
			fmt.Fprintf(p.out, "    %s\n", p.style("✖ answer y or n", red))
			continue
		}
		word := "no"
		if yes {
			word = "yes"
		}
		p.answered(lines, label, word)
		return yes, nil
	}
}

// answered redraws the question as one ✔ line (in colour), clearing its
// lines on screen: the prompt, plus a rejected answer and its ✖ for each
// retry.
func (p *Prompter) answered(lines int, label, ans string) {
	if !p.color {
		return
	}
	fmt.Fprintf(p.out, "%s  %s %s %s\n", strings.Repeat(up, lines), p.style("✔", green), p.style(fmt.Sprintf("%-*s", p.labelWidth, label), dim), p.style(ans, cyan))
}

// Warn prints a ⚠ line.
func (p *Prompter) Warn(msg string) { fmt.Fprintf(p.out, "  %s %s\n", p.style("⚠", yellow), msg) }

// Done prints a ✔ status line.
func (p *Prompter) Done(msg string) { fmt.Fprintf(p.out, "  %s %s\n", p.style("✔", green), msg) }

// Note prints a dim line.
func (p *Prompter) Note(msg string) { fmt.Fprintf(p.out, "  %s\n", p.style(msg, dim)) }

func (p *Prompter) readLine() (string, error) {
	s, err := p.in.ReadString('\n')
	if err != nil && (s == "" || !errors.Is(err, io.EOF)) {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(p.out)
			return "", ErrAborted
		}
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// AlignLabels pads every answered label to the longest of labels, so the
// values line up in one column.
func (p *Prompter) AlignLabels(labels ...string) {
	for _, l := range labels {
		p.labelWidth = max(p.labelWidth, len([]rune(l)))
	}
}
