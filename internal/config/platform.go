package config

import (
	"fmt"
	"runtime"
	"strings"
)

// Available-memory sources (resources.available_source).
const (
	// AvailableMeminfo reads MemAvailable from Linux /proc/meminfo.
	AvailableMeminfo = "meminfo"
	// AvailableVMStat runs macOS vm_stat.
	AvailableVMStat = "vm_stat"
)

// Platform is what the host can measure. It is the one place waker's
// platform differences live: Load takes defaults from it and Validate
// rejects anything else. The first entry of each list is the default.
type Platform struct {
	OS               string
	AvailableSources []string
	MemorySources    []MemorySource
}

// PlatformFor returns goos's platform. macOS has no /proc and no systemd,
// so it reads vm_stat and learns no peaks; every other OS is treated as
// Linux.
func PlatformFor(goos string) Platform {
	if goos == "darwin" {
		return Platform{OS: goos, AvailableSources: []string{AvailableVMStat}, MemorySources: []MemorySource{MemoryNone}}
	}
	return Platform{OS: goos, AvailableSources: []string{AvailableMeminfo}, MemorySources: allMemorySources}
}

// allMemorySources is every memory_source value waker knows, on any OS.
var allMemorySources = []MemorySource{MemoryProcTree, MemorySystemd, MemoryNone}

// host is the platform waker runs on; tests swap it.
var host = PlatformFor(runtime.GOOS)

// DefaultAvailableSource is the host's resources.available_source default.
func DefaultAvailableSource() string { return host.AvailableSources[0] }

// DefaultMemorySource is the host's memory_source default.
func DefaultMemorySource() MemorySource { return host.MemorySources[0] }

// unsupported explains a known value the host cannot use, e.g.
// `"systemd" is not supported on darwin (supported: none)`.
func unsupported[T ~string](v T, supported []T) string {
	names := make([]string, len(supported))
	for i, s := range supported {
		names[i] = string(s)
	}
	return fmt.Sprintf("%q is not supported on %s (supported: %s)", v, host.OS, strings.Join(names, ", "))
}
