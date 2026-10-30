package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/fum4/waker/internal/config"
)

func linear() config.Headroom {
	return config.Headroom{Curve: config.CurveLinear, Max: 5120 * config.MiB, Min: 512 * config.MiB}
}

func TestLinearHeadroomTable(t *testing.T) {
	want := []int64{5120, 4551, 3982, 3413, 2844, 2275, 1706, 1137, 568, 512}
	for i, w := range want {
		if got := HeadroomMiB(linear(), float64(i+1)); got != w {
			t.Errorf("p%d = %d, want %d", i+1, got, w)
		}
	}
}

func TestFloatPriorities(t *testing.T) {
	tests := []struct {
		p    float64
		want int64
	}{{9.5, 512}, {7.5, 1422}, {1.0, 5120}, {5.25, 2702}}
	for _, tt := range tests {
		if got := HeadroomMiB(linear(), tt.p); got != tt.want {
			t.Errorf("p%g = %d, want %d", tt.p, got, tt.want)
		}
	}
}

func TestTableCurveInterpolates(t *testing.T) {
	h := config.Headroom{Curve: config.CurveTable, Table: map[int]config.Size{}}
	for p := 1; p <= 10; p++ {
		h.Table[p] = config.Size(1100-100*p) * config.MiB // 1000, 900, … 100
	}
	tests := []struct {
		p    float64
		want int64
	}{{1, 1000}, {10, 100}, {7, 400}, {7.5, 350}, {7.25, 375}, {2.9, 810}}
	for _, tt := range tests {
		if got := HeadroomMiB(h, tt.p); got != tt.want {
			t.Errorf("table p%g = %d, want %d", tt.p, got, tt.want)
		}
	}
}

func TestFixedCurve(t *testing.T) {
	h := config.Headroom{Curve: config.CurveFixed, Value: 2 * config.GiB}
	for _, p := range []float64{1, 5.5, 10} {
		if got := HeadroomMiB(h, p); got != 2048 {
			t.Errorf("fixed p%g = %d", p, got)
		}
	}
}

func app(memory config.Size, priority float64) config.App {
	return config.App{Name: "shop", Memory: memory, Priority: priority}
}

func TestNeedIsTheHigherFigureElseTheDefault(t *testing.T) {
	res := config.Resources{Headroom: linear()}
	tests := []struct {
		declared config.Size
		learned  int64
		want     int64
	}{{0, 0, 1024}, {0, 600, 600}, {800 * config.MiB, 0, 800}, {800 * config.MiB, 600, 800}, {800 * config.MiB, 900, 900}, {config.MiB + 1, 0, 2}}
	for _, tt := range tests {
		b := NewBudget(res, map[string]int64{"shop": tt.learned})
		if got := b.Breakdown(app(tt.declared, 5)).NeedMiB; got != tt.want {
			t.Errorf("declared %s learned %d: need %d, want %d", tt.declared, tt.learned, got, tt.want)
		}
	}
}

func TestRequiredIsNeedPlusHeadroomPlusReserve(t *testing.T) {
	b := NewBudget(config.Resources{Headroom: linear()}, nil)
	if bd := b.Breakdown(app(0, 5)); bd.RequiredMiB != 1024+2844 || bd.HeadroomFrom != "linear p5" {
		t.Fatalf("p5 = %+v", bd)
	}
	b.Learn("shop", 900)
	if bd := b.Breakdown(app(0, 10)); bd.RequiredMiB != 900+512 || *bd.LearnedMiB != 900 {
		t.Fatalf("p10 learned 900 = %+v", bd)
	}
	b = NewBudget(config.Resources{Headroom: linear(), Reserve: 256 * config.MiB}, nil)
	a := app(0, 5)
	a.Headroom = 100 * config.MiB
	if bd := b.Breakdown(a); bd.RequiredMiB != 1024+100+256 || bd.HeadroomFrom != "app" || bd.ReserveMiB != 256 {
		t.Fatalf("app headroom + reserve = %+v", bd)
	}
	b.Learn("shop", 0)
	if bd := b.Breakdown(a); bd.LearnedMiB != nil {
		t.Fatal("a zero peak was learned")
	}
}

func TestMeminfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	write(t, path, "MemTotal:       16000000 kB\nMemAvailable:    8390656 kB\n")
	if got, err := (Meminfo{Path: path}).AvailableMiB(); err != nil || got != 8194 {
		t.Fatalf("= %d, %v", got, err)
	}
	write(t, path, "MemTotal: 1 kB\n")
	if _, err := (Meminfo{Path: path}).AvailableMiB(); err == nil {
		t.Fatal("missing line accepted")
	}
	if runtime.GOOS != "linux" {
		return
	}
	if got, err := NewMeminfo().AvailableMiB(); err != nil || got <= 0 {
		t.Fatalf("the real /proc/meminfo = %d, %v", got, err)
	}
}

func TestProcTreeSumsDescendantsOnly(t *testing.T) {
	procfs := t.TempDir()
	page := int64(os.Getpagesize())
	proc := func(pid, ppid string, pages int64) {
		dir := filepath.Join(procfs, pid)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, "stat"), pid+" (my (odd) app) S "+ppid+" 1 1 0")
		write(t, filepath.Join(dir, "statm"), "999 "+strconv.FormatInt((pages<<20)/page, 10)+" 0")
	}
	proc("10", "1", 100) // root
	proc("11", "10", 50) // child
	proc("12", "11", 25) // grandchild
	proc("20", "1", 999) // unrelated
	if got := ProcTreeRSSMiB(procfs, 10); got != 175 {
		t.Fatalf("tree = %d MiB, want 175", got)
	}
	if got := ProcTreeRSSMiB(procfs, 99); got != 0 {
		t.Fatalf("vanished root = %d", got)
	}
}

func TestParseSystemdPeak(t *testing.T) {
	if got, ok := parseBytesMiB("667942912\n"); !ok || got != 637 {
		t.Fatalf("= %d, %v", got, ok)
	}
	for _, s := range []string{"[not set]", "infinity", "", "0"} {
		if _, ok := parseBytesMiB(s); ok {
			t.Errorf("%q parsed", s)
		}
	}
}

func TestSampler(t *testing.T) {
	s := NewSampler()
	s.Observe("shop", 300)
	s.Observe("shop", 700)
	s.Observe("shop", 500)
	if got := s.Take("shop"); got != 700 {
		t.Fatalf("peak = %d", got)
	}
	if got := s.Take("shop"); got != 0 {
		t.Fatalf("after take = %d", got)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
