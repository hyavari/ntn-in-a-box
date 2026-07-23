package profile

// Mode selects how a profile's coverage window repeats.
type Mode string

const (
	// ModePeriodic models a satellite pass that repeatedly opens and
	// closes: in coverage for Schedule.WindowSec out of every
	// Schedule.PeriodSec, out of coverage the rest of the time (e.g. a
	// LEO pass).
	ModePeriodic Mode = "periodic"

	// ModeContinuous models a link that is always scheduled to be in
	// coverage (e.g. a GEO steady link): curves loop over
	// Schedule.PeriodSec, with no scheduled out-of-coverage gap. Coverage
	// can still drop during a Blockage (an unscheduled outage), if the
	// profile defines any.
	ModeContinuous Mode = "continuous"
)

// Schedule describes when a profile's coverage window opens and closes,
// and how much advance notice is given before a transition.
type Schedule struct {
	Mode Mode `yaml:"mode"`

	// PeriodSec is the length of one full cycle in seconds. For
	// ModePeriodic this is the time between successive window opens.
	// For ModeContinuous this is the length of the curve loop.
	PeriodSec float64 `yaml:"period_sec"`

	// WindowSec is how long the coverage window stays open within one
	// period. Only meaningful for ModePeriodic; ignored for
	// ModeContinuous (which has no scheduled out-of-coverage gap, though
	// a Blockage can still drop the link).
	WindowSec float64 `yaml:"window_sec"`

	// LookaheadSec is how far in advance a coverage-open or
	// coverage-close transition is announced. For ModePeriodic this
	// covers scheduled window boundaries. Upcoming Blockages are never
	// foreshadowed (surprise drops), regardless of this value — set it
	// to 0 on continuous+blockage profiles so recovery notices are not
	// emitted either.
	LookaheadSec float64 `yaml:"lookahead_sec"`
}

// Point is one sample of a curve: a value at an offset (in seconds) from
// the start of the current window (ModePeriodic) or cycle
// (ModeContinuous). Curves are linearly interpolated between points.
type Point struct {
	OffsetSec float64 `yaml:"offset_sec"`
	Value     float64 `yaml:"value"`
}

// Curves holds the four service-relevant impairment curves the
// Condition Engine evaluates. Each is a piecewise-linear curve defined
// by an ascending list of Points.
type Curves struct {
	DelayMs       []Point `yaml:"delay_ms"`
	JitterMs      []Point `yaml:"jitter_ms"`
	LossPct       []Point `yaml:"loss_pct"`
	BandwidthKbps []Point `yaml:"bandwidth_kbps"`
}

// Blockage is a repeating, unscheduled coverage outage layered on top of
// an otherwise-in-coverage schedule: a link drop caused by the
// environment (tunnel, terrain, dense tree cover) rather than by orbital
// geometry. It models the reality that even a continuously-covered link
// (e.g. a GEO satellite always in view) still drops intermittently as a
// vehicle moves.
//
// A blockage is defined by an offset within the schedule cycle and a
// duration; it repeats every Schedule.PeriodSec. It is active on the
// half-open interval [OffsetSec, OffsetSec+DurationSec), matching the
// window convention in scheduler.go.
//
// Unlike a periodic window's close, a blockage carries no lookahead: it
// is intentionally a "surprise" drop (a driver cannot foresee a tunnel
// from orbital mechanics), so apps must detect it reactively via timeouts.
type Blockage struct {
	OffsetSec   float64 `yaml:"offset_sec"`
	DurationSec float64 `yaml:"duration_sec"`
}

// Profile is a named pass-shape profile: a coverage schedule plus the
// impairment curves that apply while in coverage.
//
// Blockages, when present, are unscheduled outages layered on the
// schedule (see Blockage). They are primarily intended for continuous
// profiles (modeling a moving vehicle on an always-in-view GEO link) but
// are permitted on any mode; on a periodic profile they only take effect
// while a window would otherwise be open.
type Profile struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Schedule    Schedule   `yaml:"schedule"`
	Curves      Curves     `yaml:"curves"`
	Blockages   []Blockage `yaml:"blockages"`
}
