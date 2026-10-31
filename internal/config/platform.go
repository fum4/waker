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
	// ServicePATH is service.path's default: the system's binary
	// directories, never the installing shell's PATH (which would make the
	// unit depend on who ran install, from where).
	ServicePATH []string
}

// sysPATH is the standard system PATH (systemd's own default).
var sysPATH = []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"}

// PlatformFor returns goos's platform. macOS has no /proc and no systemd,
// so it reads vm_stat and learns no peaks, and Homebrew's bin joins the
// PATH; every other OS is treated as Linux.
func PlatformFor(goos string) Platform {
	if goos == "darwin" {
		return Platform{OS: goos, AvailableSources: []string{AvailableVMStat}, MemorySources: []MemorySource{MemoryNone},
			ServicePATH: append([]string{"/opt/homebrew/bin"}, sysPATH...)}
	}
	return Platform{OS: goos, AvailableSources: []string{AvailableMeminfo}, MemorySources: allMemorySources, ServicePATH: sysPATH}
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
