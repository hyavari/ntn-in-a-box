package condition

import "time"

// LookaheadState is the prediction snapshot for GET /lookahead and SSE
// enrichment on window_opening / window_closing.
type LookaheadState struct {
	InCoverage             bool
	UntilNextTransitionSec float64
	NextOpenAt             *time.Time
	NextCloseAt            *time.Time
	NextWindowDurationSec  *float64
	MaxElevationDeg        *float64
	ConfiguredLookaheadSec float64
}

// LookaheadProvider is implemented by evaluators that can expose absolute
// open/close times and related prediction fields.
type LookaheadProvider interface {
	Lookahead(now time.Time) LookaheadState
}

// Float64Ptr returns a pointer to v (helper for optional JSON fields).
func Float64Ptr(v float64) *float64 { return &v }

// TimePtr returns a pointer to t.
func TimePtr(t time.Time) *time.Time { return &t }
