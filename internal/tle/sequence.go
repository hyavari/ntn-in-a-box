package tle

import (
	"fmt"
	"math"
	"sync"
	"time"

	satellite "github.com/joshuaferrara/go-satellite"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

// SequenceConfig controls the SequenceEvaluator behavior.
type SequenceConfig struct {
	Speed        float64   // Gap acceleration factor (default: 1.0)
	StartAt      time.Time // Simulation epoch (what sim-time "now" maps to)
	LookaheadSec float64   // Lookahead for coverage transitions (default: 30)
	Observer     Observer  // Ground observer (for Position())
	Sat          Satellite // TLE satellite (for Position() SGP4 propagation)
}

// passEntry holds a pass along with its pre-built evaluator.
type passEntry struct {
	pass Pass
	eval *condition.Evaluator
}

// SequenceEvaluator manages a sequence of satellite passes, delegating
// to per-pass condition.Evaluators during coverage and reporting
// out-of-coverage during gaps. Satisfies condition.Eval.
//
// Time model: The driver calls Advance(wallNow) once per tick to move
// simulation time forward (with variable-rate acceleration). All other
// consumers call Evaluate(now) which is a pure read of the current
// simulation state — it does NOT advance the timeline.
type SequenceEvaluator struct {
	entries      []passEntry
	speed        float64
	lookaheadSec float64
	startAt      time.Time // The simulation time that wallStart maps to

	// For Position() — satellite propagation and observer look angles.
	observer Observer
	sat      Satellite
	sgp4Sat  satellite.Satellite // Cached SGP4 initialization
	obsLL    satellite.LatLong   // Observer in radians (cached)

	mu      sync.RWMutex
	simTime time.Time // Current simulation time (advanced by Advance only)
	started bool
	// Wall-clock tracking for variable-rate advancement.
	lastWall time.Time
}

// NewSequenceEvaluator builds a SequenceEvaluator from predicted passes
// and a link model. It pre-generates a profile and condition.Evaluator
// for each pass.
func NewSequenceEvaluator(passes []Pass, model LinkModel, cfg SequenceConfig) (*SequenceEvaluator, error) {
	if len(passes) == 0 {
		return nil, fmt.Errorf("tle: no passes provided to SequenceEvaluator")
	}

	speed := cfg.Speed
	if speed <= 0 {
		speed = 1.0
	}
	lookahead := cfg.LookaheadSec
	if lookahead == 0 {
		lookahead = 30
	}

	entries := make([]passEntry, len(passes))
	for i, pass := range passes {
		// Compute gap to next pass for PeriodSec.
		var gapSec float64
		if i+1 < len(passes) {
			gapSec = passes[i+1].Rise.Sub(pass.Set).Seconds()
		} else {
			// Last pass: use a synthetic gap large enough for lookahead.
			// The SequenceEvaluator handles post-sequence coverage directly,
			// so this value only matters for profile validation.
			gapSec = lookahead * 2
		}

		p, err := GenerateProfile(pass, model, GenerateOpts{
			LookaheadSec: lookahead,
			GapSec:       gapSec,
			Index:        i,
			SatName:      pass.Satellite,
		})
		if err != nil {
			return nil, fmt.Errorf("tle: generating profile for pass %d: %w", i+1, err)
		}

		eval, err := condition.NewEvaluator(*p, pass.Rise)
		if err != nil {
			return nil, fmt.Errorf("tle: creating evaluator for pass %d: %w", i+1, err)
		}

		entries[i] = passEntry{pass: pass, eval: eval}
	}

	startAt := cfg.StartAt
	if startAt.IsZero() {
		startAt = passes[0].Rise.Add(-time.Duration(lookahead) * time.Second)
	}

	return &SequenceEvaluator{
		entries:      entries,
		speed:        speed,
		lookaheadSec: lookahead,
		startAt:      startAt,
		observer:     cfg.Observer,
		sat:          cfg.Sat,
		sgp4Sat:      initSGP4(cfg.Sat),
		obsLL:        observerLatLong(cfg.Observer),
		simTime:      startAt,
		started:      false,
	}, nil
}

// Advance moves the simulation clock forward based on wall-clock
// progression. Only the driver loop should call this, once per tick.
// It applies time acceleration (Nx) during gaps and real-time (1x)
// during passes.
func (se *SequenceEvaluator) Advance(wallNow time.Time) {
	se.mu.Lock()
	defer se.mu.Unlock()

	if !se.started {
		se.started = true
		se.lastWall = wallNow
		return
	}

	wallDelta := wallNow.Sub(se.lastWall)
	if wallDelta <= 0 {
		se.lastWall = wallNow
		return
	}

	// Determine current speed factor based on whether simTime is in a pass.
	factor := se.speed // Default: gap speed
	for _, entry := range se.entries {
		if !se.simTime.Before(entry.pass.Rise) && se.simTime.Before(entry.pass.Set) {
			factor = 1.0 // In pass: real-time
			break
		}
	}

	// Advance simulation time.
	simDelta := time.Duration(float64(wallDelta) * factor)
	se.simTime = se.simTime.Add(simDelta)
	se.lastWall = wallNow

	// Snap logic: if we were in a gap and jumped past a pass rise,
	// snap to the rise so we don't skip the beginning. Also handles
	// the case where speed jumps over an entire short pass — we snap
	// to the rise of the first pass we cross into or over.
	// We snap to (rise - lookahead) if we also crossed that point,
	// so the driver has a chance to fire WindowOpening.
	if factor > 1.0 {
		oldSimTime := se.simTime.Add(-simDelta)
		for _, entry := range se.entries {
			// Did we cross this pass's rise during this tick?
			if entry.pass.Rise.After(oldSimTime) && !entry.pass.Rise.After(se.simTime) {
				// Snap to (rise - lookahead) if we also jumped past that,
				// otherwise snap to rise directly.
				lookaheadPoint := entry.pass.Rise.Add(-time.Duration(se.lookaheadSec * float64(time.Second)))
				if lookaheadPoint.After(oldSimTime) {
					se.simTime = lookaheadPoint
				} else {
					se.simTime = entry.pass.Rise
				}
				break
			}
		}
	}
}

// Evaluate returns the link state and coverage state at the current
// simulation time. This is a pure read — it does NOT advance the
// simulation clock. Safe to call concurrently from multiple goroutines
// (SSE, TUI, recorder, coverage logger, GET /condition).
//
// The `now` parameter is ignored — state is determined by the internal
// simTime advanced by Advance(). The parameter exists to satisfy the
// condition.Eval interface.
func (se *SequenceEvaluator) Evaluate(_ time.Time) (condition.LinkState, condition.CoverageState) {
	se.mu.RLock()
	simTime := se.simTime
	se.mu.RUnlock()

	return se.stateAt(simTime)
}

// stateAt computes link state and coverage state for a given simulation time.
// Pure function (no mutation).
func (se *SequenceEvaluator) stateAt(simTime time.Time) (condition.LinkState, condition.CoverageState) {
	// Find which pass (if any) contains simTime.
	for i, entry := range se.entries {
		if !simTime.Before(entry.pass.Rise) && simTime.Before(entry.pass.Set) {
			// In coverage: delegate to this pass's evaluator.
			link, _ := entry.eval.Evaluate(simTime)
			// Build our own CoverageState with correct UntilNextTransition.
			untilSet := entry.pass.Set.Sub(simTime).Seconds()
			cov := condition.CoverageState{
				InCoverage:             true,
				ElapsedSec:             simTime.Sub(entry.pass.Rise).Seconds(),
				UntilNextTransitionSec: untilSet,
			}
			return link, cov
		}

		// Check if we're in the gap before this pass.
		if simTime.Before(entry.pass.Rise) {
			untilRise := entry.pass.Rise.Sub(simTime).Seconds()
			elapsed := se.gapElapsed(simTime, i)
			nan := math.NaN()
			return condition.LinkState{
					DelayMs: nan, JitterMs: nan, LossPct: nan, BandwidthKbps: nan,
				}, condition.CoverageState{
					InCoverage:             false,
					ElapsedSec:             elapsed,
					UntilNextTransitionSec: untilRise,
				}
		}
	}

	// Past all passes: permanent out-of-coverage.
	lastSet := se.entries[len(se.entries)-1].pass.Set
	elapsed := simTime.Sub(lastSet).Seconds()
	nan := math.NaN()
	return condition.LinkState{
			DelayMs: nan, JitterMs: nan, LossPct: nan, BandwidthKbps: nan,
		}, condition.CoverageState{
			InCoverage:             false,
			ElapsedSec:             elapsed,
			UntilNextTransitionSec: math.Inf(1), // No next transition
		}
}

// gapElapsed computes how long we've been in the current gap.
func (se *SequenceEvaluator) gapElapsed(simTime time.Time, nextPassIdx int) float64 {
	if nextPassIdx == 0 {
		// Before first pass: elapsed since simulation start.
		return simTime.Sub(se.startAt).Seconds()
	}
	// After previous pass set.
	prevSet := se.entries[nextPassIdx-1].pass.Set
	return simTime.Sub(prevSet).Seconds()
}

// Passes returns the predicted passes held by this evaluator.
func (se *SequenceEvaluator) Passes() []Pass {
	passes := make([]Pass, len(se.entries))
	for i, e := range se.entries {
		passes[i] = e.pass
	}
	return passes
}

// Lookahead returns absolute rise/set prediction for the current or next
// pass. The wall-clock `now` argument is ignored (same as Evaluate).
func (se *SequenceEvaluator) Lookahead(_ time.Time) condition.LookaheadState {
	se.mu.RLock()
	simTime := se.simTime
	se.mu.RUnlock()

	_, cov := se.stateAt(simTime)
	st := condition.LookaheadState{
		InCoverage:             cov.InCoverage,
		UntilNextTransitionSec: cov.UntilNextTransitionSec,
		ConfiguredLookaheadSec: se.lookaheadSec,
	}

	if math.IsInf(cov.UntilNextTransitionSec, 0) {
		return st
	}

	for _, entry := range se.entries {
		pass := entry.pass
		if !simTime.Before(pass.Rise) && simTime.Before(pass.Set) {
			dur := pass.Set.Sub(pass.Rise).Seconds()
			st.NextOpenAt = condition.TimePtr(pass.Rise)
			st.NextCloseAt = condition.TimePtr(pass.Set)
			st.NextWindowDurationSec = condition.Float64Ptr(dur)
			st.MaxElevationDeg = condition.Float64Ptr(pass.MaxElevDeg)
			return st
		}
		if simTime.Before(pass.Rise) {
			dur := pass.Set.Sub(pass.Rise).Seconds()
			st.NextOpenAt = condition.TimePtr(pass.Rise)
			st.NextCloseAt = condition.TimePtr(pass.Set)
			st.NextWindowDurationSec = condition.Float64Ptr(dur)
			st.MaxElevationDeg = condition.Float64Ptr(pass.MaxElevDeg)
			return st
		}
	}
	return st
}

// LookaheadSec returns the configured lookahead.
func (se *SequenceEvaluator) LookaheadSec() float64 {
	return se.lookaheadSec
}

// SimTime returns the current simulation time (thread-safe).
func (se *SequenceEvaluator) SimTime() time.Time {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.simTime
}

// Position computes the satellite's current geodetic position and look
// angles from the observer at the current simulation time. Implements
// condition.Positioner. Thread-safe (reads simTime under RLock).
func (se *SequenceEvaluator) Position() (latDeg, lonDeg, altKm, elevDeg, azDeg, rangeKm float64) {
	se.mu.RLock()
	t := se.simTime
	se.mu.RUnlock()

	pos, ok := geodeticAt(se.sgp4Sat, se.obsLL, se.observer.AltKm, t)
	if !ok {
		return 0, 0, 0, -90, 0, 0
	}
	return pos.LatDeg, pos.LonDeg, pos.AltKm, pos.ElevationDeg, pos.AzimuthDeg, pos.RangeKm
}

// Observer returns the stored observer location.
func (se *SequenceEvaluator) Observer() Observer {
	return se.observer
}

// Sat returns the stored satellite TLE data.
func (se *SequenceEvaluator) SatData() Satellite {
	return se.sat
}
