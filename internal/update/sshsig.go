package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// The release signature is an OpenSSH SSHSIG (`ssh-keygen -Y sign`), the
// format install.sh checks with `ssh-keygen -Y verify`; waker verifies it
// itself, so `waker update` needs no OpenSSH. Only ed25519 keys are
// accepted: the release key is one.
const (
	sigMagic  = "SSHSIG"
	sigBegin  = "-----BEGIN SSH SIGNATURE-----"
	sigEnd    = "-----END SSH SIGNATURE-----"
	keyType   = "ssh-ed25519"
	sigFormat = 1
)

// ErrBadSignature is any signature that doesn't verify.
var ErrBadSignature = errors.New("bad signature")

// PublicKey is an ed25519 key from an authorized_keys-style line
// ("ssh-ed25519 AAAA… comment").
type PublicKey struct {
	wire []byte // the SSH wire encoding, as embedded in a signature
	key  ed25519.PublicKey
}

// ParsePublicKey parses an "ssh-ed25519 <base64> [comment]" line.
func ParsePublicKey(line string) (PublicKey, error) {
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != keyType {
		return PublicKey{}, fmt.Errorf("not an %s key", keyType)
	}
	wire, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return PublicKey{}, fmt.Errorf("public key: %w", err)
	}
	return parseWireKey(wire)
}

func parseWireKey(wire []byte) (PublicKey, error) {
	r := reader{b: wire}
	typ, key := r.str(), r.str()
	if r.err != nil || string(typ) != keyType || len(key) != ed25519.PublicKeySize || len(r.b) != 0 {
		return PublicKey{}, fmt.Errorf("not an %s key", keyType)
	}
	return PublicKey{wire: wire, key: ed25519.PublicKey(key)}, nil
}

// Verify checks that armored is k's SSHSIG over msg in namespace.
func (k PublicKey) Verify(namespace string, msg, armored []byte) error {
	blob, err := dearmor(armored)
	if err != nil {
		return err
	}
	r := reader{b: blob}
	magic := r.next(len(sigMagic))
	version := r.u32()
	pub, ns, reserved, hashAlg, sig := r.str(), r.str(), r.str(), r.str(), r.str()
	if r.err != nil || string(magic) != sigMagic || len(r.b) != 0 {
		return fmt.Errorf("%w: malformed", ErrBadSignature)
	}
	if version != sigFormat {
		return fmt.Errorf("%w: version %d", ErrBadSignature, version)
	}
	if !bytes.Equal(pub, k.wire) {
		return fmt.Errorf("%w: signed by another key", ErrBadSignature)
	}
	if string(ns) != namespace {
		return fmt.Errorf("%w: namespace %q, want %q", ErrBadSignature, ns, namespace)
	}
	var digest []byte
	switch string(hashAlg) {
	case "sha512":
		d := sha512.Sum512(msg)
		digest = d[:]
	case "sha256":
		d := sha256.Sum256(msg)
		digest = d[:]
	default:
		return fmt.Errorf("%w: hash %q", ErrBadSignature, hashAlg)
	}
	sr := reader{b: sig}
	format, raw := sr.str(), sr.str()
	if sr.err != nil || string(format) != keyType || len(sr.b) != 0 {
		return fmt.Errorf("%w: not an %s signature", ErrBadSignature, keyType)
	}
	if !ed25519.Verify(k.key, signedData(ns, reserved, hashAlg, digest), raw) {
		return ErrBadSignature
	}
	return nil
}

// Sign makes the armored SSHSIG (sha512) of msg by priv, whose public half
// is k: what `ssh-keygen -Y sign` writes. Release signing is
// fum4/releases' job; this is for test fixtures.
func (k PublicKey) Sign(priv ed25519.PrivateKey, namespace string, msg []byte) []byte {
	d := sha512.Sum512(msg)
	raw := ed25519.Sign(priv, signedData([]byte(namespace), nil, []byte("sha512"), d[:]))
	var sig, blob bytes.Buffer
	putString(&sig, []byte(keyType))
	putString(&sig, raw)
	blob.WriteString(sigMagic)
	_ = binary.Write(&blob, binary.BigEndian, uint32(sigFormat))
	for _, s := range [][]byte{k.wire, []byte(namespace), nil, []byte("sha512"), sig.Bytes()} {
		putString(&blob, s)
	}
	enc := base64.StdEncoding.EncodeToString(blob.Bytes())
	var out strings.Builder
	out.WriteString(sigBegin + "\n")
	for len(enc) > 70 {
		out.WriteString(enc[:70] + "\n")
		enc = enc[70:]
	}
	out.WriteString(enc + "\n" + sigEnd + "\n")
	return []byte(out.String())
}

// NewPublicKey wraps an ed25519 public key.
func NewPublicKey(key ed25519.PublicKey) PublicKey {
	var b bytes.Buffer
	putString(&b, []byte(keyType))
	putString(&b, key)
	return PublicKey{wire: b.Bytes(), key: key}
}

// signedData is what an SSHSIG signs: the magic, then the namespace,
// reserved field, hash name and message digest as SSH strings.
func signedData(ns, reserved, hashAlg, digest []byte) []byte {
	var b bytes.Buffer
	b.WriteString(sigMagic)
	for _, s := range [][]byte{ns, reserved, hashAlg, digest} {
		putString(&b, s)
	}
	return b.Bytes()
}

func dearmor(armored []byte) ([]byte, error) {
	lines := strings.Split(strings.TrimSpace(string(armored)), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != sigBegin || strings.TrimSpace(lines[len(lines)-1]) != sigEnd {
		return nil, fmt.Errorf("%w: not an SSH signature", ErrBadSignature)
	}
	var body strings.Builder
	for _, l := range lines[1 : len(lines)-1] {
		body.WriteString(strings.TrimSpace(l))
	}
	blob, err := base64.StdEncoding.DecodeString(body.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadSignature, err)
	}
	return blob, nil
}

func putString(b *bytes.Buffer, s []byte) {
	_ = binary.Write(b, binary.BigEndian, uint32(len(s)))
	b.Write(s)
}

// reader reads SSH wire fields; the first short read sets err and every
// later read returns nil.
type reader struct {
	b   []byte
	err error
}

func (r *reader) next(n int) []byte {
	if r.err != nil || n < 0 || len(r.b) < n {
		r.err = errors.New("short")
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u32() uint32 {
	v := r.next(4)
	if v == nil {
		return 0
	}
	return binary.BigEndian.Uint32(v)
}

func (r *reader) str() []byte {
	n := r.u32()
	if r.err != nil || n > uint32(len(r.b)) {
		r.err = errors.New("short")
		return nil
	}
	return r.next(int(n))
}
