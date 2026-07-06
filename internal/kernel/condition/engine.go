package condition

import (
	"fmt"
	"math"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// Evaluator computes coverage state and link impairment values for a
// Profile at any point in time, relative to a fixed schedule start
// (epoch) — the instant the profile's first cycle/window begins.
//
// Evaluator holds a validated copy of the profile so that Coverage and
// Evaluate never need to re-check invariants (period > 0, curves
// non-empty, etc.) that profile.Profile.Validate already guarantees.
type Evaluator struct {
	profile profile.Profile
	epoch   time.Time
}

// NewEvaluator validates p and returns an Evaluator anchored at epoch.
// Returns an error if p fails profile.Profile.Validate.
//
// The returned Evaluator holds its own copy of p's curve slices (not
// just a copy of the Profile struct, which would still share backing
// arrays with the caller's slices) — mutating p after calling
// NewEvaluator does not affect the Evaluator.
func NewEvaluator(p profile.Profile, epoch time.Time) (*Evaluator, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("condition: invalid profile: %w", err)
	}
	return &Evaluator{profile: cloneProfile(p), epoch: epoch}, nil
}

// cloneProfile copies p, giving the copy's curve slices their own
// backing arrays instead of sharing p's.
func cloneProfile(p profile.Profile) profile.Profile {
	clone := p
	clone.Curves.DelayMs = append([]profile.Point(nil), p.Curves.DelayMs...)
	clone.Curves.JitterMs = append([]profile.Point(nil), p.Curves.JitterMs...)
	clone.Curves.LossPct = append([]profile.Point(nil), p.Curves.LossPct...)
	clone.Curves.BandwidthKbps = append([]profile.Point(nil), p.Curves.BandwidthKbps...)
	return clone
}

// LinkState holds interpolated impairment values at a given instant.
//
// Callers must check the accompanying CoverageState.InCoverage: when
// false, every field is NaN — there is no link, so "0ms delay" or
// "0 loss" would be a misleading reading rather than an absent one.
type LinkState struct {
	DelayMs       float64
	JitterMs      float64
	LossPct       float64
	BandwidthKbps float64
}

// Evaluate returns both the CoverageState and LinkState for instant now.
func (e *Evaluator) Evaluate(now time.Time) (LinkState, CoverageState) {
	cov := e.Coverage(now)
	if !cov.InCoverage {
		nan := math.NaN()
		return LinkState{DelayMs: nan, JitterMs: nan, LossPct: nan, BandwidthKbps: nan}, cov
	}

	c := e.profile.Curves
	return LinkState{
		DelayMs:       evaluateCurve(c.DelayMs, cov.ElapsedSec),
		JitterMs:      evaluateCurve(c.JitterMs, cov.ElapsedSec),
		LossPct:       evaluateCurve(c.LossPct, cov.ElapsedSec),
		BandwidthKbps: evaluateCurve(c.BandwidthKbps, cov.ElapsedSec),
	}, cov
}
