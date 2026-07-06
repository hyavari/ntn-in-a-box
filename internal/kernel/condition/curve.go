package condition

import (
	"math"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// evaluateCurve linearly interpolates points at offsetSec. points must
// be non-empty and sorted by ascending OffsetSec — profile.Validate
// enforces this for any profile that reaches an Evaluator, so this
// function doesn't re-validate ordering itself.
//
// offsetSec before the first point or at/after the last point is
// clamped to that boundary's value rather than extrapolated. A single
// point is a constant: its value applies at every offset.
func evaluateCurve(points []profile.Point, offsetSec float64) float64 {
	if len(points) == 0 {
		return math.NaN() // unreachable for a validated profile
	}
	if offsetSec <= points[0].OffsetSec {
		return points[0].Value
	}
	last := points[len(points)-1]
	if offsetSec >= last.OffsetSec {
		return last.Value
	}
	for i := 1; i < len(points); i++ {
		if offsetSec <= points[i].OffsetSec {
			prev := points[i-1]
			curr := points[i]
			frac := (offsetSec - prev.OffsetSec) / (curr.OffsetSec - prev.OffsetSec)
			return prev.Value + frac*(curr.Value-prev.Value)
		}
	}
	return last.Value // unreachable given the bounds checks above
}
