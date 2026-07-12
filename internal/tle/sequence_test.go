package tle

import (
	"math"
	"testing"
	"time"
)

func syntheticPasses() []Pass {
	base := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)

	// Pass 1: 12:01:00 - 12:06:00 (5 minutes)
	rise1 := base.Add(1 * time.Minute)
	set1 := base.Add(6 * time.Minute)

	// Pass 2: 12:20:00 - 12:25:00 (5 minutes), 14 min gap
	rise2 := base.Add(20 * time.Minute)
	set2 := base.Add(25 * time.Minute)

	makeSamples := func(rise, set time.Time) []ElevSample {
		dur := set.Sub(rise)
		var samples []ElevSample
		for offset := time.Duration(0); offset <= dur; offset += 5 * time.Second {
			t := rise.Add(offset)
			frac := float64(offset) / float64(dur)
			// Simple arc: 10° at edges, 60° at peak
			elev := 10 + 50*math.Sin(frac*math.Pi)
			samples = append(samples, ElevSample{T: t, ElevDeg: elev, RangeKm: 1000})
		}
		return samples
	}

	return []Pass{
		{
			Satellite: "TEST", NoradID: 99999,
			Rise: rise1, Set: set1, Duration: set1.Sub(rise1),
			MaxElevDeg: 60, MaxElevTime: rise1.Add(set1.Sub(rise1) / 2),
			Samples: makeSamples(rise1, set1),
		},
		{
			Satellite: "TEST", NoradID: 99999,
			Rise: rise2, Set: set2, Duration: set2.Sub(rise2),
			MaxElevDeg: 60, MaxElevTime: rise2.Add(set2.Sub(rise2) / 2),
			Samples: makeSamples(rise2, set2),
		},
	}
}

func TestSequenceEvaluator_InCoverage(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()

	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:   1.0,
		StartAt: passes[0].Rise,
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	// First advance establishes wallStart.
	wallStart := time.Now()
	se.Advance(wallStart)
	link, cov := se.Evaluate(wallStart)

	if !cov.InCoverage {
		t.Fatal("expected in-coverage at pass rise")
	}
	if cov.ElapsedSec != 0 {
		t.Errorf("ElapsedSec = %f, want 0", cov.ElapsedSec)
	}
	if math.IsNaN(link.DelayMs) {
		t.Error("DelayMs is NaN during coverage")
	}

	// 2.5 minutes into pass 1.
	se.Advance(wallStart.Add(150 * time.Second))
	link2, cov2 := se.Evaluate(time.Time{})
	if !cov2.InCoverage {
		t.Fatal("expected in-coverage at mid-pass")
	}
	if cov2.ElapsedSec < 149 || cov2.ElapsedSec > 151 {
		t.Errorf("ElapsedSec = %f, want ~150", cov2.ElapsedSec)
	}
	if math.IsNaN(link2.DelayMs) {
		t.Error("DelayMs is NaN during coverage")
	}
}

func TestSequenceEvaluator_Gap(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()

	// Start at 30s before first pass rise.
	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:   1.0,
		StartAt: passes[0].Rise.Add(-30 * time.Second),
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	wallStart := time.Now()
	se.Advance(wallStart)
	link, cov := se.Evaluate(time.Time{})

	if cov.InCoverage {
		t.Fatal("expected out-of-coverage before pass rise")
	}
	if math.IsNaN(cov.UntilNextTransitionSec) {
		t.Error("UntilNextTransitionSec is NaN")
	}
	if cov.UntilNextTransitionSec < 29 || cov.UntilNextTransitionSec > 31 {
		t.Errorf("UntilNextTransitionSec = %f, want ~30", cov.UntilNextTransitionSec)
	}
	if !math.IsNaN(link.DelayMs) {
		t.Errorf("DelayMs = %f, want NaN during gap", link.DelayMs)
	}
}

func TestSequenceEvaluator_SpeedAcceleration(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()

	// Gap between passes is 14 minutes (840s). With 10x speed, gap
	// takes 84s of wall time.
	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:   10.0,
		StartAt: passes[0].Set, // Start at end of pass 1 (in gap)
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	wallStart := time.Now()
	se.Advance(wallStart)
	_, cov := se.Evaluate(time.Time{})
	if cov.InCoverage {
		t.Fatal("expected out-of-coverage in gap")
	}

	// After 50 wall-seconds with 10x speed → 500 sim-seconds elapsed.
	// Gap is 840s, so we should still be in gap.
	se.Advance(wallStart.Add(50 * time.Second))
	_, cov2 := se.Evaluate(time.Time{})
	if cov2.InCoverage {
		t.Fatal("expected still in gap after 50 wall-seconds (500 sim-seconds, gap=840s)")
	}

	// After 84+ wall-seconds with 10x → ~840 sim-seconds. Snap lands at
	// (rise - lookahead = rise - 30s) to give driver a chance to fire
	// WindowOpening. Verify we're in the lookahead zone (still gap).
	se.Advance(wallStart.Add(85 * time.Second))
	_, cov3 := se.Evaluate(time.Time{})
	if cov3.InCoverage {
		t.Fatal("expected out-of-coverage at lookahead snap point (rise - 30s)")
	}
	if cov3.UntilNextTransitionSec > 31 || cov3.UntilNextTransitionSec < 29 {
		t.Errorf("UntilNextTransitionSec = %f, want ~30 (lookahead snap)", cov3.UntilNextTransitionSec)
	}

	// Next advance: still gap speed (10x), 1 more wall-second = 10 sim-seconds.
	// Still before rise (need 30 sim-seconds to cover the lookahead gap).
	// 3 more wall-seconds * 10 = 30 sim-seconds → crosses rise, snaps to rise.
	se.Advance(wallStart.Add(89 * time.Second))
	_, cov4 := se.Evaluate(time.Time{})
	if !cov4.InCoverage {
		t.Fatal("expected in-coverage after advancing past lookahead zone")
	}
}

func TestSequenceEvaluator_AllPassesExhausted(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()

	// Start near end of pass 2.
	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:   1.0,
		StartAt: passes[1].Set.Add(-5 * time.Second),
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	wallStart := time.Now()
	se.Advance(wallStart)

	// After 10s: past end of pass 2, all exhausted.
	se.Advance(wallStart.Add(10 * time.Second))
	link, cov := se.Evaluate(time.Time{})
	if cov.InCoverage {
		t.Fatal("expected out-of-coverage after all passes exhausted")
	}
	if !math.IsInf(cov.UntilNextTransitionSec, 1) {
		t.Errorf("UntilNextTransitionSec = %f, want +Inf", cov.UntilNextTransitionSec)
	}
	if !math.IsNaN(link.DelayMs) {
		t.Errorf("DelayMs = %f, want NaN", link.DelayMs)
	}
}

func TestSequenceEvaluator_ConcurrentEvaluate(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()

	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:   1.0,
		StartAt: passes[0].Rise,
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	wallStart := time.Now()
	se.Advance(wallStart)

	// Multiple concurrent Evaluate calls should not race.
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_, cov := se.Evaluate(time.Time{})
			if !cov.InCoverage {
				t.Error("expected in-coverage")
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestSequenceEvaluator_Passes(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()

	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		StartAt: passes[0].Rise,
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	got := se.Passes()
	if len(got) != 2 {
		t.Fatalf("Passes() returned %d, want 2", len(got))
	}
	if got[0].Rise != passes[0].Rise {
		t.Error("Passes()[0].Rise mismatch")
	}
}

func TestSequenceEvaluator_Lookahead(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()
	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:        1.0,
		StartAt:      passes[0].Rise,
		LookaheadSec: 25,
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	wallStart := time.Now()
	se.Advance(wallStart)

	st := se.Lookahead(time.Time{})
	if !st.InCoverage {
		t.Fatal("expected in coverage at rise")
	}
	if st.ConfiguredLookaheadSec != 25 {
		t.Errorf("ConfiguredLookaheadSec = %v, want 25", st.ConfiguredLookaheadSec)
	}
	if st.NextOpenAt == nil || !st.NextOpenAt.Equal(passes[0].Rise) {
		t.Errorf("NextOpenAt = %v, want %v", st.NextOpenAt, passes[0].Rise)
	}
	if st.NextCloseAt == nil || !st.NextCloseAt.Equal(passes[0].Set) {
		t.Errorf("NextCloseAt = %v, want %v", st.NextCloseAt, passes[0].Set)
	}
	if st.MaxElevationDeg == nil || *st.MaxElevationDeg != 60 {
		t.Errorf("MaxElevationDeg = %v, want 60", st.MaxElevationDeg)
	}

	// Into gap before pass 2.
	se.Advance(wallStart.Add(10 * time.Minute))
	st2 := se.Lookahead(time.Time{})
	if st2.InCoverage {
		t.Fatal("expected out of coverage in gap")
	}
	if st2.NextOpenAt == nil || !st2.NextOpenAt.Equal(passes[1].Rise) {
		t.Errorf("NextOpenAt = %v, want %v", st2.NextOpenAt, passes[1].Rise)
	}
}

func TestSequenceEvaluator_LookaheadPastLastPass(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()
	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:   1.0,
		StartAt: passes[1].Set.Add(-5 * time.Second),
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	wallStart := time.Now()
	se.Advance(wallStart)
	se.Advance(wallStart.Add(10 * time.Second))

	st := se.Lookahead(time.Time{})
	if st.InCoverage {
		t.Fatal("expected out of coverage")
	}
	if !math.IsInf(st.UntilNextTransitionSec, 1) {
		t.Errorf("UntilNextTransitionSec = %v, want +Inf", st.UntilNextTransitionSec)
	}
	if st.NextOpenAt != nil || st.NextCloseAt != nil || st.NextWindowDurationSec != nil || st.MaxElevationDeg != nil {
		t.Errorf("expected omitted absolute fields, got %+v", st)
	}
}
