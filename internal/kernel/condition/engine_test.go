package condition

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func TestNewEvaluator_RejectsInvalidProfile(t *testing.T) {
	_, err := NewEvaluator(profile.Profile{}, testEpoch) // missing everything
	if err == nil {
		t.Fatal("NewEvaluator with an invalid profile: expected an error, got nil")
	}
}

func TestEvaluate_OutOfCoverageIsNaN(t *testing.T) {
	p := periodicTestProfile(t, 100, 20, 10)
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	state, cov := ev.Evaluate(testEpoch.Add(50 * time.Second)) // mid-gap
	if cov.InCoverage {
		t.Fatal("expected mid-gap instant to be out of coverage")
	}
	for name, v := range map[string]float64{
		"DelayMs":       state.DelayMs,
		"JitterMs":      state.JitterMs,
		"LossPct":       state.LossPct,
		"BandwidthKbps": state.BandwidthKbps,
	} {
		if !math.IsNaN(v) {
			t.Errorf("LinkState.%s = %v while out of coverage, want NaN", name, v)
		}
	}
}

func TestEvaluate_InCoverageMatchesCurve(t *testing.T) {
	p := periodicTestProfile(t, 100, 20, 10)
	// Override with a non-flat delay curve so interpolation is actually
	// exercised, not just the constant fixture curve.
	p.Curves.DelayMs = []profile.Point{{OffsetSec: 0, Value: 100}, {OffsetSec: 20, Value: 200}}
	ev, err := NewEvaluator(p, testEpoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	state, cov := ev.Evaluate(testEpoch.Add(10 * time.Second)) // midpoint of window
	if !cov.InCoverage {
		t.Fatal("expected mid-window instant to be in coverage")
	}
	if state.DelayMs != 150 {
		t.Errorf("DelayMs = %v, want 150 (midpoint of 100->200)", state.DelayMs)
	}
}

// TestEvaluate_SampleProfiles is an end-to-end check against the real
// sample profiles in testdata/profiles, confirming the Condition Engine
// produces the coverage/link-state sequence a Dev Sandbox user would
// actually observe.
func TestEvaluate_SampleProfiles(t *testing.T) {
	tests := []struct {
		name           string
		file           string
		offset         time.Duration
		wantInCoverage bool
		wantDelayMs    float64 // ignored if wantInCoverage is false
	}{
		{"leo_pass_90s: window just opened (edge degradation)", "leo_pass_90s.yaml", 0, true, 150},
		{"leo_pass_90s: mid-ramp interpolation (0->15: 150->40)", "leo_pass_90s.yaml", 7500 * time.Millisecond, true, 95},
		{"leo_pass_90s: mid-steady", "leo_pass_90s.yaml", 45 * time.Second, true, 40},
		{"leo_pass_90s: mid-ramp interpolation (75->90: 40->100)", "leo_pass_90s.yaml", 82_500 * time.Millisecond, true, 70},
		{"leo_pass_90s: in the gap", "leo_pass_90s.yaml", 95 * time.Second, false, 0},
		{"geo_steady: always in coverage", "geo_steady.yaml", 12345 * time.Second, true, 600},
		{"d2c_burst: mid-window", "d2c_burst.yaml", 10 * time.Second, true, 200},
		{"d2c_burst: in the gap", "d2c_burst.yaml", 25 * time.Second, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "testdata", "profiles", tt.file)
			p, err := profile.LoadFile(path)
			if err != nil {
				t.Fatalf("LoadFile(%s): %v", path, err)
			}
			ev, err := NewEvaluator(*p, testEpoch)
			if err != nil {
				t.Fatalf("NewEvaluator: %v", err)
			}

			state, cov := ev.Evaluate(testEpoch.Add(tt.offset))
			if cov.InCoverage != tt.wantInCoverage {
				t.Fatalf("InCoverage = %v, want %v", cov.InCoverage, tt.wantInCoverage)
			}
			if !tt.wantInCoverage {
				if !math.IsNaN(state.DelayMs) {
					t.Errorf("DelayMs = %v while out of coverage, want NaN", state.DelayMs)
				}
				return
			}
			if state.DelayMs != tt.wantDelayMs {
				t.Errorf("DelayMs = %v, want %v", state.DelayMs, tt.wantDelayMs)
			}
		})
	}
}
