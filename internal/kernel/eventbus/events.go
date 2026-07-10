package eventbus

import (
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

// CoverageEventKind identifies which kind of coverage transition a
// CoverageEvent describes. "Opening"/"Closing" are lookahead notices
// (the transition hasn't happened yet, but is scheduled); "Opened"/
// "Closed" fire at the instant the transition actually occurs.
type CoverageEventKind string

// Coverage event kinds.
const (
	// KindWindowOpening is a lookahead notice: the window will open soon.
	KindWindowOpening CoverageEventKind = "window_opening"
	// KindWindowOpened fires the instant the window actually opens.
	KindWindowOpened CoverageEventKind = "window_opened"
	// KindWindowClosing is a lookahead notice: the window will close soon.
	KindWindowClosing CoverageEventKind = "window_closing"
	// KindWindowClosed fires the instant the window actually closes.
	KindWindowClosed CoverageEventKind = "window_closed"
)

// CoverageEvent is published for every coverage transition and its
// lookahead notice. Coverage transitions are discrete and always
// published — they are never throttled, unlike LinkStateEvent.
type CoverageEvent struct {
	Kind CoverageEventKind
	At   time.Time

	// Optional pre-computed state (used by replay mode when no
	// evaluator is available). Zero values mean "not provided."
	InCoverage          bool
	ElapsedSec          float64
	UntilNextTransition float64
}

// LinkStateEvent carries a snapshot of link impairment values.
// Publication is throttled — see LinkStateThrottle — so subscribers
// should not assume every possible instant is represented, only that
// meaningful changes and a periodic heartbeat are.
//
// Callers should only publish a LinkStateEvent while in coverage; use
// CoverageEvent for transitions, not a LinkState with NaN fields.
type LinkStateEvent struct {
	State condition.LinkState
	At    time.Time
}

// ObservabilityEvent is a generic, unthrottled event for metrics/
// tracing — the "emit" hook in the module contract.
type ObservabilityEvent struct {
	Name   string
	Fields map[string]any
	At     time.Time
}

// Well-known observability event names.
const (
	// ObsReplayDone is emitted when a replay session completes.
	ObsReplayDone = "replay_done"
)

// CoverageHandler is called for every published CoverageEvent.
type CoverageHandler func(CoverageEvent)

// LinkStateHandler is called for every LinkStateEvent that passes the
// throttle.
type LinkStateHandler func(LinkStateEvent)

// ObservabilityHandler is called for every published ObservabilityEvent.
type ObservabilityHandler func(ObservabilityEvent)

// SatellitePositionEvent carries the satellite's current geodetic
// position and look angles from the observer. Published every ~1s
// in TLE mode only.
type SatellitePositionEvent struct {
	LatDeg       float64
	LonDeg       float64
	AltKm        float64
	ElevationDeg float64
	AzimuthDeg   float64
	RangeKm      float64
	At           time.Time
}

// SatellitePositionHandler is called for every SatellitePositionEvent.
type SatellitePositionHandler func(SatellitePositionEvent)
