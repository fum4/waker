// Package state persists what the waker learns at runtime: mode overrides
// (modes.json), learned memory peaks (peaks.json) and serve's self-heal
// loop guard (self-heal.json), all written
// atomically (temp file, fsync, rename, fsync of the directory).
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fum4/waker/internal/config"
)

// File names inside the state directory.
const (
	ModesFile = "modes.json"
	PeaksFile = "peaks.json"
	// SelfHealFile is serve's loop guard: when it last rewrote its unit.
	SelfHealFile = "self-heal.json"
)

// Override is a runtime mode set with `waker mode`, keyed by app name.
type Override struct {
	Mode  config.Mode `json:"mode"`
	Since time.Time   `json:"since"`
	// Idle is an on-demand idle override; zero means the declared idle.
	Idle config.Duration `json:"idle,omitempty"`
}

// Problem returns why an override is invalid, or "" when it is fine.
func (o Override) Problem() string {
	if !o.Mode.Valid() {
		return "mode must be one of on, off, on-demand"
	}
	if o.Idle < 0 {
		return "idle must be positive"
	}
	if o.Idle > 0 && o.Mode != config.ModeOnDemand {
		return "idle applies to on-demand only"
	}
	return ""
}

// Peak is an app's learned memory peak.
type Peak struct {
	MiB int64     `json:"peak_mib"`
	At  time.Time `json:"at"`
}

// LoadOverrides reads modes.json; a missing file is an empty map.
func LoadOverrides(dir string) (map[string]Override, error) {
	m := map[string]Override{}
	return m, readJSON(filepath.Join(dir, ModesFile), &m)
}

// SaveOverrides writes modes.json atomically.
func SaveOverrides(dir string, m map[string]Override) error {
	return writeJSON(filepath.Join(dir, ModesFile), m)
}

// LoadPeaks reads peaks.json; a missing file is an empty map.
func LoadPeaks(dir string) (map[string]Peak, error) {
	m := map[string]Peak{}
	return m, readJSON(filepath.Join(dir, PeaksFile), &m)
}

// SavePeaks writes peaks.json atomically.
func SavePeaks(dir string, m map[string]Peak) error {
	return writeJSON(filepath.Join(dir, PeaksFile), m)
}

// selfHeal is self-heal.json: when serve last rewrote its own unit.
type selfHeal struct {
	At time.Time `json:"at"`
}

// LoadSelfHeal reads when serve last rewrote its unit; zero when never.
func LoadSelfHeal(dir string) (time.Time, error) {
	var s selfHeal
	err := readJSON(filepath.Join(dir, SelfHealFile), &s)
	return s.At, err
}

// SaveSelfHeal records that serve rewrote its unit at t.
func SaveSelfHeal(dir string, t time.Time) error {
	return writeJSON(filepath.Join(dir, SelfHealFile), selfHeal{At: t})
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeJSON replaces path atomically: a reader sees the old or the new file,
// never a torn one, even across a crash.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
