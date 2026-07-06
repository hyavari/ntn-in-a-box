package condition

import (
	"math"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// CoverageState describes whether a device is in coverage at a given
// instant, and how that instant relates to the current window
// (periodic mode) or cycle (continuous mode).
type CoverageState struct {
	// InCoverage is true if the device has a link at this instant.
	// Always true for profile.ModeContinuous.
	InCoverage bool

	// ElapsedSec is the time elapsed, in seconds, since the current
	// window opened (periodic, while InCoverage) or since the current
	// cycle started (continuous). This is the value curve evaluation
	// uses. While out of coverage (periodic only), it instead reports
	// time elapsed since the window closed — informational only, since
	// curves don't apply outside a window.
	ElapsedSec float64

	// UntilNextTransitionSec is the time remaining, in seconds, until
	// the next coverage transition: window close if InCoverage, window
	// open if not. For ModeContinuous there is no real transition
	// (coverage never drops); this instead reports time remaining in
	// the current curve cycle.
	UntilNextTransitionSec float64
}

// Coverage computes the CoverageState at instant now.
//
// A window is treated as open on the half-open interval
// [0, WindowSec) within each period: at cyclePos == WindowSec exactly,
// the device is considered already out of coverage.
//
// Note: elapsed is derived from now.Sub(epoch).Seconds() as a float64.
// For an Evaluator kept alive across very long spans (many months),
// float64 precision loss at large elapsed values could in principle
// shift an exact-boundary check by a tiny amount. Not a concern for
// this project's expected usage (dev/test sessions, not long-lived
// daemons evaluating a single Evaluator for months), so not guarded
// against here.
func (e *Evaluator) Coverage(now time.Time) CoverageState {
	elapsed := now.Sub(e.epoch).Seconds()
	period := e.profile.Schedule.PeriodSec
	cyclePos := positiveMod(elapsed, period)

	if e.profile.Schedule.Mode == profile.ModeContinuous {
		return CoverageState{
			InCoverage:             true,
			ElapsedSec:             cyclePos,
			UntilNextTransitionSec: period - cyclePos,
		}
	}

	window := e.profile.Schedule.WindowSec
	if cyclePos < window {
		return CoverageState{
			InCoverage:             true,
			ElapsedSec:             cyclePos,
			UntilNextTransitionSec: window - cyclePos,
		}
	}
	return CoverageState{
		InCoverage:             false,
		ElapsedSec:             cyclePos - window,
		UntilNextTransitionSec: period - cyclePos,
	}
}

// positiveMod returns a mod m in [0, m), matching mathematical modulo
// rather than Go's %, which can return a negative result when a is
// negative (e.g. when now is before the evaluator's epoch).
func positiveMod(a, m float64) float64 {
	r := math.Mod(a, m)
	if r < 0 {
		r += m
	}
	return r
}
