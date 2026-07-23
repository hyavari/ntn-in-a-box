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
	// InCoverage is true if the device has a link at this instant. For
	// ModeContinuous it is true except during a blockage (an unscheduled
	// outage; see profile.Blockage).
	InCoverage bool

	// ElapsedSec is the time elapsed, in seconds, since the current
	// window opened (periodic, while InCoverage) or since the current
	// cycle started (continuous). This is the value curve evaluation
	// uses. While out of coverage — a periodic gap, or a blockage in any
	// mode — it instead reports time elapsed since the link dropped;
	// informational only, since curves don't apply while out of coverage.
	ElapsedSec float64

	// UntilNextTransitionSec is the time remaining, in seconds, until
	// the next coverage transition. While InCoverage this is the time
	// until the scheduled window closes (periodic) or the current cycle
	// ends (continuous); an upcoming blockage is deliberately not
	// reflected here so it stays unforeseeable (blockages have no
	// lookahead). While out of coverage it is the time until coverage
	// resumes — the next window opens (periodic gap) or the blockage
	// clears.
	UntilNextTransitionSec float64

	// CyclePosSec is the position within the schedule period [0, PeriodSec),
	// always — including during a blockage. Progress bars use this so a
	// surprise drop does not reset or hijack the scheduled window/cycle
	// progress (ElapsedSec is blockage-relative while blocked).
	CyclePosSec float64

	// InBlockage is true when the link is down because of an unscheduled
	// blockage overlay (tunnel/terrain), not a scheduled periodic gap.
	// Only meaningful when InCoverage is false.
	InBlockage bool
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

	base := e.scheduledCoverage(cyclePos, period)

	// Overlay unscheduled blockages. A blockage only matters while the
	// schedule would otherwise have coverage; during a periodic gap the
	// link is already down. Deliberately, we do NOT shorten an
	// in-coverage UntilNextTransitionSec to reveal an upcoming
	// blockage: a blockage is a surprise drop (see profile.Blockage), so
	// consumers reading lookahead must not be able to foresee it.
	if base.InCoverage {
		if blk, ok := e.activeBlockage(cyclePos); ok {
			return CoverageState{
				InCoverage:             false,
				ElapsedSec:             cyclePos - blk.OffsetSec,
				UntilNextTransitionSec: (blk.OffsetSec + blk.DurationSec) - cyclePos,
				CyclePosSec:            cyclePos,
				InBlockage:             true,
			}
		}
	}
	return base
}

// scheduledCoverage computes coverage from the schedule alone (periodic
// window or continuous), before any blockage overlay.
func (e *Evaluator) scheduledCoverage(cyclePos, period float64) CoverageState {
	if e.profile.Schedule.Mode == profile.ModeContinuous {
		return CoverageState{
			InCoverage:             true,
			ElapsedSec:             cyclePos,
			UntilNextTransitionSec: period - cyclePos,
			CyclePosSec:            cyclePos,
		}
	}

	window := e.profile.Schedule.WindowSec
	if cyclePos < window {
		return CoverageState{
			InCoverage:             true,
			ElapsedSec:             cyclePos,
			UntilNextTransitionSec: window - cyclePos,
			CyclePosSec:            cyclePos,
		}
	}
	return CoverageState{
		InCoverage:             false,
		ElapsedSec:             cyclePos - window,
		UntilNextTransitionSec: period - cyclePos,
		CyclePosSec:            cyclePos,
	}
}

// activeBlockage returns the blockage covering cyclePos (on the half-open
// interval [OffsetSec, OffsetSec+DurationSec)), if any. Blockages are
// validated to be ascending and non-overlapping, so at most one matches.
func (e *Evaluator) activeBlockage(cyclePos float64) (profile.Blockage, bool) {
	for _, b := range e.profile.Blockages {
		if cyclePos >= b.OffsetSec && cyclePos < b.OffsetSec+b.DurationSec {
			return b, true
		}
	}
	return profile.Blockage{}, false
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
