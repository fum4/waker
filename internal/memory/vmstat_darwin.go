//go:build darwin

package memory

import (
	"context"
	"os/exec"
)

// runVMStat runs the system vm_stat by absolute path: a launchd job's PATH
// is minimal, and a vm_stat earlier on PATH is not the one to trust.
func runVMStat(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "/usr/bin/vm_stat").Output()
}
