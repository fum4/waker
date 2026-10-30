//go:build !darwin

package memory

import (
	"context"
	"errors"
)

// runVMStat refuses off macOS; config validation already rejects vm_stat
// there, so this is never reached by a checked config.
func runVMStat(context.Context) ([]byte, error) {
	return nil, errors.New("vm_stat is macOS only")
}
