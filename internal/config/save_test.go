package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedBy(t *testing.T) {
	tests := []struct{ src, want string }{
		{"# managed: chezmoi\napps: []\n", "chezmoi"},
		{"# waker.yaml\n#managed:ansible\n\napps: []\n", "ansible"},
		{"\n# header\n#   managed:  terraform  # why\napps: []\n", "terraform"},
		{"apps: []\n# managed: chezmoi\n", ""}, // after the first key: not the header
		{"%YAML 1.2\n---\n# managed: chezmoi\napps: []\n", "chezmoi"},
		{"# manages: x\napps: []\n", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ManagedBy([]byte(tt.src)); got != tt.want {
			t.Errorf("ManagedBy(%q) = %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestEditServiceRefusesManaged(t *testing.T) {
	_, err := EditService("w.yaml", []byte("# managed: chezmoi\napps: []\n"), []ServiceKey{{"restart", "never"}})
	var m *ManagedError
	if !errors.As(err, &m) || m.Tool != "chezmoi" {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteFileKeepsModeAndFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.yaml")
	link := filepath.Join(dir, "waker.yaml")
	if err := os.WriteFile(real, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(link, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v %v", fi, err)
	}
	b, _ := os.ReadFile(real)
	fi, _ := os.Stat(real)
	if string(b) != "new\n" || fi.Mode().Perm() != 0o600 {
		t.Errorf("real = %q %v", b, fi.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".real.yaml.*")); len(left) != 0 {
		t.Errorf("temp files left: %q", left)
	}
}
