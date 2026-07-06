package eventbus

import (
	"math"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

// LinkStateThrottle decides whether a candidate LinkState update is
// actually published to subscribers or suppressed as redundant.
//
// An update is published if either:
//   - it changed by more than DeltaThreshold (a fraction, e.g. 0.05
//     for 5%) in any of the four impairment fields since the last
//     published state, or
//   - at least Interval has elapsed since the last publish — a
//     heartbeat, so subscribers still hear about a static link
//     periodically rather than going silent forever.
//
// The first-ever update is always published (there is no prior state
// to compare against).
type LinkStateThrottle struct {
	Interval       time.Duration
	DeltaThreshold float64
}

// DefaultLinkStateThrottle is the Step 0 default. See the design doc's
// open questions: 250ms / 5% delta was the direction proposed there;
// exact numbers were left to be tuned once observable, which is what
// this default represents — a starting point, not a final answer.
var DefaultLinkStateThrottle = LinkStateThrottle{
	Interval:       250 * time.Millisecond,
	DeltaThreshold: 0.05,
}

// linkStateDelta returns the largest relative change across all four
// impairment fields between a and b, as a fraction (0.05 == 5%).
//
// A change away from exactly zero is always treated as significant
// (returns +Inf) since a relative delta is undefined at zero.
func linkStateDelta(a, b condition.LinkState) float64 {
	return max(
		relDelta(a.DelayMs, b.DelayMs),
		relDelta(a.JitterMs, b.JitterMs),
		relDelta(a.LossPct, b.LossPct),
		relDelta(a.BandwidthKbps, b.BandwidthKbps),
	)
}

func relDelta(oldVal, newVal float64) float64 {
	if oldVal == newVal {
		return 0
	}
	// A change to or from exactly zero is always significant — a
	// relative delta is undefined at zero, and treating only the
	// 0->x direction as infinite (while x->0 fell through to the
	// division below, giving exactly 1.0) was an inconsistent
	// asymmetry rather than a deliberate choice.
	if oldVal == 0 || newVal == 0 {
		return math.Inf(1)
	}
	return math.Abs(newVal-oldVal) / math.Abs(oldVal)
}
