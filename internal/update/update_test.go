package update_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fum4/waker/internal/update"
	"github.com/fum4/waker/internal/update/updatetest"
)

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A signature made by real `ssh-keygen -Y sign` verifies: the format is
// OpenSSH's, not just our own round trip.
func TestVerifyOpenSSHSignature(t *testing.T) {
	k, err := update.ParsePublicKey(string(read(t, "testdata/openssh.pub")))
	if err != nil {
		t.Fatal(err)
	}
	msg, sig := read(t, "testdata/openssh.msg"), read(t, "testdata/openssh.msg.sig")
	if err := k.Verify(update.Namespace, msg, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
	for name, bad := range map[string]func() error{
		"tampered message": func() error { return k.Verify(update.Namespace, append(msg, 'x'), sig) },
		"other namespace":  func() error { return k.Verify("file", msg, sig) },
		"other key":        func() error { return update.ReleaseKey().Verify(update.Namespace, msg, sig) },
		"not armored":      func() error { return k.Verify(update.Namespace, msg, []byte("nope")) },
	} {
		if err := bad(); !errors.Is(err, update.ErrBadSignature) {
			t.Errorf("%s: err = %v, want ErrBadSignature", name, err)
		}
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	k := update.NewPublicKey(pub)
	sig := k.Sign(priv, update.Namespace, []byte("hello"))
	if err := k.Verify(update.Namespace, []byte("hello"), sig); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseKeyParses(t *testing.T) {
	_ = update.ReleaseKey() // panics if the embedded key is broken
}

func TestParseTag(t *testing.T) {
	for in, want := range map[string]string{"v1.2.3": "v1.2.3", "0.3.0": "v0.3.0", "v10.0.1": "v10.0.1"} {
		got, err := update.ParseTag(in)
		if err != nil || got.String() != want {
			t.Errorf("ParseTag(%q) = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"dev", "v1.2", "v1.2.3-rc1", "v01.2.3", ""} {
		if _, err := update.ParseTag(in); !errors.Is(err, update.ErrNotATag) {
			t.Errorf("ParseTag(%q) err = %v", in, err)
		}
	}
	a, _ := update.ParseTag("v0.9.0")
	b, _ := update.ParseTag("v0.10.0")
	if a.Compare(b) != -1 || b.Compare(a) != 1 || a.Compare(a) != 0 {
		t.Error("Compare isn't numeric")
	}
}

func TestFetch(t *testing.T) {
	ctx := context.Background()
	h := updatetest.New(t)
	h.Publish("v1.2.3", updatetest.Script("v1.2.3"))
	src := h.Source()
	tag, err := src.Latest(ctx)
	if err != nil || tag.String() != "v1.2.3" {
		t.Fatalf("latest = %v, %v", tag, err)
	}
	rel, err := src.Fetch(ctx, tag, update.Asset())
	if err != nil || string(rel.Binary) != string(updatetest.Script("v1.2.3")) {
		t.Fatalf("fetch = %q, %v", rel.Binary, err)
	}

	tests := []struct {
		name   string
		tamper func(h *updatetest.Home)
		want   string
	}{
		{"binary swapped", func(h *updatetest.Home) { h.Write("v1.2.3/"+update.Asset(), []byte("evil")) }, "checksum mismatch"},
		{"signed by another key", func(h *updatetest.Home) {
			other := updatetest.New(t)
			other.Publish("v1.2.3", []byte("evil"))
			h.Write("v1.2.3/SHA256SUMS", read(t, filepath.Join(other.Dir, "waker/v1.2.3/SHA256SUMS")))
			h.Write("v1.2.3/SHA256SUMS.sig", read(t, filepath.Join(other.Dir, "waker/v1.2.3/SHA256SUMS.sig")))
		}, "signed by another key"},
		{"an older release under a new tag", func(h *updatetest.Home) {
			// v1.0.0's whole signed release copied to v7.7.7.
			for _, f := range []string{"SHA256SUMS", "SHA256SUMS.sig", "RELEASE", update.Asset()} {
				h.Write("v7.7.7/"+f, read(t, filepath.Join(h.Dir, "waker/v1.0.0", f)))
			}
		}, `signed as "waker v1.0.0", not waker v7.7.7`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := updatetest.New(t)
			h.Publish("v1.0.0", updatetest.Script("v1.0.0"))
			h.Publish("v1.2.3", updatetest.Script("v1.2.3"))
			tc.tamper(h)
			tag := "v1.2.3"
			if strings.Contains(tc.name, "new tag") {
				tag = "v7.7.7"
			}
			tg, _ := update.ParseTag(tag)
			if _, err := h.Source().Fetch(ctx, tg, update.Asset()); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLatestRejectsGarbage(t *testing.T) {
	h := updatetest.New(t)
	h.Write("latest", []byte("<html>oops</html>\n"))
	if _, err := h.Source().Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "not a version tag") {
		t.Fatalf("err = %v", err)
	}
}

func TestInstall(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "waker")
	old := updatetest.Script("v1.0.0")
	if err := os.WriteFile(dest, old, 0o755); err != nil {
		t.Fatal(err)
	}
	tag, _ := update.ParseTag("v1.2.3")
	if err := update.Install(ctx, update.Release{Tag: tag, Binary: updatetest.Script("v1.2.3")}, dest); err != nil {
		t.Fatal(err)
	}
	if string(read(t, dest)) != string(updatetest.Script("v1.2.3")) || string(read(t, dest+".prev")) != string(old) {
		t.Error("dest isn't the new binary, or .prev isn't the old one")
	}
	if st, _ := os.Stat(dest); st.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", st.Mode())
	}
	if got := string(read(t, update.VersionRecord(dest))); got != "v1.2.3\n" {
		t.Errorf("version record = %q", got)
	}
	if update.VersionRecord(dest) != filepath.Join(filepath.Dir(dir), "share", "nomadware-releases", "waker.version") {
		t.Errorf("record at %s, not where install.sh keeps it", update.VersionRecord(dest))
	}
	if _, err := os.Stat(dest + ".new"); !os.IsNotExist(err) {
		t.Error("dest.new left behind")
	}
	// The same build again: unchanged, and a lost record is repaired.
	if err := os.Remove(update.VersionRecord(dest)); err != nil {
		t.Fatal(err)
	}
	if err := update.Install(ctx, update.Release{Tag: tag, Binary: updatetest.Script("v1.2.3")}, dest); !errors.Is(err, update.ErrUnchanged) {
		t.Errorf("same build again: err = %v", err)
	}
	if got := string(read(t, update.VersionRecord(dest))); got != "v1.2.3\n" {
		t.Errorf("record not repaired: %q", got)
	}
}

func TestVersionRecordOnlyForABinDir(t *testing.T) {
	for dest, want := range map[string]string{
		"/usr/local/bin/waker": "/usr/local/share/nomadware-releases/waker.version",
		"/opt/waker/waker":     "",
		"/bin/waker":           "",
	} {
		if got := update.VersionRecord(dest); got != want {
			t.Errorf("VersionRecord(%s) = %q, want %q", dest, got, want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	v040, _ := update.ParseTag("v0.4.0")
	v041, _ := update.ParseTag("v0.4.1")
	tests := []struct {
		in                 string
		older040, older041 bool
	}{
		{"0.4.0", false, true},
		{"v0.4.1-0.20261030120000-abcdef123456", false, true},
		{"v0.4.1-rc1", false, true},
		{"0.4.1", false, false},
		{"dev", true, true},
	}
	for _, tc := range tests {
		v := update.ParseVersion(tc.in)
		if v.OlderThan(v040) != tc.older040 || v.OlderThan(v041) != tc.older041 {
			t.Errorf("%s: older than v0.4.0 = %v, v0.4.1 = %v", tc.in, v.OlderThan(v040), v.OlderThan(v041))
		}
	}
}

// A binary that doesn't report the tag it was released as never replaces
// the working one.
func TestInstallRefusesABinaryThatFailsItsDryRun(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "waker")
	old := updatetest.Script("v1.0.0")
	if err := os.WriteFile(dest, old, 0o755); err != nil {
		t.Fatal(err)
	}
	tag, _ := update.ParseTag("v1.2.3")
	for name, bin := range map[string][]byte{
		"wrong version": updatetest.Script("v9.9.9"),
		"crashes":       []byte("#!/bin/sh\nexit 3\n"),
	} {
		err := update.Install(context.Background(), update.Release{Tag: tag, Binary: bin}, dest)
		if err == nil {
			t.Errorf("%s: installed", name)
		}
		if string(read(t, dest)) != string(old) {
			t.Errorf("%s: dest replaced", name)
		}
		if _, err := os.Stat(dest + ".prev"); !os.IsNotExist(err) {
			t.Errorf("%s: .prev written", name)
		}
	}
}
