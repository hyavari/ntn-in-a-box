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
		open := now.Add(-secDur(cov.ElapsedSec))
		closeAt := now.Add(secDur(cov.UntilNextTransitionSec))
		st.NextOpenAt = TimePtr(open)
		st.NextCloseAt = TimePtr(closeAt)
		return st
	}

	if math.IsInf(cov.UntilNextTransitionSec, 0) {
		return st
	}

	st.NextWindowDurationSec = Float64Ptr(window)

	// Out of coverage. This can be a scheduled inter-window gap or a
	// mid-window blockage. They predict different close times, so
	// distinguish them by asking what the schedule alone (ignoring
	// blockages) says at this instant.
	elapsed := now.Sub(e.epoch).Seconds()
	period := e.profile.Schedule.PeriodSec
	cyclePos := positiveMod(elapsed, period)
	scheduled := e.scheduledCoverage(cyclePos, period)

	if scheduled.InCoverage {
		// Mid-window blockage: the schedule still has the window open, so
		// its close time is unaffected by the (transient) blockage.
		// Coverage resumes when the blockage clears
		// (cov.UntilNextTransitionSec); the window closes at its
		// scheduled time (scheduled.UntilNextTransitionSec from now).
		schedClose := now.Add(secDur(scheduled.UntilNextTransitionSec))
		if cov.UntilNextTransitionSec < scheduled.UntilNextTransitionSec {
			// Blockage clears before the window closes: coverage returns
			// for the remainder of this window.
			st.NextOpenAt = TimePtr(now.Add(secDur(cov.UntilNextTransitionSec)))
			st.NextCloseAt = TimePtr(schedClose)
		} else {
			// Blockage lasts past this window's scheduled close: no more
			// coverage this window — predict the next scheduled window.
			nextOpen := schedClose.Add(secDur(period - window))
			st.NextOpenAt = TimePtr(nextOpen)
			st.NextCloseAt = TimePtr(nextOpen.Add(secDur(window)))
		}
		return st
	}

	// Scheduled inter-window gap: the next window opens after the gap and
	// runs for the full window duration.
	open := now.Add(secDur(cov.UntilNextTransitionSec))
	st.NextOpenAt = TimePtr(open)
	st.NextCloseAt = TimePtr(open.Add(secDur(window)))
	return st
}

// secDur converts a float seconds value into a time.Duration.
func secDur(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
