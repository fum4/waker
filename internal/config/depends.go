package config

import (
	"fmt"
	"slices"
	"strings"
)

// validateDependsOn checks every app's depends_on against the configured
// names (unknown, itself, listed twice), then reports each cycle once.
func (c *Config) validateDependsOn(names map[string]int, add addFunc) {
	edges := make([][]int, len(c.Apps))
	for i, a := range c.Apps {
		field, appAdd := c.appField(i, add)
		seen := map[string]bool{}
		for j, dep := range a.DependsOn {
			f := fmt.Sprintf("%s.depends_on[%d]", field, j)
			k, known := names[dep]
			switch {
			case dep == a.Name:
				appAdd(f, "%s depends on itself", a.Name)
			case !known:
				appAdd(f, "%q is not a configured app", dep)
			case seen[dep]:
				appAdd(f, "%q is listed twice", dep)
			default:
				edges[i] = append(edges[i], k)
			}
			seen[dep] = true
		}
	}
	c.reportCycles(edges, add)
}

// reportCycles walks the dependency graph depth-first; each back edge
// closes one cycle, reported on the app it starts from.
func (c *Config) reportCycles(edges [][]int, add addFunc) {
	const (
		unseen = iota
		open
		done
	)
	colour := make([]int, len(c.Apps))
	var path []int
	var visit func(i int)
	visit = func(i int) {
		colour[i] = open
		path = append(path, i)
		for _, k := range edges[i] {
			switch colour[k] {
			case unseen:
				visit(k)
			case open:
				var cycle []string
				for _, n := range path[slices.Index(path, k):] {
					cycle = append(cycle, c.Apps[n].Name)
				}
				field, appAdd := c.appField(k, add)
				appAdd(field+".depends_on", "cycle %s", strings.Join(append(cycle, c.Apps[k].Name), " → "))
			}
		}
		path = path[:len(path)-1]
		colour[i] = done
	}
	for i := range c.Apps {
		if colour[i] == unseen {
			visit(i)
		}
	}
}
