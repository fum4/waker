package daemon

import (
	"context"
	"time"

	"github.com/fum4/waker/internal/app"
	"github.com/fum4/waker/internal/config"
	"github.com/fum4/waker/internal/control"
	"github.com/fum4/waker/internal/evict"
	"github.com/fum4/waker/internal/memory"
	"github.com/fum4/waker/internal/state"
)

// SampleInterval is how often a running proctree app's RSS is sampled.
const SampleInterval = 5 * time.Second

// procfs is where process-tree sampling reads from.
const procfs = "/proc"

// newGate builds the memory gate over the listening apps, starting from
// the learned peaks in peaks.json (a corrupt file is logged and ignored).
func (d *Daemon) newGate() *evict.Gate {
	learned := map[string]int64{}
	peaks, err := state.LoadPeaks(d.cfg.StateDir)
	if err != nil {
		d.log.Error("learned memory figures unreadable, starting without them", "err", err)
		peaks = map[string]state.Peak{}
	}
	d.peaks = peaks
	for name, p := range peaks {
		learned[name] = p.MiB
	}
	d.budget = memory.NewBudget(d.cfg.Resources, learned)
	d.sampler = memory.NewSampler()
	return &evict.Gate{
		Apps:   func() []*app.App { return d.listening },
		Budget: d.budget,
		Avail:  d.avail,
		Policy: d.cfg.Resources.Eviction,
		Log:    d.log,
	}
}

// sample records the RSS of every running proctree app until ctx ends.
func (d *Daemon) sample(ctx context.Context) {
	t := time.NewTicker(SampleInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, a := range d.listening {
				d.sampleOne(a)
			}
		}
	}
}

func (d *Daemon) sampleOne(a *app.App) {
	if a.Config().MemorySource != config.MemoryProcTree {
		return
	}
	if pid := d.procs[a.Name()].PID(); pid > 0 {
		d.sampler.Observe(a.Name(), memory.ProcTreeRSSMiB(procfs, pid))
	}
}

// recordPeak runs before every stop: it reads the app's peak (the unit's
// MemoryPeak, or the sampled process tree) while it still exists, learns it
// and persists peaks.json.
func (d *Daemon) recordPeak(a *app.App) {
	ac := a.Config()
	var peak int64
	switch ac.MemorySource {
	case config.MemoryProcTree:
		d.sampleOne(a)
		peak = d.sampler.Take(ac.Name)
	case config.MemorySystemd:
		peak, _ = memory.SystemdPeakMiB(context.Background(), ac.SystemdUnit)
	}
	if peak <= 0 {
		return
	}
	d.budget.Learn(ac.Name, peak)
	d.log.Info("memory peak learned", "app", ac.Name, "peak_mib", peak)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.peaks[ac.Name] = state.Peak{MiB: peak, At: time.Now().UTC().Truncate(time.Second)}
	if err := state.SavePeaks(d.cfg.StateDir, d.peaks); err != nil {
		d.log.Error("saving learned memory figures failed", "err", err)
	}
}

// withMemory adds the app's memory breakdown to its snapshot.
func (d *Daemon) withMemory(a *app.App, s app.Snapshot) app.Snapshot {
	bd := d.budget.Breakdown(a.Config())
	s.Memory = &bd
	return s
}

// policy is the effective resource policy for status.
func (d *Daemon) policy() control.Policy {
	r := d.cfg.Resources
	p := control.Policy{Headroom: headroomText(r.Headroom), ReserveMiB: r.Reserve.MiBs(), Eviction: r.Eviction.Summary()}
	if avail, err := d.avail.AvailableMiB(); err == nil {
		p.AvailableMiB = &avail
	}
	return p
}

func headroomText(h config.Headroom) string {
	switch h.Curve {
	case config.CurveFixed:
		return "fixed " + h.Value.String()
	case config.CurveTable:
		return "table"
	default:
		return "linear max " + h.Max.String() + ", min " + h.Min.String()
	}
}
