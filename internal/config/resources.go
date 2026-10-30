package config

import "strings"

// Resources is the memory policy: how much must be free to boot an app, and
// which running apps may be stopped to make room. Every key is optional; the
// defaults are the linear headroom curve and lower-priority eviction.
type Resources struct {
	Headroom Headroom `yaml:"headroom"`
	// Reserve is kept free on top of every app's headroom (default 0).
	Reserve Size `yaml:"reserve"`
	// AvailableSource is where available memory is read (meminfo only, for now).
	AvailableSource string   `yaml:"available_source"`
	Eviction        Eviction `yaml:"eviction"`
}

// HeadroomCurve maps a priority to the headroom an app must leave free.
type HeadroomCurve string

const (
	// CurveLinear is max(min, max × (10 − p) / 9), rounded down to MiB.
	CurveLinear HeadroomCurve = "linear"
	// CurveTable takes explicit values for priorities 1–10 and interpolates
	// linearly between neighbours for a fractional priority.
	CurveTable HeadroomCurve = "table"
	// CurveFixed gives every priority the same headroom.
	CurveFixed HeadroomCurve = "fixed"
)

// Headroom configures the curve.
type Headroom struct {
	Curve HeadroomCurve `yaml:"curve"`
	Max   Size          `yaml:"max"`   // linear: headroom at priority 1
	Min   Size          `yaml:"min"`   // linear: the floor
	Table map[int]Size  `yaml:"table"` // table: priority 1–10 → headroom
	Value Size          `yaml:"value"` // fixed: the headroom
}

// EvictionPolicy says which running apps a booting app may stop.
type EvictionPolicy string

const (
	// EvictLower evicts only strictly lower priorities.
	EvictLower EvictionPolicy = "lower-priority"
	// EvictLowerOrEqual also evicts equal priorities.
	EvictLowerOrEqual EvictionPolicy = "lower-or-equal"
	// EvictNever never evicts.
	EvictNever EvictionPolicy = "never"
)

// EvictionOrder says which candidate goes first.
type EvictionOrder string

const (
	// OrderPriorityThenIdle stops the lowest priority first, longest idle among equals.
	OrderPriorityThenIdle EvictionOrder = "priority-then-idle"
	// OrderIdleThenPriority stops the longest idle first, lowest priority among equals.
	OrderIdleThenPriority EvictionOrder = "idle-then-priority"
)

// Eviction configures stopping running apps to make room for a boot.
type Eviction struct {
	// EnabledRaw is the file's value (nil = omitted); read Enabled.
	EnabledRaw *bool          `yaml:"enabled"`
	Enabled    bool           `yaml:"-"`
	Policy     EvictionPolicy `yaml:"policy"`
	Order      EvictionOrder  `yaml:"order"`
	// Protect lists app names that are never evicted.
	Protect []string `yaml:"protect"`
}

// Summary renders the effective policy in one phrase, e.g.
// "lower-priority, priority-then-idle, protects db".
func (e Eviction) Summary() string {
	if !e.Enabled || e.Policy == EvictNever {
		return "off"
	}
	s := string(e.Policy) + ", " + string(e.Order)
	if len(e.Protect) > 0 {
		s += ", protects " + strings.Join(e.Protect, ", ")
	}
	return s
}

// Resource defaults.
const (
	DefaultCurve           = CurveLinear
	DefaultHeadroomMax     = 5120 * MiB
	DefaultHeadroomMin     = 512 * MiB
	DefaultAvailableSource = "meminfo"
	DefaultEvictionPolicy  = EvictLower
	DefaultEvictionOrder   = OrderPriorityThenIdle
)

func (r *Resources) applyDefaults() {
	h := &r.Headroom
	if h.Curve == "" {
		h.Curve = DefaultCurve
	}
	if h.Max == 0 {
		h.Max = DefaultHeadroomMax
	}
	if h.Min == 0 {
		h.Min = DefaultHeadroomMin
	}
	if r.AvailableSource == "" {
		r.AvailableSource = DefaultAvailableSource
	}
	e := &r.Eviction
	e.Enabled = e.EnabledRaw == nil || *e.EnabledRaw
	if e.Policy == "" {
		e.Policy = DefaultEvictionPolicy
	}
	if e.Order == "" {
		e.Order = DefaultEvictionOrder
	}
}

func (r Resources) validate(names map[string]int, add addFunc) {
	h := r.Headroom
	switch h.Curve {
	case CurveLinear:
		if h.Min > h.Max {
			add("resources.headroom.min", "%s exceeds max %s", h.Min, h.Max)
		}
	case CurveTable:
		for p := 1; p <= 10; p++ {
			if _, ok := h.Table[p]; !ok {
				add("resources.headroom.table", "needs a value for every priority 1–10 (missing %d)", p)
				break
			}
		}
		for p := range h.Table {
			if p < 1 || p > 10 {
				add("resources.headroom.table", "priority %d is outside 1–10", p)
			}
		}
	case CurveFixed:
		if h.Value == 0 {
			add("resources.headroom.value", "is required for curve: fixed")
		}
	default:
		add("resources.headroom.curve", "%q must be linear, table or fixed", h.Curve)
	}
	if h.Max < 0 || h.Min < 0 || h.Value < 0 || r.Reserve < 0 {
		add("resources", "sizes must not be negative")
	}
	if r.AvailableSource != DefaultAvailableSource {
		add("resources.available_source", "%q is not supported (only meminfo)", r.AvailableSource)
	}
	e := r.Eviction
	switch e.Policy {
	case EvictLower, EvictLowerOrEqual, EvictNever:
	default:
		add("resources.eviction.policy", "%q must be lower-priority, lower-or-equal or never", e.Policy)
	}
	switch e.Order {
	case OrderPriorityThenIdle, OrderIdleThenPriority:
	default:
		add("resources.eviction.order", "%q must be priority-then-idle or idle-then-priority", e.Order)
	}
	for i, name := range e.Protect {
		if _, ok := names[name]; !ok {
			add("resources.eviction.protect", "[%d] %q is not a configured app", i, name)
		}
	}
}
