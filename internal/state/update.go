package state

import (
	"path/filepath"
	"time"
)

// UpdateFile records waker's release checks and automatic updates.
const UpdateFile = "update.json"

// Outcomes of an automatic update (Attempt.Outcome).
const (
	// UpdateInstalled: the new binary is in place and serve re-executed
	// into it; it becomes UpdateApplied once that binary is serving.
	UpdateInstalled = "installed"
	// UpdateApplied: the new binary started and is serving.
	UpdateApplied = "applied"
	// UpdateRolledBack: the new binary didn't come up (or rejected the
	// config); the old one is back and the tag is blocked.
	UpdateRolledBack = "rolled-back"
	// UpdateFailed: the download, verification or install failed; nothing
	// changed.
	UpdateFailed = "failed"
	// UpdateSkipped: serve couldn't write its binary (Detail has the
	// command to run).
	UpdateSkipped = "skipped"
)

// Update is update.json. Serve and the CLI share it, so the once-a-day
// cap holds across both.
type Update struct {
	CheckedAt time.Time `json:"checked_at"`
	// Latest is the release tag the last successful check saw ("v0.4.0").
	Latest string `json:"latest,omitempty"`
	// AutoAt is when serve last tried update.auto: once a day at most.
	AutoAt time.Time `json:"auto_at,omitzero"`
	// Last is the last automatic update and how it ended.
	Last *Attempt `json:"last,omitempty"`
	// Blocked are the tags a rollback took back; auto never retries them.
	Blocked []string `json:"blocked,omitempty"`
}

// Attempt is one automatic update.
type Attempt struct {
	At      time.Time `json:"at"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Outcome string    `json:"outcome"`
	Detail  string    `json:"detail,omitempty"`
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
