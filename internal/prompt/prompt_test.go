package prompt

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestAsk(t *testing.T) {
	var out bytes.Buffer
	p := New(strings.NewReader("\nBAD\nok\n"), &out, false)
	got, err := p.Ask(Question{Label: "Name", Default: "blog"})
	if err != nil || got != "blog" {
		t.Fatalf("empty answer = %q, %v; want the default", got, err)
	}
	upper := func(s string) error {
		if strings.ToLower(s) != s {
			return errors.New("lowercase only")
		}
		return nil
	}
	got, err = p.Ask(Question{Label: "Again", Hint: "lowercase", Validate: upper})
	if err != nil || got != "ok" {
		t.Fatalf("= %q, %v", got, err)
	}
	for _, want := range []string{"❯ Name (blog) ", "❯ Again · lowercase ", "✖ lowercase only"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("colour off, yet ANSI in output: %q", out.String())
	}
}

func TestAskRequiresAnAnswer(t *testing.T) {
	var out bytes.Buffer
	p := New(strings.NewReader("\nnpm start\n"), &out, false)
	if got, err := p.Ask(Question{Label: "Start"}); err != nil || got != "npm start" {
		t.Fatalf("= %q, %v", got, err)
	}
	if !strings.Contains(out.String(), "✖ an answer is required") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		in   string
		def  bool
		want bool
	}{{"\n", true, true}, {"\n", false, false}, {"n\n", true, false}, {"YES\n", false, true}, {"maybe\ny\n", false, true}}
	for _, tt := range tests {
		got, err := New(strings.NewReader(tt.in), &bytes.Buffer{}, false).Confirm("Install?", tt.def)
		if err != nil || got != tt.want {
			t.Errorf("Confirm(%q, %v) = %v, %v; want %v", tt.in, tt.def, got, err, tt.want)
		}
	}
}

func TestEndOfInputAborts(t *testing.T) {
	p := New(strings.NewReader(""), &bytes.Buffer{}, false)
	if _, err := p.Ask(Question{Label: "Name", Default: "x"}); !errors.Is(err, ErrAborted) {
		t.Errorf("Ask at EOF = %v, want ErrAborted", err)
	}
	if _, err := p.Confirm("Install?", true); !errors.Is(err, ErrAborted) {
		t.Errorf("Confirm at EOF = %v, want ErrAborted", err)
	}
	// A last line without a newline is still an answer.
	if got, err := New(strings.NewReader("blog"), &bytes.Buffer{}, false).Ask(Question{Label: "Name"}); err != nil || got != "blog" {
		t.Errorf("unterminated answer = %q, %v", got, err)
	}
}

func TestColourRedrawsTheAnsweredLine(t *testing.T) {
	var out bytes.Buffer
	if _, err := New(strings.NewReader("\n"), &out, true).Ask(Question{Label: "Name", Default: "blog"}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, up+"  "+green+"✔"+reset) || !strings.Contains(s, cyan+"blog"+reset) {
		t.Errorf("no ✔ redraw: %q", s)
	}
}

func TestAlignLabels(t *testing.T) {
	var out bytes.Buffer
	p := New(strings.NewReader("a\nb\n"), &out, true)
	p.AlignLabels("Name", "Start command")
	for _, l := range []string{"Name", "Start command"} {
		if _, err := p.Ask(Question{Label: l}); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out.String(), dim+"Name         "+reset) || !strings.Contains(out.String(), dim+"Start command"+reset) {
		t.Errorf("labels not padded to one width: %q", out.String())
	}
}
