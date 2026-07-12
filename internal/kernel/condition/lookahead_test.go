package condition

import (
	"testing"
	"time"
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
