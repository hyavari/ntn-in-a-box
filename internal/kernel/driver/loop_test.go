package driver

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// fastProfile returns a profile with a short period for testing:
// 2s window in a 4s period, 0.5s lookahead.
func fastProfile() profile.Profile {
	return profile.Profile{
		Name: "fast_test",
		Schedule: profile.Schedule{
			Mode:         profile.ModePeriodic,
			PeriodSec:    4,
			WindowSec:    2,
			LookaheadSec: 0.5,
		},
		Curves: profile.Curves{
			DelayMs:       []profile.Point{{OffsetSec: 0, Value: 100}, {OffsetSec: 2, Value: 50}},
			JitterMs:      []profile.Point{{OffsetSec: 0, Value: 10}, {OffsetSec: 2, Value: 5}},
			LossPct:       []profile.Point{{OffsetSec: 0, Value: 5}, {OffsetSec: 2, Value: 1}},
			BandwidthKbps: []profile.Point{{OffsetSec: 0, Value: 1000}, {OffsetSec: 2, Value: 5000}},
		},
	}
}

// clock is a thread-safe injectable clock for tests.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func TestDriverLoopCoverageTransitions(t *testing.T) {
	p := fastProfile()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eval, err := condition.NewEvaluator(p, epoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	var mu sync.Mutex
	var covEvents []eventbus.CoverageEvent
	bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) {
		mu.Lock()
		covEvents = append(covEvents, ev)
		mu.Unlock()
	})

	tickCh := make(chan time.Time, 100)
	clk := &clock{t: epoch}

	loop := New(Config{
		Evaluator:    eval,
		Bus:          bus,
		LookaheadSec: p.Schedule.LookaheadSec,
		TickCh:       tickCh,
		Now:          clk.now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(done)
	}()

	// Advance through the full period in 50ms steps.
	step := 50 * time.Millisecond
	for i := 0; i < 80; i++ { // 80 * 50ms = 4s = one full period
		clk.set(epoch.Add(time.Duration(i) * step))
		tickCh <- time.Time{} // value unused; loop calls clk.now()
		time.Sleep(1 * time.Millisecond)
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()

	// Expected event sequence for a periodic profile starting in coverage:
	// 1. window_opened (initial state, t=0)
	// 2. window_closing (lookahead: when UntilClose <= 0.5s, i.e. t >= 1.5s)
	// 3. window_closed (at t=2s)
	// 4. window_opening (lookahead: when UntilOpen <= 0.5s, i.e. t >= 3.5s)

	if len(covEvents) < 4 {
		t.Fatalf("got %d coverage events, want at least 4; events: %v", len(covEvents), covEventKinds(covEvents))
	}

	wantKinds := []eventbus.CoverageEventKind{
		eventbus.KindWindowOpened,  // initial (t=0, in coverage)
		eventbus.KindWindowClosing, // lookahead (t~1.5s)
		eventbus.KindWindowClosed,  // transition (t~2.0s)
		eventbus.KindWindowOpening, // lookahead (t~3.5s)
	}

	for i, want := range wantKinds {
		if covEvents[i].Kind != want {
			t.Errorf("event[%d].Kind = %q, want %q (all: %v)", i, covEvents[i].Kind, want, covEventKinds(covEvents))
		}
	}
}

func TestDriverLoopLinkStatePublished(t *testing.T) {
	p := fastProfile()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eval, err := condition.NewEvaluator(p, epoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	var mu sync.Mutex
	var linkEvents []eventbus.LinkStateEvent
	bus.SubscribeLinkState(func(ev eventbus.LinkStateEvent) {
		mu.Lock()
		linkEvents = append(linkEvents, ev)
		mu.Unlock()
	})

	tickCh := make(chan time.Time, 50)
	clk := &clock{t: epoch}

	loop := New(Config{
		Evaluator:    eval,
		Bus:          bus,
		LookaheadSec: p.Schedule.LookaheadSec,
		TickCh:       tickCh,
		Now:          clk.now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(done)
	}()

	// Tick a few times while in coverage (first 2s).
	for i := 1; i <= 10; i++ {
		clk.set(epoch.Add(time.Duration(i) * 100 * time.Millisecond))
		tickCh <- time.Time{}
		time.Sleep(1 * time.Millisecond)
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()

	// Should have at least one link state event (the initial one).
	if len(linkEvents) == 0 {
		t.Fatal("got 0 link state events, want at least 1")
	}

	// First event should have delay ~100ms (profile starts at 100ms at t=0).
	first := linkEvents[0]
	if first.State.DelayMs < 50 || first.State.DelayMs > 100 {
		t.Errorf("first link state DelayMs = %.1f, want ~100", first.State.DelayMs)
	}
	if first.State.BandwidthKbps < 1000 || first.State.BandwidthKbps > 5000 {
		t.Errorf("first link state BandwidthKbps = %.1f, want ~1000-5000", first.State.BandwidthKbps)
	}
}

func TestDriverLoopNoLinkStateOutOfCoverage(t *testing.T) {
	p := fastProfile()
	// Start epoch so that we begin OUT of coverage (2.5s into the period).
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	startTime := epoch.Add(2500 * time.Millisecond)

	eval, err := condition.NewEvaluator(p, epoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	var mu sync.Mutex
	var linkEvents []eventbus.LinkStateEvent
	var covEvents []eventbus.CoverageEvent
	bus.SubscribeLinkState(func(ev eventbus.LinkStateEvent) {
		mu.Lock()
		linkEvents = append(linkEvents, ev)
		mu.Unlock()
	})
	bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) {
		mu.Lock()
		covEvents = append(covEvents, ev)
		mu.Unlock()
	})

	tickCh := make(chan time.Time, 50)
	clk := &clock{t: startTime}

	loop := New(Config{
		Evaluator:    eval,
		Bus:          bus,
		LookaheadSec: p.Schedule.LookaheadSec,
		TickCh:       tickCh,
		Now:          clk.now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(done)
	}()

	// Tick 5 times while staying out of coverage (2.5s to 3.0s, gap is 2s-4s).
	for i := 1; i <= 5; i++ {
		clk.set(startTime.Add(time.Duration(i) * 100 * time.Millisecond))
		tickCh <- time.Time{}
		time.Sleep(1 * time.Millisecond)
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()

	// Should have no link state events (out of coverage).
	if len(linkEvents) != 0 {
		t.Errorf("got %d link state events while out of coverage, want 0", len(linkEvents))
	}

	// Should have initial window_closed event.
	if len(covEvents) == 0 {
		t.Fatal("got 0 coverage events, want at least 1 (initial closed)")
	}
	if covEvents[0].Kind != eventbus.KindWindowClosed {
		t.Errorf("first event = %q, want window_closed", covEvents[0].Kind)
	}
}

func TestDriverLoopContinuousMode(t *testing.T) {
	p := profile.Profile{
		Name: "continuous_test",
		Schedule: profile.Schedule{
			Mode:      profile.ModeContinuous,
			PeriodSec: 2,
		},
		Curves: profile.Curves{
			DelayMs:       []profile.Point{{OffsetSec: 0, Value: 30}, {OffsetSec: 2, Value: 30}},
			JitterMs:      []profile.Point{{OffsetSec: 0, Value: 2}},
			LossPct:       []profile.Point{{OffsetSec: 0, Value: 0.1}},
			BandwidthKbps: []profile.Point{{OffsetSec: 0, Value: 10000}},
		},
	}

	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	eval, err := condition.NewEvaluator(p, epoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	var mu sync.Mutex
	var covEvents []eventbus.CoverageEvent
	var linkEvents []eventbus.LinkStateEvent
	bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) {
		mu.Lock()
		covEvents = append(covEvents, ev)
		mu.Unlock()
	})
	bus.SubscribeLinkState(func(ev eventbus.LinkStateEvent) {
		mu.Lock()
		linkEvents = append(linkEvents, ev)
		mu.Unlock()
	})

	tickCh := make(chan time.Time, 50)
	clk := &clock{t: epoch}

	loop := New(Config{
		Evaluator:    eval,
		Bus:          bus,
		LookaheadSec: 0, // continuous mode has no lookahead
		TickCh:       tickCh,
		Now:          clk.now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		loop.Run(ctx)
		close(done)
	}()

	// Tick through the full cycle.
	for i := 1; i <= 20; i++ {
		clk.set(epoch.Add(time.Duration(i) * 100 * time.Millisecond))
		tickCh <- time.Time{}
		time.Sleep(1 * time.Millisecond)
	}

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()

	// Continuous mode: should only get window_opened (initial) and link states.
	if len(covEvents) != 1 {
		t.Errorf("got %d coverage events, want 1 (initial opened); kinds: %v", len(covEvents), covEventKinds(covEvents))
	}
	if len(covEvents) > 0 && covEvents[0].Kind != eventbus.KindWindowOpened {
		t.Errorf("first event = %q, want window_opened", covEvents[0].Kind)
	}

	// Should have link state events.
	if len(linkEvents) == 0 {
		t.Error("got 0 link state events in continuous mode, want some")
	}
}

func covEventKinds(events []eventbus.CoverageEvent) []string {
	kinds := make([]string, len(events))
	for i, ev := range events {
		kinds[i] = string(ev.Kind)
	}
	return kinds
}
