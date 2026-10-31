// Package updatetest serves fixture release homes laid out the way
// fum4/releases publishes them (latest, <tag>/SHA256SUMS + .sig, RELEASE,
// one binary per platform), signed with a key of the test's own.
package updatetest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fum4/waker/internal/update"
)

// Home is a fixture release home over HTTP.
type Home struct {
	t    *testing.T
	Dir  string // the files, as <Dir>/waker/...
	URL  string // the server's root; the release base is URL + "/waker"
	Key  update.PublicKey
	priv ed25519.PrivateKey
}

// New starts an empty release home with a fresh signing key.
func New(t *testing.T) *Home {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &Home{t: t, Dir: t.TempDir(), Key: update.NewPublicKey(pub), priv: priv}
	srv := httptest.NewServer(http.FileServer(http.Dir(h.Dir)))
	t.Cleanup(srv.Close)
	h.URL = srv.URL
	return h
}

// Source is the release home as waker's update source.
func (h *Home) Source() update.Source {
	return update.Source{Base: h.URL + "/" + update.Product, Key: h.Key}
}

// Script is a stand-in waker binary for this platform that reports tag
// (the dry run's `version`).
func Script(tag string) []byte {
	return []byte("#!/bin/sh\necho \"waker " + tag[1:] + "\"\n")
}

// Publish writes tag's release with bin as this platform's binary and
// moves latest to it. Tamper afterwards with Write.
func (h *Home) Publish(tag string, bin []byte) {
	h.t.Helper()
	files := map[string][]byte{
		update.Asset(): bin,
		"RELEASE":      []byte(update.Product + " " + tag + "\n"),
	}
	var sums []byte
	for _, name := range []string{"RELEASE", update.Asset()} {
		s := sha256.Sum256(files[name])
		sums = append(sums, fmt.Sprintf("%s  %s\n", hex.EncodeToString(s[:]), name)...)
	}
	files["SHA256SUMS"] = sums
	files["SHA256SUMS.sig"] = h.Key.Sign(h.priv, update.Namespace, sums)
	for name, b := range files {
		h.Write(tag+"/"+name, b)
	}
	h.Write("latest", []byte(tag+"\n"))
}

// Write replaces one file of the home (path under waker/).
func (h *Home) Write(path string, b []byte) {
	h.t.Helper()
	p := filepath.Join(h.Dir, update.Product, path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		h.t.Fatal(err)
	}
}
