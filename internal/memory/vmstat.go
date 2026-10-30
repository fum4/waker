package memory

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// VMStatTimeout bounds one vm_stat run; it returns in milliseconds.
const VMStatTimeout = 5 * time.Second

// vmStatAvailable are the vm_stat lines summed as available memory:
//
//   - Pages free: truly free (vm_stat already subtracts speculative pages,
//     which are file read-ahead and so counted under File-backed).
//   - File-backed pages: the file cache, dropped without I/O when clean
//     (Linux MemAvailable likewise counts the page cache).
//   - Pages purgeable: volatile memory apps marked discardable.
//
// That is Activity Monitor's free + "Cached Files". Inactive pages are
// not added: on macOS they include anonymous memory that has to be
// compressed or swapped to reclaim, which MemAvailable leaves out too.
var vmStatAvailable = []string{"Pages free", "File-backed pages", "Pages purgeable"}

var vmStatPageSize = regexp.MustCompile(`page size of (\d+) bytes`)

// VMStat reads available memory from macOS vm_stat.
type VMStat struct{}

// AvailableMiB runs vm_stat and returns its available memory in MiB,
// rounded down.
func (VMStat) AvailableMiB() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), VMStatTimeout)
	defer cancel()
	out, err := runVMStat(ctx)
	if err != nil {
		return 0, fmt.Errorf("vm_stat: %w", err)
	}
	return ParseVMStat(out)
}

// ParseVMStat returns the available memory in vm_stat output, in MiB
// rounded down: (free + file-backed + purgeable pages) × page size.
func ParseVMStat(out []byte) (int64, error) {
	m := vmStatPageSize.FindSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("vm_stat: no page size in the header")
	}
	page, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil || page <= 0 {
		return 0, fmt.Errorf("vm_stat: bad page size %q", m[1])
	}
	pages := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(val), "."), 10, 64)
		if err == nil {
			pages[strings.Trim(key, `"`)] = n
		}
	}
	var total int64
	for _, k := range vmStatAvailable {
		n, ok := pages[k]
		if !ok {
			return 0, fmt.Errorf("vm_stat: no %q line", k)
		}
		total += n
	}
	return total * page >> 20, nil
}
