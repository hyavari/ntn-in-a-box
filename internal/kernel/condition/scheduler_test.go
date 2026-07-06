package condition

import (
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

var testEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func periodicTestProfile(t *testing.T, periodSec, windowSec, lookaheadSec float64) profile.Profile {
	t.Helper()
	flat := []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: windowSec, Value: 1}}
	return profile.Profile{
		Name: "test-periodic",
		Schedule: profile.Schedule{
			Mode:         profile.ModePeriodic,
			PeriodSec:    periodSec,
			WindowSec:    windowSec,
			LookaheadSec: lookaheadSec,
		},
		Curves: profile.Curves{
			DelayMs:       flat,
			JitterMs:      flat,
			LossPct:       flat,
			BandwidthKbps: flat,
		},
	}
}

func continuousTestProfile(t *testing.T, periodSec float64) profile.Profile {
	t.Helper()
	flat := []profile.Point{{OffsetSec: 0, Value: 1}}
	return profile.Profile{
		Name: "test-continuous",
		Schedule: profile.Schedule{
			Mode:      profile.ModeContinuous,
			PeriodSec: periodSec,
		},
		Curves: profile.Curves{
			DelayMs:       flat,
			JitterMs:      flat,
			LossPct:       flat,
			BandwidthKbps: flat,
		},
	}
}

func TestCoverage_Periodic(t *testing.T) {
	p := periodicTestProfile(t, 100, 20, 10)
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	tests := []struct {
		name             string
		offset           time.Duration
		wantInCoverage   bool
		wantElapsedSec   float64
		wantUntilNextSec float64
	}{
		{"at epoch, window just opened", 0, true, 0, 20},
		{"mid-window", 10 * time.Second, true, 10, 10},
		{"just before window closes", 19_999 * time.Millisecond, true, 19.999, 0.001},
		{"exactly at window boundary is out of coverage", 20 * time.Second, false, 0, 80},
		{"mid-gap", 50 * time.Second, false, 30, 50},
		{"next period, window reopened", 100 * time.Second, true, 0, 20},
		{"two periods + into the gap", 220 * time.Second, false, 0, 80},
		{"before epoch wraps to the previous cycle's gap", -10 * time.Second, false, 70, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ev.Coverage(testEpoch.Add(tt.offset))
			if got.InCoverage != tt.wantInCoverage {
				t.Errorf("InCoverage = %v, want %v", got.InCoverage, tt.wantInCoverage)
			}
			if !approxEqual(got.ElapsedSec, tt.wantElapsedSec, 1e-6) {
				t.Errorf("ElapsedSec = %v, want %v", got.ElapsedSec, tt.wantElapsedSec)
			}
			if !approxEqual(got.UntilNextTransitionSec, tt.wantUntilNextSec, 1e-6) {
				t.Errorf("UntilNextTransitionSec = %v, want %v", got.UntilNextTransitionSec, tt.wantUntilNextSec)
			}
		})
	}
}

func TestCoverage_Continuous(t *testing.T) {
	p := continuousTestProfile(t, 60)
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	tests := []struct {
		name             string
		offset           time.Duration
		wantElapsedSec   float64
		wantUntilNextSec float64
	}{
		{"at epoch", 0, 0, 60},
		{"mid-cycle", 30 * time.Second, 30, 30},
		{"cycle wraps exactly", 60 * time.Second, 0, 60},
		{"second cycle, mid-way", 90 * time.Second, 30, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ev.Coverage(testEpoch.Add(tt.offset))
			if !got.InCoverage {
				t.Error("InCoverage = false, want true (continuous mode is always in coverage)")
			}
			if !approxEqual(got.ElapsedSec, tt.wantElapsedSec, 1e-6) {
				t.Errorf("ElapsedSec = %v, want %v", got.ElapsedSec, tt.wantElapsedSec)
			}
			if !approxEqual(got.UntilNextTransitionSec, tt.wantUntilNextSec, 1e-6) {
				t.Errorf("UntilNextTransitionSec = %v, want %v", got.UntilNextTransitionSec, tt.wantUntilNextSec)
			}
		})
	}
}

func approxEqual(a, b, epsilon float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= epsilon
}
