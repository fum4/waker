package update

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Tag is a release tag, vMAJOR.MINOR.PATCH (install.sh accepts no other).
type Tag struct{ Major, Minor, Patch int }

var tagPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// ErrNotATag is a version that isn't vN.N.N (a dev build's "dev").
var ErrNotATag = errors.New("not a release version")

// ParseTag parses "v1.2.3"; the leading v is optional, since a binary
// reports its version without it.
func ParseTag(s string) (Tag, error) {
	m := tagPattern.FindStringSubmatch(s)
	if m == nil {
		return Tag{}, fmt.Errorf("%q: %w (want vN.N.N)", s, ErrNotATag)
	}
	var n [3]int
	for i := range n {
		n[i], _ = strconv.Atoi(m[i+1])
	}
	return Tag{n[0], n[1], n[2]}, nil
}

// Version is a running build's version: a release tag, a pre-release or
// Go pseudo-version built from one (v0.4.1-0.20261030-abcdef, older than
// v0.4.1 but newer than v0.4.0), or unknown ("dev").
type Version struct {
	Core  Tag
	Pre   bool // a suffix after the core: before Core's release
	Known bool
}

// ParseVersion parses a build's version; anything without a vN.N.N core
// is unknown.
func ParseVersion(s string) Version {
	core, suffix, cut := strings.Cut(s, "-")
	if !cut {
		core, _, _ = strings.Cut(s, "+")
	}
	t, err := ParseTag(core)
	if err != nil {
		return Version{}
	}
	return Version{Core: t, Pre: cut && suffix != "", Known: true}
}

// String is the version as a tag, with "-…" for a pre-release.
func (v Version) String() string {
	switch {
	case !v.Known:
		return "dev"
	case v.Pre:
		return v.Core.String() + "-…"
	}
	return v.Core.String()
}

// OlderThan reports whether release t is newer than v; an unknown
// version is older than every release.
func (v Version) OlderThan(t Tag) bool {
	if !v.Known {
		return true
	}
	c := t.Compare(v.Core)
	return c > 0 || (c == 0 && v.Pre)
}

func (t Tag) String() string { return fmt.Sprintf("v%d.%d.%d", t.Major, t.Minor, t.Patch) }

// Compare is -1, 0 or 1 as t is older than, the same as or newer than o.
func (t Tag) Compare(o Tag) int {
	for _, d := range []int{t.Major - o.Major, t.Minor - o.Minor, t.Patch - o.Patch} {
		if d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ChangelogURL is where a release's changes are listed.
func ChangelogURL(t Tag) string {
	return "https://github.com/fum4/waker/blob/" + t.String() + "/CHANGELOG.md"
}
