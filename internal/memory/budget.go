// Package memory decides how much memory an app needs to boot:
//
//	need     = max(declared memory, learned peak), else 1024 MiB
//	headroom = the app's own headroom, else the curve at its priority
//	required = need + headroom + reserve
//
// and measures what is available and what an app actually used.
package memory

import (
	"fmt"
	"math"
	"sync"

	"github.com/fum4/waker/internal/config"
)

// HeadroomMiB is the curve's headroom at priority p, rounded down to MiB.
func HeadroomMiB(h config.Headroom, p float64) int64 {
	switch h.Curve {
	case config.CurveFixed:
		return h.Value.MiBs()
	case config.CurveTable:
		lo, hi := math.Floor(p), math.Ceil(p)
		a, b := float64(h.Table[int(lo)].MiBs()), float64(h.Table[int(hi)].MiBs())
		return int64(math.Floor(a + (b-a)*(p-lo)))
	default:
		linear := int64(math.Floor(float64(h.Max.MiBs()) * (10 - p) / 9))
		return max(h.Min.MiBs(), linear)
	}
}

// Breakdown is how an app's boot requirement is made up.
type Breakdown struct {
	DeclaredMiB  *int64 `json:"declared_mib"`
	LearnedMiB   *int64 `json:"learned_mib"`
	NeedMiB      int64  `json:"need_mib"`
	HeadroomMiB  int64  `json:"headroom_mib"`
	HeadroomFrom string `json:"headroom_from"`
	ReserveMiB   int64  `json:"reserve_mib"`
	RequiredMiB  int64  `json:"required_mib"`
}

// Budget holds the resource policy and every app's learned peak.
type Budget struct {
	res     config.Resources
	mu      sync.Mutex
	learned map[string]int64
}

// NewBudget starts from persisted learned peaks (MiB by app name).
func NewBudget(res config.Resources, learned map[string]int64) *Budget {
	b := &Budget{res: res, learned: map[string]int64{}}
	for k, v := range learned {
		if v > 0 {
			b.learned[k] = v
		}
	}
	return b
}

// Learn records an app's latest measured peak.
func (b *Budget) Learn(name string, mib int64) {
	if mib <= 0 {
		return
	}
	b.mu.Lock()
	b.learned[name] = mib
	b.mu.Unlock()
}

// Learned returns every learned peak.
func (b *Budget) Learned() map[string]int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]int64, len(b.learned))
	for k, v := range b.learned {
		out[k] = v
	}
	return out
}

// Breakdown computes a's requirement.
func (b *Budget) Breakdown(a config.App) Breakdown {
	var bd Breakdown
	if a.Memory > 0 {
		d := ceilMiB(a.Memory)
		bd.DeclaredMiB = &d
	}
	b.mu.Lock()
	if l, ok := b.learned[a.Name]; ok {
		bd.LearnedMiB = &l
	}
	b.mu.Unlock()
	bd.NeedMiB = config.DefaultNeed.MiBs()
	if bd.DeclaredMiB != nil || bd.LearnedMiB != nil {
		bd.NeedMiB = max(deref(bd.DeclaredMiB), deref(bd.LearnedMiB))
	}
	if a.Headroom > 0 {
		bd.HeadroomMiB, bd.HeadroomFrom = a.Headroom.MiBs(), "app"
	} else {
		bd.HeadroomMiB = HeadroomMiB(b.res.Headroom, a.Priority)
		bd.HeadroomFrom = fmt.Sprintf("%s p%g", b.res.Headroom.Curve, a.Priority)
	}
	bd.ReserveMiB = b.res.Reserve.MiBs()
	bd.RequiredMiB = bd.NeedMiB + bd.HeadroomMiB + bd.ReserveMiB
	return bd
}

func ceilMiB(s config.Size) int64 { return int64((s + config.MiB - 1) / config.MiB) }

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
