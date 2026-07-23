package profile

import (
	"errors"
	"fmt"
	"math"
)

// Validate checks that a Profile is well-formed enough for the
// Condition Engine to evaluate: known schedule mode, consistent
// timings, and curves that are non-empty, start at offset 0, strictly
// ascending, within the valid range for the profile's mode, end exactly
// at that range's boundary (or are a single constant point), and are
// physically sane (no NaNs, non-negative, loss_pct within [0, 100]).
//
// All problems found are returned together (via errors.Join) rather
// than stopping at the first one, since profile authors will usually
// want to fix everything in one pass.
func (p *Profile) Validate() error {
	var errs []error

	if p.Name == "" {
		errs = append(errs, errors.New("name is required"))
	}

	errs = append(errs, p.validateSchedule()...)

	validRange := p.curveRange()
	errs = append(errs, validateCurve("delay_ms", p.Curves.DelayMs, validRange, false)...)
	errs = append(errs, validateCurve("jitter_ms", p.Curves.JitterMs, validRange, false)...)
	errs = append(errs, validateCurve("loss_pct", p.Curves.LossPct, validRange, true)...)
	errs = append(errs, validateCurve("bandwidth_kbps", p.Curves.BandwidthKbps, validRange, false)...)

	errs = append(errs, p.validateBlockages()...)

	return errors.Join(errs...)
}

// validateBlockages checks that blockage intervals are well-formed: each
// has a non-negative offset and positive duration, fits entirely within
// one schedule cycle (no wrap past period_sec), and that the set is
// strictly ascending and non-overlapping. Blockages repeat every cycle,
// so an interval that ran past period_sec would have ambiguous meaning.
func (p *Profile) validateBlockages() []error {
	var errs []error

	period := p.Schedule.PeriodSec
	var prevEnd float64
	for i, b := range p.Blockages {
		if math.IsNaN(b.OffsetSec) || math.IsNaN(b.DurationSec) {
			errs = append(errs, fmt.Errorf("blockages[%d]: offset_sec and duration_sec must not be NaN", i))
			continue // further comparisons against NaN are meaningless
		}
		if b.OffsetSec < 0 {
			errs = append(errs, fmt.Errorf("blockages[%d]: offset_sec must be >= 0, got %.2f", i, b.OffsetSec))
		}
		if b.DurationSec <= 0 {
			errs = append(errs, fmt.Errorf("blockages[%d]: duration_sec must be > 0, got %.2f", i, b.DurationSec))
		}
		// Only range/ordering-check once the interval itself is sane and
		// the period is known-valid (period<=0 is already reported).
		if b.OffsetSec >= 0 && b.DurationSec > 0 {
			if period > 0 && b.OffsetSec+b.DurationSec > period {
				errs = append(errs, fmt.Errorf(
					"blockages[%d]: offset_sec + duration_sec (%.2f) must be <= schedule.period_sec (%.2f) — a blockage cannot wrap past the cycle end",
					i, b.OffsetSec+b.DurationSec, period))
			}
			if i > 0 && b.OffsetSec < prevEnd {
				errs = append(errs, fmt.Errorf(
					"blockages[%d]: offset_sec %.2f overlaps the previous blockage (ends at %.2f) — blockages must be strictly ascending and non-overlapping",
					i, b.OffsetSec, prevEnd))
			}
			prevEnd = b.OffsetSec + b.DurationSec
		}
	}

	return errs
}

func (p *Profile) validateSchedule() []error {
	var errs []error
	s := p.Schedule

	switch s.Mode {
	case ModePeriodic, ModeContinuous:
		// ok
	case "":
		errs = append(errs, errors.New("schedule.mode is required (periodic or continuous)"))
	default:
		errs = append(errs, fmt.Errorf("schedule.mode %q is not one of: periodic, continuous", s.Mode))
	}

	// These checks apply regardless of mode (including an unrecognized
	// one) so that a single invalid field doesn't hide every other
	// problem in the schedule.
	if s.PeriodSec <= 0 {
		errs = append(errs, errors.New("schedule.period_sec must be > 0"))
	}
	if s.LookaheadSec < 0 {
		errs = append(errs, errors.New("schedule.lookahead_sec must be >= 0"))
	}

	// window_sec and the lookahead-vs-gap cross-check only apply to
	// periodic mode: continuous mode has no out-of-coverage gap, so
	// window_sec is unused and any non-negative lookahead is moot.
	if s.Mode == ModePeriodic {
		if s.WindowSec <= 0 {
			errs = append(errs, errors.New("schedule.window_sec must be > 0 for periodic mode"))
		} else if s.PeriodSec > 0 && s.WindowSec > s.PeriodSec {
			errs = append(errs, errors.New("schedule.window_sec must be <= schedule.period_sec"))
		}

		if s.PeriodSec > 0 && s.WindowSec > 0 && s.WindowSec <= s.PeriodSec && s.LookaheadSec >= 0 {
			gap := s.PeriodSec - s.WindowSec
			if s.LookaheadSec > gap {
				errs = append(errs, fmt.Errorf(
					"schedule.lookahead_sec (%.2f) must be <= the out-of-coverage gap (%.2f = period_sec - window_sec)",
					s.LookaheadSec, gap))
			}
		}
	}

	return errs
}

// curveRange returns the valid offset range [0, max] for curve points,
// depending on the profile's schedule mode.
func (p *Profile) curveRange() float64 {
	if p.Schedule.Mode == ModePeriodic {
		return p.Schedule.WindowSec
	}
	return p.Schedule.PeriodSec
}

// validateCurve checks one impairment curve. maxOffset is the valid
// upper bound for offset_sec (window_sec for periodic profiles,
// period_sec for continuous ones); a value of 0 or less means the bound
// itself is invalid (already reported by validateSchedule) and offset
// range checks are skipped to avoid confusing cascading errors.
func validateCurve(name string, points []Point, maxOffset float64, isPercent bool) []error {
	var errs []error

	if len(points) == 0 {
		errs = append(errs, fmt.Errorf("curves.%s must have at least one point", name))
		return errs
	}

	if points[0].OffsetSec != 0 {
		errs = append(errs, fmt.Errorf("curves.%s: first point must be at offset_sec 0, got %.2f", name, points[0].OffsetSec))
	}

	for i, pt := range points {
		if math.IsNaN(pt.OffsetSec) || math.IsNaN(pt.Value) {
			errs = append(errs, fmt.Errorf("curves.%s[%d]: offset_sec and value must not be NaN", name, i))
			continue // further numeric comparisons against NaN are meaningless
		}

		if pt.Value < 0 {
			errs = append(errs, fmt.Errorf("curves.%s[%d]: value must be >= 0, got %.2f", name, i, pt.Value))
		}
		if isPercent && pt.Value > 100 {
			errs = append(errs, fmt.Errorf("curves.%s[%d]: value must be <= 100, got %.2f", name, i, pt.Value))
		}
		if maxOffset > 0 && pt.OffsetSec > maxOffset {
			errs = append(errs, fmt.Errorf("curves.%s[%d]: offset_sec %.2f exceeds valid range [0, %.2f]", name, i, pt.OffsetSec, maxOffset))
		}
		if i > 0 && pt.OffsetSec <= points[i-1].OffsetSec {
			errs = append(errs, fmt.Errorf("curves.%s[%d]: offset_sec must be strictly ascending (got %.2f after %.2f)", name, i, pt.OffsetSec, points[i-1].OffsetSec))
		}
	}

	// Tail semantics: a curve either explicitly covers the full range
	// (last point's offset_sec == maxOffset) or is a single point,
	// which is shorthand for "this value holds constant across the
	// whole range" (e.g. a flat GEO-steady curve). Anything in
	// between — a multi-point curve that stops short of maxOffset —
	// would leave "what happens after the last point" undefined for
	// whatever evaluates the curve, so it's rejected here instead.
	if len(points) > 1 && maxOffset > 0 {
		last := points[len(points)-1]
		if !math.IsNaN(last.OffsetSec) && last.OffsetSec != maxOffset {
			errs = append(errs, fmt.Errorf(
				"curves.%s: last point must be at offset_sec %.2f (the end of the window/cycle), got %.2f — use a single point instead to represent a constant value",
				name, maxOffset, last.OffsetSec))
		}
	}

	return errs
}
