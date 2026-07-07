package driver

import (
	"context"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// DefaultInterval is the default tick interval for the driver loop.
// Matches the event bus's default link-state throttle heartbeat (250ms).
const DefaultInterval = 250 * time.Millisecond

// Config holds the driver loop's dependencies.
type Config struct {
	// Evaluator computes coverage + link state at any instant.
	Evaluator *condition.Evaluator

	// Bus publishes events to subscribers.
	Bus *eventbus.Bus

	// LookaheadSec is how far in advance (in seconds) to announce an
	// upcoming coverage transition. Copied from the profile's schedule
	// at construction time so the driver doesn't need access to the
	// profile struct.
	LookaheadSec float64

	// TickCh, if non-nil, replaces the real time.Ticker for testing.
	// Each receive on TickCh triggers one evaluation cycle. When nil,
	// a real ticker at Interval is used.
	TickCh <-chan time.Time

	// Interval is the tick interval. Ignored if TickCh is set.
	// Zero means DefaultInterval.
	Interval time.Duration

	// Now, if non-nil, replaces time.Now for evaluation. Useful in
	// tests with injected tick channels where wall-clock time doesn't
	// advance predictably.
	Now func() time.Time
}

// Loop is the kernel's driver loop. It ticks at a fixed interval,
// evaluates the Condition Engine, and publishes events to the bus.
// Safe to run from a single goroutine (not internally concurrent).
type Loop struct {
	eval         *condition.Evaluator
	bus          *eventbus.Bus
	lookaheadSec float64
	tickCh       <-chan time.Time
	interval     time.Duration
	now          func() time.Time

	// State tracked between ticks for transition detection.
	prevInCoverage      bool
	prevInCoverageSet   bool // false until first tick
	lookaheadOpenFired  bool
	lookaheadCloseFired bool
}

// New creates a Loop from the given config.
func New(cfg Config) *Loop {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultInterval
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Loop{
		eval:         cfg.Evaluator,
		bus:          cfg.Bus,
		lookaheadSec: cfg.LookaheadSec,
		tickCh:       cfg.TickCh,
		interval:     interval,
		now:          nowFn,
	}
}

// Run blocks until ctx is cancelled, evaluating on each tick and
// publishing events. It performs one immediate evaluation before
// waiting for the first tick.
func (l *Loop) Run(ctx context.Context) {
	var tickCh <-chan time.Time
	if l.tickCh != nil {
		tickCh = l.tickCh
	} else {
		ticker := time.NewTicker(l.interval)
		defer ticker.Stop()
		tickCh = ticker.C
	}

	// Evaluate immediately on start so subscribers get initial state.
	l.tick()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tickCh:
			l.tick()
		}
	}
}

func (l *Loop) tick() {
	now := l.now()
	link, cov := l.eval.Evaluate(now)

	// Detect coverage transitions.
	if !l.prevInCoverageSet {
		// First tick: publish the initial state as a transition so
		// subscribers know where they stand.
		l.prevInCoverageSet = true
		l.prevInCoverage = cov.InCoverage
		if cov.InCoverage {
			l.bus.PublishCoverageEvent(eventbus.CoverageEvent{
				Kind: eventbus.KindWindowOpened,
				At:   now,
			})
		} else {
			l.bus.PublishCoverageEvent(eventbus.CoverageEvent{
				Kind: eventbus.KindWindowClosed,
				At:   now,
			})
		}
	} else if cov.InCoverage != l.prevInCoverage {
		// Actual transition.
		l.prevInCoverage = cov.InCoverage
		if cov.InCoverage {
			l.bus.PublishCoverageEvent(eventbus.CoverageEvent{
				Kind: eventbus.KindWindowOpened,
				At:   now,
			})
			l.lookaheadOpenFired = false
			l.lookaheadCloseFired = false
		} else {
			l.bus.PublishCoverageEvent(eventbus.CoverageEvent{
				Kind: eventbus.KindWindowClosed,
				At:   now,
			})
			l.lookaheadOpenFired = false
			l.lookaheadCloseFired = false
		}
	}

	// Lookahead notices.
	if l.lookaheadSec > 0 {
		if cov.InCoverage && !l.lookaheadCloseFired && cov.UntilNextTransitionSec <= l.lookaheadSec {
			l.lookaheadCloseFired = true
			l.bus.PublishCoverageEvent(eventbus.CoverageEvent{
				Kind: eventbus.KindWindowClosing,
				At:   now,
			})
		}
		if !cov.InCoverage && !l.lookaheadOpenFired && cov.UntilNextTransitionSec <= l.lookaheadSec {
			l.lookaheadOpenFired = true
			l.bus.PublishCoverageEvent(eventbus.CoverageEvent{
				Kind: eventbus.KindWindowOpening,
				At:   now,
			})
		}
	}

	// Publish link state while in coverage.
	if cov.InCoverage {
		l.bus.PublishLinkState(link, now)
	}
}
