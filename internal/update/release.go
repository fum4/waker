// Package update installs waker's signed releases the way install.sh
// (fum4/releases) does: the release home's SHA256SUMS must carry a valid
// signature by the nomadware release key, RELEASE must read
// "waker <tag>", and the binary must match its checksum, before anything
// replaces the running executable.
package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
)

// Product is the release home's package name and RELEASE's first word.
const Product = "waker"

// DefaultBase is where waker's releases live; NOMADWARE_RELEASES_BASE
// (install.sh's override, for tests and mirrors) replaces the host part.
const (
	DefaultBase = "https://releases.nomadware.io/" + Product
	EnvBase     = "NOMADWARE_RELEASES_BASE"
	// Namespace is the SSHSIG namespace and signer identity.
	Namespace = "nomadware-release"
)

// maxBinary bounds a download (waker is ~10 MiB).
const maxBinary = 256 << 20

//go:embed nomadware-release.pub
var releaseKeyLine string

// ReleaseKey is the key every release is signed with (fum4/releases
// keys/nomadware-release.pub).
func ReleaseKey() PublicKey {
	k, err := ParsePublicKey(releaseKeyLine)
	if err != nil {
		panic("embedded release key: " + err.Error()) // a build that can't verify must not ship
	}
	return k
}

// Base is the release home: NOMADWARE_RELEASES_BASE + "/waker" when set,
// else DefaultBase.
func Base() string {
	if b := os.Getenv(EnvBase); b != "" {
		return strings.TrimRight(b, "/") + "/" + Product
	}
	return DefaultBase
}

// Asset is this platform's binary name in a release.
func Asset() string { return Product + "-" + runtime.GOOS + "-" + runtime.GOARCH }

// Source is a release home and the key its releases must be signed with.
type Source struct {
	Base   string
	Key    PublicKey
	Client *http.Client
}

// Release is one verified release binary.
type Release struct {
	Tag    Tag
	Binary []byte
}

// Latest is the tag the release home's `latest` names.
func (s Source) Latest(ctx context.Context) (Tag, error) {
	b, err := s.get(ctx, "latest", 64)
	if err != nil {
		return Tag{}, err
	}
	line := strings.TrimSuffix(string(b), "\n")
	t, err := ParseTag(line)
	if err != nil || strings.Contains(line, "\n") {
		return Tag{}, fmt.Errorf("%s/latest is not a version tag: %q", s.Base, string(b))
	}
	return t, nil
}

// Fetch downloads asset for tag and verifies it: the signature over
// SHA256SUMS first, then RELEASE ("waker <tag>"), then the binary's sum.
func (s Source) Fetch(ctx context.Context, tag Tag, asset string) (Release, error) {
	sums, err := s.get(ctx, tag.String()+"/SHA256SUMS", 1<<16)
	if err != nil {
		return Release{}, err
	}
	sig, err := s.get(ctx, tag.String()+"/SHA256SUMS.sig", 1<<16)
	if err != nil {
		return Release{}, err
	}
	if err := s.Key.Verify(Namespace, sums, sig); err != nil {
		return Release{}, fmt.Errorf("%s %s: SHA256SUMS: %w; do not use it, report it", Product, tag, err)
	}
	rel, err := s.verified(ctx, tag, sums, "RELEASE", 1<<10)
	if err != nil {
		return Release{}, err
	}
	if want := Product + " " + tag.String() + "\n"; string(rel) != want {
		return Release{}, fmt.Errorf("%s/%s is signed as %q, not %s %s; do not use it, report it", s.Base, tag, strings.TrimSpace(string(rel)), Product, tag)
	}
	bin, err := s.verified(ctx, tag, sums, asset, maxBinary)
	if err != nil {
		return Release{}, err
	}
	return Release{Tag: tag, Binary: bin}, nil
}

// verified fetches name from tag's release and checks it against sums.
func (s Source) verified(ctx context.Context, tag Tag, sums []byte, name string, limit int64) ([]byte, error) {
	want, ok := lookupSum(sums, name)
	if !ok {
		return nil, fmt.Errorf("%s %s has no %s", Product, tag, name)
	}
	b, err := s.get(ctx, tag.String()+"/"+name, limit)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(b)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("checksum mismatch for %s %s/%s; do not use it, report it", Product, tag, name)
	}
	return b, nil
}

// lookupSum finds name's hash in a sha256sum listing ("<hex>  name", or
// "<hex> *name" in binary mode).
func lookupSum(sums []byte, name string) (string, bool) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// get reads path under the base: https(s) through the client, file://
// from disk (install.sh's tests use file://), at most limit bytes.
func (s Source) get(ctx context.Context, path string, limit int64) ([]byte, error) {
	u := s.Base + "/" + path
	var body io.ReadCloser
	if rest, ok := strings.CutPrefix(u, "file://"); ok {
		p, err := url.PathUnescape(rest)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", u, err)
		}
		body = f
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		c := s.Client
		if c == nil {
			c = http.DefaultClient
		}
		resp, err := c.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", u, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("fetch %s: %s", u, resp.Status)
		}
		body = resp.Body
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", u, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("fetch %s: larger than %d bytes", u, limit)
	}
	return b, nil
}

// sameBytes reports whether path already holds b (install.sh's "already
// this build").
func sameBytes(path string, b []byte) bool {
	cur, err := os.ReadFile(path)
	return err == nil && bytes.Equal(cur, b)
}
