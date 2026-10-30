package config

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// depsConfig builds apps api, db and cache; deps holds each one's
// depends_on (YAML flow list, "" = omitted), defs a defaults body.
func depsConfig(defs string, deps ...string) string {
	var b strings.Builder
	if defs != "" {
		b.WriteString("defaults:\n" + defs)
	}
	b.WriteString("apps:\n")
	for i, name := range []string{"api", "db", "cache"} {
		fmt.Fprintf(&b, "  - {name: %s, listen: '127.0.0.1:%d', upstream: 'http://127.0.0.1:%d', start: [x]", name, 8080+i, 3000+i)
		if i < len(deps) && deps[i] != "" {
			b.WriteString(", depends_on: " + deps[i])
		}
		b.WriteString("}\n")
	}
	return b.String()
}

func TestDependsOnValid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want [][]string
	}{
		{"chain", depsConfig("", "[db, cache]", "[cache]"), [][]string{{"db", "cache"}, {"cache"}, nil}},
		{"inherited, app's empty list wins", depsConfig("  depends_on: [cache]\n", "", "", "[]"), [][]string{{"cache"}, {"cache"}, {}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
			for i, w := range tt.want {
				if !slices.Equal(cfg.Apps[i].DependsOn, w) {
					t.Errorf("%s depends_on = %v, want %v", cfg.Apps[i].Name, cfg.Apps[i].DependsOn, w)
				}
			}
		})
	}
}

// A defaults list naming an app applies to every other app; the named one
// drops itself. Its own list naming itself stays an error (see below).
func TestInheritedDependsOnDropsTheAppItself(t *testing.T) {
	cfg, err := Parse([]byte(depsConfig("  depends_on: [db]\n")))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"api": {"db"}, "db": {}, "cache": {"db"}}
	for _, a := range cfg.Apps {
		if !slices.Equal(a.DependsOn, want[a.Name]) {
			t.Errorf("%s depends_on = %v, want %v", a.Name, a.DependsOn, want[a.Name])
		}
	}
}

func TestDependsOnProblems(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string // each exactly once
		not  []string
	}{
		{"unknown", depsConfig("", "", "[ghost]"),
			[]string{`apps[1] (db).depends_on[0]: "ghost" is not a configured app`}, nil},
		{"self", depsConfig("", "[db, api]"),
			[]string{`apps[0] (api).depends_on[1]: api depends on itself`}, []string{"cycle"}},
		{"duplicate", depsConfig("", "[db, db]"),
			[]string{`apps[0] (api).depends_on[1]: "db" is listed twice`}, nil},
		{"two-node cycle", depsConfig("", "[db]", "[api]"),
			[]string{`apps[0] (api).depends_on: cycle api → db → api`}, []string{"(db).depends_on: cycle"}},
		{"three-node cycle", depsConfig("", "[db]", "[cache]", "[api]"),
			[]string{`apps[0] (api).depends_on: cycle api → db → cache → api`}, []string{"(db).depends_on: cycle", "(cache).depends_on: cycle"}},
		{"cycle not through the start", depsConfig("", "[db]", "[cache]", "[db]"),
			[]string{`apps[1] (db).depends_on: cycle db → cache → db`}, []string{"api →"}},
		{"inherited unknown: defaults path, once", depsConfig("  depends_on: [ghost]\n"),
			[]string{`defaults.depends_on[0]: "ghost" is not a configured app`}, []string{"apps["}},
		{"app's own value: app path", depsConfig("  depends_on: [db]\n", "", "[ghost]", "[]"),
			[]string{`apps[1] (db).depends_on[0]: "ghost"`}, []string{"defaults.depends_on"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}
			verr := cfg.Validate()
			if verr == nil {
				t.Fatal("Validate passed")
			}
			msg := verr.Error()
			for _, w := range tt.want {
				if strings.Count(msg, w) != 1 {
					t.Errorf("want %q once in:\n%s", w, msg)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(msg, n) {
					t.Errorf("unexpected %q in:\n%s", n, msg)
				}
			}
		})
	}
}
