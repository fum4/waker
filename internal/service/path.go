package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LookPath finds file on the given PATH (not the process's own), the way
// the service's start commands will be found. A name with a slash is
// checked as is, relative to dir.
func LookPath(file, path, dir string) (string, bool) {
	if strings.Contains(file, "/") {
		if !filepath.IsAbs(file) {
			file = filepath.Join(dir, file)
		}
		return file, executable(file)
	}
	for _, d := range filepath.SplitList(path) {
		if d == "" {
			d = "."
		}
		if !filepath.IsAbs(d) {
			d = filepath.Join(dir, d)
		}
		if p := filepath.Join(d, file); executable(p) {
			return p, true
		}
	}
	return "", false
}

func executable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

var (
	unitPATH  = regexp.MustCompile(`(?m)^Environment="PATH=((?:[^"\\]|\\.)*)"$`)
	plistPATH = regexp.MustCompile(`<key>PATH</key>\s*<string>([^<]*)</string>`)
)

// ServicePATH is the PATH the installed service runs with, and the unit it
// came from; ok is false when no service is installed (or it has no PATH).
func ServicePATH(h Host) (path, unit string, ok bool) {
	m, err := managerFor(h)
	if err != nil {
		return "", "", false
	}
	for _, system := range []bool{false, true} {
		unit := m.unitPath(system)
		b, err := os.ReadFile(unit)
		if err != nil {
			continue
		}
		if g := unitPATH.FindSubmatch(b); g != nil {
			return strings.NewReplacer(`\\`, `\`, `\"`, `"`, "%%", "%").Replace(string(g[1])), unit, true
		}
		if g := plistPATH.FindSubmatch(b); g != nil {
			return unescapeXML(string(g[1])), unit, true
		}
	}
	return "", "", false
}

func unescapeXML(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'", "&#x9;", "\t", "&#xA;", "\n", "&#xD;", "\r", "&amp;", "&").Replace(s)
}
