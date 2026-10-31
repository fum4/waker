package update

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
)

// MarkerSuffix names the file beside the executable that says an
// automatic update is being tried. It sits next to the binary, not in the
// state dir, so serve finds it before it loads a config the new binary
// might reject.
const MarkerSuffix = ".update-pending"

// Marker is an automatic update in flight.
type Marker struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Attempts counts the new binary's starts; a second one means the
	// first never reached serving.
	Attempts int `json:"attempts"`
	// RolledBack: the old binary is back; it records the outcome and
	// removes the marker once it has loaded its config.
	RolledBack bool `json:"rolled_back,omitempty"`
}

// ReadMarker reads exe's marker; ok is false when there is none (or it is
// unreadable, which leaves nothing to resume).
func ReadMarker(exe string) (m Marker, ok bool) {
	b, err := os.ReadFile(exe + MarkerSuffix)
	if err != nil || json.Unmarshal(b, &m) != nil {
		return Marker{}, false
	}
	return m, true
}

// WriteMarker writes exe's marker.
func WriteMarker(exe string, m Marker) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := exe + MarkerSuffix + ".tmp"
	if err := writeFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, exe+MarkerSuffix)
}

// ClearMarker removes exe's marker; a missing one is fine.
func ClearMarker(exe string) error {
	if err := os.Remove(exe + MarkerSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Rollback puts exe.prev back over exe (the binary Install replaced).
func Rollback(exe string) error {
	return os.Rename(exe+PrevSuffix, exe)
}
