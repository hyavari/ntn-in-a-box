package condition

import (
	"math"
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
				t.Error("InCoverage = false, want true (continuous profile with no blockages)")
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

func continuousBlockageProfile(t *testing.T, periodSec float64, blocks []profile.Blockage) profile.Profile {
	t.Helper()
	p := continuousTestProfile(t, periodSec)
	p.Name = "test-blockage"
	p.Blockages = blocks
	return p
}

func TestCoverage_Blockage(t *testing.T) {
	// Continuous base (always scheduled-in-coverage) with two repeating
	// blockages per 300s cycle: [60, 68) and [180, 200).
	p := continuousBlockageProfile(t, 300, []profile.Blockage{
		{OffsetSec: 60, DurationSec: 8},
		{OffsetSec: 180, DurationSec: 20},
	})
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
		// In coverage: UntilNextTransitionSec reports the full remaining
		// cycle and is deliberately NOT shortened to reveal the upcoming
		// blockage (a surprise drop must stay unforeseeable).
		{"at epoch, in coverage", 0, true, 0, 300},
		{"just before first blockage", 59 * time.Second, true, 59, 241},
		{"first blockage starts (half-open, blocked)", 60 * time.Second, false, 0, 8},
		{"mid first blockage", 64 * time.Second, false, 4, 4},
		{"just before first blockage clears", 67999 * time.Millisecond, false, 7.999, 0.001},
		{"first blockage clears (back in coverage)", 68 * time.Second, true, 68, 232},
		{"between blockages", 120 * time.Second, true, 120, 180},
		{"second blockage starts", 180 * time.Second, false, 0, 20},
		{"just before second clears", 199999 * time.Millisecond, false, 19.999, 0.001},
		{"second blockage clears", 200 * time.Second, true, 200, 100},
		// Blockages repeat every cycle.
		{"first blockage in the second cycle", 360 * time.Second, false, 0, 8},
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
			// CyclePosSec always tracks the schedule period, even during a blockage
			// (unlike ElapsedSec, which is blockage-relative while blocked).
			wantCycle := positiveMod(tt.offset.Seconds(), 300)
			if !approxEqual(got.CyclePosSec, wantCycle, 1e-6) {
				t.Errorf("CyclePosSec = %v, want %v", got.CyclePosSec, wantCycle)
			}
			// Continuous profile: every outage is a blockage.
			if got.InBlockage != !tt.wantInCoverage {
				t.Errorf("InBlockage = %v, want %v", got.InBlockage, !tt.wantInCoverage)
			}
		})
	}
}

func TestEvaluate_BlockageYieldsNaNLinkState(t *testing.T) {
	p := continuousBlockageProfile(t, 300, []profile.Blockage{{OffsetSec: 60, DurationSec: 8}})
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	link, cov := ev.Evaluate(testEpoch.Add(64 * time.Second))
	if cov.InCoverage {
		t.Fatal("expected out of coverage during blockage")
	}
	if !math.IsNaN(link.DelayMs) || !math.IsNaN(link.LossPct) {
		t.Errorf("expected NaN link state during blockage, got %+v", link)
	}
}

func TestCoverage_BlockageIsDeterministic(t *testing.T) {
	// Evaluating the same instant twice must return identical state:
	// SSE/TUI/recorder rely on Coverage being a pure function of time.
	p := continuousBlockageProfile(t, 300, []profile.Blockage{{OffsetSec: 60, DurationSec: 8}})
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	at := testEpoch.Add(63 * time.Second)
	a := ev.Coverage(at)
	b := ev.Coverage(at)
	if a != b {
		t.Errorf("Coverage not deterministic: %+v vs %+v", a, b)
	}
}

func TestCoverage_PeriodicBlockage(t *testing.T) {
	// period=100, window=50, blockage [20,30) only bites while the window
	// would otherwise be open. A blockage that falls entirely in the gap
	// is a no-op (already out of coverage).
	p := periodicTestProfile(t, 100, 50, 10)
	p.Blockages = []profile.Blockage{
		{OffsetSec: 20, DurationSec: 10},
		{OffsetSec: 70, DurationSec: 5}, // inside the scheduled gap — ignored
	}
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
		{"before mid-window blockage", 19 * time.Second, true, 19, 31}, // until scheduled close, not blockage
		{"mid-window blockage", 25 * time.Second, false, 5, 5},
		{"blockage clears, still in window", 30 * time.Second, true, 30, 20},
		{"scheduled gap (gap blockage ignored)", 72 * time.Second, false, 22, 28},
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

func approxEqual(a, b, epsilon float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= epsilon
}
