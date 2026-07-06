package profile

// Mode selects how a profile's coverage window repeats.
type Mode string

const (
	// ModePeriodic models a satellite pass that repeatedly opens and
	// closes: in coverage for Schedule.WindowSec out of every
	// Schedule.PeriodSec, out of coverage the rest of the time (e.g. a
	// LEO pass).
	ModePeriodic Mode = "periodic"

	// ModeContinuous models a link that never loses coverage (e.g. a
	// GEO steady link): curves loop over Schedule.PeriodSec, but there
	// is no out-of-coverage gap.
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
	// ModeContinuous (which is always "in coverage").
	WindowSec float64 `yaml:"window_sec"`

	// LookaheadSec is how far in advance a coverage-open or
	// coverage-close transition is announced. Only meaningful for
	// ModePeriodic.
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

// Profile is a named pass-shape profile: a coverage schedule plus the
// impairment curves that apply while in coverage.
type Profile struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Schedule    Schedule `yaml:"schedule"`
	Curves      Curves   `yaml:"curves"`
}
