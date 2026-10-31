package yamledit

import (
	"strings"
	"testing"
)

const house = `# waker.yaml
apps:
  - name: web       # the app
    listen: 0.0.0.0:8080

service:
  # how it restarts
  restart: always   # aligned comment
  path:    /usr/bin
  env: {A: "1"}

# trailing note
`

func TestSet(t *testing.T) {
	tests := []struct {
		name, src, key string
		value          any
		want           string
	}{
		{"replace keeps the comment and alignment", house, "restart", "never",
			strings.Replace(house, "restart: always ", "restart: never ", 1)},
		{"replace a padded value", house, "path", "/opt/bin:/usr/bin",
			strings.Replace(house, "path:    /usr/bin", "path:    /opt/bin:/usr/bin", 1)},
		{"a new key goes first, at the block's indent", house, "system", true,
			strings.Replace(house, "service:\n", "service:\n  system: true\n", 1)},
		{"a missing section is appended", "apps: []\n", "start", false,
			"apps: []\n\nservice:\n  start: false\n"},
		{"appended after a blank line without adding another", "apps: []\n\n", "start", false,
			"apps: []\n\nservice:\n  start: false\n"},
		{"appended to a file without a final newline", "apps: []", "start", false,
			"apps: []\n\nservice:\n  start: false\n"},
		{"flow: replace", "service: {restart: on-failure, path: '/a:/b'}\n", "path", "/c",
			"service: {restart: on-failure, path: /c}\n"},
		{"flow: replace a quoted value", "service: {path: '/a:/b', restart: never}\n", "restart", "always",
			"service: {path: '/a:/b', restart: always}\n"},
		{"flow: add quotes what flow can't hold", "service: {restart: never}\n", "path", "/a:/b",
			`service: {restart: never, path: "/a:/b"}` + "\n"},
		{"flow: add to an empty mapping", "service: {}\n", "user", "www",
			"service: {user: www}\n"},
		{"flow: add after a nested mapping", "service: {env: {A: '1'}}  # x\n", "start", true,
			"service: {env: {A: '1'}, start: true}  # x\n"},
		{"empty section", "service:\napps: []\n", "restart", "never",
			"service:\n  restart: never\napps: []\n"},
		{"null section", "service: ~ # none\n", "restart", "never",
			"service: {restart: never} # none\n"},
		{"a string that reads as a bool is quoted", house, "user", "true",
			strings.Replace(house, "service:\n", "service:\n  user: \"true\"\n", 1)},
		{"a quoted value with escapes", "service:\n  path: \"/a\\\"b\" # c\n", "path", "/x",
			"service:\n  path: /x # c\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Set([]byte(tt.src), "service", tt.key, tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestSetRefuses(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"multi-line value", "service:\n  path: |\n    /a\n", "only a one-line value"},
		{"not a mapping", "service: [a]\n", "is not a mapping"},
		{"flow top level", "{service: {}}\n", "not a block mapping"},
		{"empty file", "", "empty"},
		{"first key on the section's line", "service: &s\n  path: /a\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Set([]byte(tt.src), "service", "path", "/x")
			if tt.want == "" {
				return // only must not panic; either outcome is verified
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v; want %q", err, tt.want)
			}
		})
	}
}

func TestSetRejectsOtherTypes(t *testing.T) {
	if _, err := Set([]byte("a: 1\n"), "service", "x", 3); err == nil {
		t.Error("an int value was accepted")
	}
}
