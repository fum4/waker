package update

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
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
