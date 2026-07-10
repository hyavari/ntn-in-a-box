package condition

import "time"

// Eval computes link state and coverage state at a given instant.
// Satisfied by *Evaluator and by external evaluator implementations
// (e.g. tle.SequenceEvaluator).
type Eval interface {
	Evaluate(now time.Time) (LinkState, CoverageState)
}

// Advancer is an optional interface that an Eval may implement if it
// needs explicit time advancement (e.g. variable-rate simulation).
// The driver calls Advance(wallNow) once per tick before Evaluate.
// Regular Evaluators do not implement this (they derive state from now).
type Advancer interface {
	Advance(wallNow time.Time)
}
