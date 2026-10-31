package state

import (
	"path/filepath"
	"time"
)

// UpdateFile records waker's release checks.
const UpdateFile = "update.json"

// Update is update.json: when waker last asked for the latest release and
// what it heard. Serve and the CLI share it, so the once-a-day cap holds
// across both.
type Update struct {
	CheckedAt time.Time `json:"checked_at"`
	// Latest is the release tag the last successful check saw ("v0.4.0").
	Latest string `json:"latest,omitempty"`
}

// LoadUpdate reads update.json; a missing file is the zero record.
func LoadUpdate(dir string) (Update, error) {
	var u Update
	return u, readJSON(filepath.Join(dir, UpdateFile), &u)
}

// SaveUpdate writes update.json atomically.
func SaveUpdate(dir string, u Update) error {
	return writeJSON(filepath.Join(dir, UpdateFile), u)
}
