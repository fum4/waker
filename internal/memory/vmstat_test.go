package memory

import (
	"strings"
	"testing"

	"github.com/fum4/waker/internal/config"
)

// Apple silicon (16 KiB pages), the layout of macOS 14's vm_stat.
const vmStatAppleSilicon = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                3720.
Pages active:                            301297.
Pages inactive:                          297562.
Pages speculative:                         2468.
Pages throttled:                              0.
Pages wired down:                        135710.
Pages purgeable:                           9816.
"Translation faults":                1386305718.
Pages copy-on-write:                   45628214.
Pages zero filled:                    600291375.
Pages reactivated:                      9276423.
Pages purged:                           2117420.
File-backed pages:                       231004.
Anonymous pages:                         370323.
Pages stored in compressor:              768931.
Pages occupied by compressor:            232167.
Decompressions:                        17766853.
Compressions:                          24497005.
Pageins:                               29718046.
Pageouts:                                348066.
Swapins:                                3082512.
Swapouts:                               3437806.
`

// Intel (4 KiB pages), the layout of macOS 12's vm_stat.
const vmStatIntel = `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                              104738.
Pages active:                           1064163.
Pages inactive:                         1019720.
Pages speculative:                        28157.
Pages throttled:                              0.
Pages wired down:                        711376.
Pages purgeable:                          45232.
"Translation faults":                 489517227.
Pages copy-on-write:                   21946702.
Pages zero filled:                    245361908.
Pages reactivated:                      1270145.
Pages purged:                            613477.
File-backed pages:                       812548.
Anonymous pages:                        1299492.
Pages stored in compressor:              688342.
Pages occupied by compressor:            266795.
Decompressions:                          917360.
Compressions:                           1773004.
Pageins:                                7429134.
Pageouts:                                 84211.
Swapins:                                      0.
Swapouts:                                     0.
`

func TestParseVMStat(t *testing.T) {
	without := func(line string) string {
		var keep []string
		for _, l := range strings.Split(vmStatIntel, "\n") {
			if !strings.HasPrefix(l, line) {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	}
	tests := []struct {
		name string
		out  string
		want int64  // MiB
		err  string // a substring of the error, if any
	}{
		// (3720 + 231004 + 9816) × 16384 B = 3823 MiB rounded down.
		{"apple silicon", vmStatAppleSilicon, (3720 + 231004 + 9816) * 16384 >> 20, ""},
		// (104738 + 812548 + 45232) × 4096 B = 3759 MiB rounded down.
		{"intel", vmStatIntel, (104738 + 812548 + 45232) * 4096 >> 20, ""},
		{"no header", strings.SplitN(vmStatIntel, "\n", 2)[1], 0, "no page size"},
		{"zero page size", strings.Replace(vmStatIntel, "4096", "0", 1), 0, "bad page size"},
		{"no free", without("Pages free:"), 0, `"Pages free"`},
		{"no file-backed", without("File-backed pages:"), 0, `"File-backed pages"`},
		{"no purgeable", without("Pages purgeable:"), 0, `"Pages purgeable"`},
		{"empty", "", 0, "no page size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVMStat([]byte(tt.out))
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("= %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestForSource(t *testing.T) {
	if a, err := ForSource(config.AvailableMeminfo); err != nil || a != NewMeminfo() {
		t.Fatalf("meminfo = %v, %v", a, err)
	}
	if a, err := ForSource(config.AvailableVMStat); err != nil || a != (VMStat{}) {
		t.Fatalf("vm_stat = %v, %v", a, err)
	}
	if _, err := ForSource("cgroup"); err == nil || !strings.Contains(err.Error(), "resources.available_source") {
		t.Fatalf("unknown source: %v", err)
	}
}
