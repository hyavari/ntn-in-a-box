package condition

import (
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func TestLookahead_PeriodicInCoverage(t *testing.T) {
	p := periodicTestProfile(t, 100, 20, 10)
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	now := testEpoch.Add(10 * time.Second) // mid-window
	st := ev.Lookahead(now)
	if !st.InCoverage {
		t.Fatal("expected in coverage")
	}
	if st.ConfiguredLookaheadSec != 10 {
		t.Errorf("ConfiguredLookaheadSec = %v, want 10", st.ConfiguredLookaheadSec)
	}
	if st.NextWindowDurationSec == nil || *st.NextWindowDurationSec != 20 {
		t.Fatalf("NextWindowDurationSec = %v, want 20", st.NextWindowDurationSec)
	}
	if st.NextOpenAt == nil || !st.NextOpenAt.Equal(testEpoch) {
		t.Errorf("NextOpenAt = %v, want %v", st.NextOpenAt, testEpoch)
	}
	wantClose := testEpoch.Add(20 * time.Second)
	if st.NextCloseAt == nil || !st.NextCloseAt.Equal(wantClose) {
		t.Errorf("NextCloseAt = %v, want %v", st.NextCloseAt, wantClose)
	}
	if st.MaxElevationDeg != nil {
		t.Errorf("MaxElevationDeg should be nil for periodic")
	}
}

func TestLookahead_PeriodicOutOfCoverage(t *testing.T) {
	p := periodicTestProfile(t, 100, 20, 10)
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	now := testEpoch.Add(50 * time.Second) // mid-gap, until open = 50
	st := ev.Lookahead(now)
	if st.InCoverage {
		t.Fatal("expected out of coverage")
	}
	wantOpen := now.Add(50 * time.Second)
	wantClose := wantOpen.Add(20 * time.Second)
	if st.NextOpenAt == nil || !st.NextOpenAt.Equal(wantOpen) {
		t.Errorf("NextOpenAt = %v, want %v", st.NextOpenAt, wantOpen)
	}
	if st.NextCloseAt == nil || !st.NextCloseAt.Equal(wantClose) {
		t.Errorf("NextCloseAt = %v, want %v", st.NextCloseAt, wantClose)
	}
}

func TestLookahead_PeriodicMidWindowBlockage(t *testing.T) {
	// period=100, window=50, blockage [20,30). During the blockage the
	// window's scheduled close (t=50) must not shift; only NextOpenAt
	// reflects when coverage resumes (blockage clear, t=30).
	p := periodicTestProfile(t, 100, 50, 10)
	p.Blockages = []profile.Blockage{{OffsetSec: 20, DurationSec: 10}}
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	// Mid-window blockage at t=25.
	st := ev.Lookahead(testEpoch.Add(25 * time.Second))
	if st.InCoverage {
		t.Fatal("expected out of coverage during blockage")
	}
	wantOpen := testEpoch.Add(30 * time.Second)  // blockage clears
	wantClose := testEpoch.Add(50 * time.Second) // scheduled window close
	if st.NextOpenAt == nil || !st.NextOpenAt.Equal(wantOpen) {
		t.Errorf("NextOpenAt = %v, want %v", st.NextOpenAt, wantOpen)
	}
	if st.NextCloseAt == nil || !st.NextCloseAt.Equal(wantClose) {
		t.Errorf("NextCloseAt = %v, want %v (must not shift to open+window)", st.NextCloseAt, wantClose)
	}
	if st.NextWindowDurationSec == nil || *st.NextWindowDurationSec != 50 {
		t.Errorf("NextWindowDurationSec = %v, want 50", st.NextWindowDurationSec)
	}

	// A scheduled inter-window gap on the same profile still predicts the
	// next full window correctly (t=70 → open t=100, close t=150).
	gap := ev.Lookahead(testEpoch.Add(70 * time.Second))
	if gap.InCoverage {
		t.Fatal("expected out of coverage in the scheduled gap")
	}
	wantOpen2 := testEpoch.Add(100 * time.Second)
	wantClose2 := testEpoch.Add(150 * time.Second)
	if gap.NextOpenAt == nil || !gap.NextOpenAt.Equal(wantOpen2) {
		t.Errorf("gap NextOpenAt = %v, want %v", gap.NextOpenAt, wantOpen2)
	}
	if gap.NextCloseAt == nil || !gap.NextCloseAt.Equal(wantClose2) {
		t.Errorf("gap NextCloseAt = %v, want %v", gap.NextCloseAt, wantClose2)
	}
}

func TestLookahead_PeriodicBlockagePastWindowClose(t *testing.T) {
	// period=100, window=50, blockage [40,60) extends into the scheduled gap.
	// During the blockage there is no remaining coverage this window —
	// Lookahead must predict the next scheduled window, not open+WindowSec
	// from the blockage-clear instant.
	p := periodicTestProfile(t, 100, 50, 10)
	p.Blockages = []profile.Blockage{{OffsetSec: 40, DurationSec: 20}}
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	st := ev.Lookahead(testEpoch.Add(45 * time.Second))
	if st.InCoverage {
		t.Fatal("expected out of coverage during blockage")
	}
	wantOpen := testEpoch.Add(100 * time.Second)
	wantClose := testEpoch.Add(150 * time.Second)
	if st.NextOpenAt == nil || !st.NextOpenAt.Equal(wantOpen) {
		t.Errorf("NextOpenAt = %v, want %v", st.NextOpenAt, wantOpen)
	}
	if st.NextCloseAt == nil || !st.NextCloseAt.Equal(wantClose) {
		t.Errorf("NextCloseAt = %v, want %v", st.NextCloseAt, wantClose)
	}
}

func TestLookahead_ContinuousOmitsTimes(t *testing.T) {
	p := continuousTestProfile(t, 60)
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	st := ev.Lookahead(testEpoch.Add(10 * time.Second))
	if !st.InCoverage {
		t.Fatal("expected in coverage")
	}
	if st.NextOpenAt != nil || st.NextCloseAt != nil || st.NextWindowDurationSec != nil {
		t.Errorf("continuous should omit open/close/duration, got %+v", st)
	}
}
