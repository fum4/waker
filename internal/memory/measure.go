package memory

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fum4/waker/internal/config"
)

// Available reports memory free for a new app.
type Available interface {
	AvailableMiB() (int64, error)
}

// ForSource returns the reader for a resources.available_source value.
func ForSource(src string) (Available, error) {
	switch src {
	case config.AvailableMeminfo:
		return NewMeminfo(), nil
	case config.AvailableVMStat:
		return VMStat{}, nil
	}
	return nil, fmt.Errorf("resources.available_source: %q has no reader", src)
}

// Meminfo reads MemAvailable from a Linux meminfo file.
type Meminfo struct{ Path string }

// NewMeminfo reads /proc/meminfo.
func NewMeminfo() Meminfo { return Meminfo{Path: "/proc/meminfo"} }

// AvailableMiB returns MemAvailable in MiB, rounded down.
func (m Meminfo) AvailableMiB() (int64, error) {
	f, err := os.Open(m.Path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("%s: %w", m.Path, err)
			}
			return kb / 1024, nil
		}
	}
	return 0, fmt.Errorf("%s: no MemAvailable line", m.Path)
}

// ProcTreeRSSMiB sums the resident memory of root and all its descendants,
// read from procfs, rounded up to MiB. A vanished root reads as 0. Linux
// only: config rejects memory_source: proctree where there is no /proc.
func ProcTreeRSSMiB(procfs string, root int) int64 {
	children := map[int][]int{}
	entries, _ := os.ReadDir(procfs)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if ppid, ok := parentPID(filepath.Join(procfs, e.Name(), "stat")); ok {
			children[ppid] = append(children[ppid], pid)
		}
	}
	page := int64(os.Getpagesize())
	var total int64
	for queue := []int{root}; len(queue) > 0; queue = queue[1:] {
		pid := queue[0]
		statm, err := os.ReadFile(filepath.Join(procfs, strconv.Itoa(pid), "statm"))
		if err == nil {
			if f := strings.Fields(string(statm)); len(f) > 1 {
				pages, _ := strconv.ParseInt(f[1], 10, 64)
				total += pages * page
			}
		}
		queue = append(queue, children[pid]...)
	}
	return (total + 1<<20 - 1) >> 20
}

// parentPID reads field 4 of /proc/<pid>/stat; comm (field 2) may contain
// spaces and parentheses, so parse after its last ')'.
func parentPID(statPath string) (int, bool) {
	b, err := os.ReadFile(statPath)
	if err != nil {
		return 0, false
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(f[1])
	return ppid, err == nil
}

// SystemdPeakMiB reads a unit's MemoryPeak (the user manager unless the
// waker runs as root). ok is false when the unit has no figure.
func SystemdPeakMiB(ctx context.Context, unit string) (int64, bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := []string{"show", "-p", "MemoryPeak", "--value", unit}
	if os.Geteuid() != 0 {
		args = append([]string{"--user"}, args...)
	}
	out, err := exec.CommandContext(ctx, "systemctl", args...).Output()
	if err != nil {
		return 0, false
	}
	return parseBytesMiB(string(out))
}

func parseBytesMiB(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, false // "[not set]", "infinity", empty
	}
	return (n + 1<<20 - 1) >> 20, true
}

// Sampler keeps the highest sample per app since the last Take.
type Sampler struct {
	mu   sync.Mutex
	peak map[string]int64
}

// NewSampler returns an empty sampler.
func NewSampler() *Sampler { return &Sampler{peak: map[string]int64{}} }

// Observe records a sample.
func (s *Sampler) Observe(name string, mib int64) {
	s.mu.Lock()
	s.peak[name] = max(s.peak[name], mib)
	s.mu.Unlock()
}

// Take returns and clears an app's peak.
func (s *Sampler) Take(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.peak[name]
	delete(s.peak, name)
	return p
}
