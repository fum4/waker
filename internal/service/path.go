package service

import (
	"os"
	"path/filepath"
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
