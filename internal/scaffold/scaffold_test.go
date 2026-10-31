package scaffold

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

func sample() Answers {
	return Answers{
		Name: "blog", Listen: DefaultListen, UpstreamPort: 3000,
		Start:      []string{"python3", "-m", "http.server", "3000", "--bind", "127.0.0.1"},
		HealthPath: "/", Idle: "30m",
		ServicePath: "~/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
}

// The file init writes, byte for byte: the house style is reviewed here.
func TestRenderGolden(t *testing.T) {
	got := sample().Render()
	golden := filepath.Join("testdata", "waker.yaml.golden")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("Render() differs from %s (go test -update to accept):\n%s", golden, got)
	}
}

// Whatever the answers, the file waker reads back says the same thing.
func TestRenderRoundTrips(t *testing.T) {
	odd := []string{"node", "server.js", "--port=3000", "on", "yes", "-", "-1", "@scope/pkg", "%x", "a b", `say "hi"`, "#tag", "x:", "ünï", "~", ""}
	a := sample()
	a.Name, a.Listen, a.Start, a.HealthPath, a.Idle = "my-app-2", "127.0.0.1:8080", odd, "/healthz?full=1", "1h30m"
	cfg, err := config.Parse(a.Render())
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, a.Render())
	}
	app := cfg.Apps[0]
	if !slices.Equal(app.Start, odd) {
		t.Errorf("start = %q, want %q\n%s", app.Start, odd, a.Render())
	}
	if app.Name != "my-app-2" || app.Listen != "127.0.0.1:8080" || app.Upstream != "http://127.0.0.1:3000" ||
		app.Health.Path != "/healthz?full=1" || app.Idle.String() != "1h30m0s" {
		t.Errorf("app = %+v", app)
	}
}

func TestScalarQuotesOnlyWhenNeeded(t *testing.T) {
	for in, want := range map[string]string{
		"python3": "python3", "--bind": "--bind", "-m": "-m", "127.0.0.1": "127.0.0.1",
		"3000": `"3000"`, "on": `"on"`, "No": `"No"`, "-": `"-"`, "-1": `"-1"`,
		"a b": `"a b"`, "@x": `"@x"`, "x:": `"x:"`, "": `""`,
	} {
		if got := scalar(in); got != want {
			t.Errorf("scalar(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDefaultName(t *testing.T) {
	for dir, want := range map[string]string{
		"/srv/My Blog": "my-blog", "/srv/api_v2": "api-v2", "/srv/---": "app", "/": "app",
		"/x/" + strings.Repeat("a", 80): strings.Repeat("a", 63),
	} {
		if got := DefaultName(dir); got != want {
			t.Errorf("DefaultName(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := sample().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := Answers{Name: "Bad Name", Listen: "0.0.0.0:3000", UpstreamPort: 3000, HealthPath: "health", Idle: "soon"}
	err := bad.Validate()
	for _, want := range []string{"name:", "can't both listen on 3000", "start:", "health path:", "idle:"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %v, want %q", err, want)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"npm run start", []string{"npm", "run", "start"}},
		{"  python3   -m http.server  ", []string{"python3", "-m", "http.server"}},
		{`sh -c 'echo "$HOME"'`, []string{"sh", "-c", `echo "$HOME"`}},
		{`say "a \"b\" \\ c"`, []string{"say", `a "b" \ c`}},
		{`a\ b c`, []string{"a b", "c"}},
		{`x ''`, []string{"x", ""}},
		{"", nil},
	}
	for _, tt := range tests {
		got, err := SplitArgs(tt.in)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("SplitArgs(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{`echo 'oops`, `echo "oops`, `trailing\`} {
		if _, err := SplitArgs(bad); err == nil {
			t.Errorf("SplitArgs(%q) = nil error", bad)
		}
	}
}

func TestListen(t *testing.T) {
	for s, loop := range map[string]bool{"127.0.0.1:8080": true, "localhost:80": true, "[::1]:80": true, "0.0.0.0:8080": false, "[::]:80": false, "192.168.1.5:80": false} {
		if _, err := ParseListen(s); err != nil {
			t.Errorf("ParseListen(%q) = %v", s, err)
		}
		if Loopback(s) != loop {
			t.Errorf("Loopback(%q) = %v", s, !loop)
		}
	}
	for _, bad := range []string{"8080", ":8080", "example.com:80", "0.0.0.0:0", "0.0.0.0:99999", "0.0.0.0:http"} {
		if _, err := ParseListen(bad); err == nil {
			t.Errorf("ParseListen(%q) = nil error", bad)
		}
	}
	if !strings.Contains(string(Answers{Name: "x", Listen: "127.0.0.1:8080", UpstreamPort: 3000, Start: []string{"x"}, HealthPath: "/", Idle: "1m"}.Render()), "# this machine only") {
		t.Error("a loopback listen should say so")
	}
}
