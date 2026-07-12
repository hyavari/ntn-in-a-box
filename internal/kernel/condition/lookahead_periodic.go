package condition

import (
	"math"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// Lookahead returns absolute open/close prediction for the current or
// next coverage window. Continuous mode omits open/close/duration.
func (e *Evaluator) Lookahead(now time.Time) LookaheadState {
	cov := e.Coverage(now)
	st := LookaheadState{
		InCoverage:             cov.InCoverage,
		UntilNextTransitionSec: cov.UntilNextTransitionSec,
		ConfiguredLookaheadSec: e.profile.Schedule.LookaheadSec,
	}

	if e.profile.Schedule.Mode == profile.ModeContinuous {
		return st
	}

	window := e.profile.Schedule.WindowSec

	if cov.InCoverage {
		st.NextWindowDurationSec = Float64Ptr(window)
		open := now.Add(-time.Duration(cov.ElapsedSec * float64(time.Second)))
		closeAt := now.Add(time.Duration(cov.UntilNextTransitionSec * float64(time.Second)))
		st.NextOpenAt = TimePtr(open)
		st.NextCloseAt = TimePtr(closeAt)
		return st
	}

	if math.IsInf(cov.UntilNextTransitionSec, 0) {
		return st
	}

	st.NextWindowDurationSec = Float64Ptr(window)
	open := now.Add(time.Duration(cov.UntilNextTransitionSec * float64(time.Second)))
	closeAt := open.Add(time.Duration(window * float64(time.Second)))
	st.NextOpenAt = TimePtr(open)
	st.NextCloseAt = TimePtr(closeAt)
	return st
}
