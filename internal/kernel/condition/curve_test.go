package condition

import (
	"math"
	"testing"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func TestEvaluateCurve_SinglePointIsConstant(t *testing.T) {
	points := []profile.Point{{OffsetSec: 0, Value: 42}}
	for _, offset := range []float64{0, 5, 1000} {
		if got := evaluateCurve(points, offset); got != 42 {
			t.Errorf("evaluateCurve(%v, %v) = %v, want 42", points, offset, got)
		}
	}
}

func TestEvaluateCurve_LinearInterpolation(t *testing.T) {
	points := []profile.Point{
		{OffsetSec: 0, Value: 100},
		{OffsetSec: 10, Value: 200},
	}
	tests := []struct {
		offset float64
		want   float64
	}{
		{0, 100},
		{5, 150}, // midpoint
		{10, 200},
		{2.5, 125},
	}
	for _, tt := range tests {
		if got := evaluateCurve(points, tt.offset); got != tt.want {
			t.Errorf("evaluateCurve at offset %v = %v, want %v", tt.offset, got, tt.want)
		}
	}
}

func TestEvaluateCurve_MultiSegment(t *testing.T) {
	// Mirrors the shape of testdata/profiles/leo_pass_90s.yaml's
	// delay_ms curve: ramp down, steady, ramp up.
	points := []profile.Point{
		{OffsetSec: 0, Value: 150},
		{OffsetSec: 15, Value: 40},
		{OffsetSec: 75, Value: 40},
		{OffsetSec: 90, Value: 100},
	}
	tests := []struct {
		offset float64
		want   float64
	}{
		{0, 150},
		{7.5, 95},  // midpoint of first ramp: 150 -> 40
		{15, 40},   // start of steady segment
		{45, 40},   // middle of steady segment (flat)
		{75, 40},   // end of steady segment
		{82.5, 70}, // midpoint of final ramp: 40 -> 100
		{90, 100},
	}
	for _, tt := range tests {
		if got := evaluateCurve(points, tt.offset); got != tt.want {
			t.Errorf("evaluateCurve at offset %v = %v, want %v", tt.offset, got, tt.want)
		}
	}
}

func TestEvaluateCurve_ClampsOutsideRange(t *testing.T) {
	points := []profile.Point{
		{OffsetSec: 10, Value: 1}, // note: a validated profile always starts at 0,
		{OffsetSec: 20, Value: 2}, // this just exercises the clamping logic directly
	}
	if got := evaluateCurve(points, -5); got != 1 {
		t.Errorf("offset before first point: got %v, want 1 (clamped)", got)
	}
	if got := evaluateCurve(points, 1000); got != 2 {
		t.Errorf("offset after last point: got %v, want 2 (clamped)", got)
	}
}

func TestEvaluateCurve_EmptyIsNaN(t *testing.T) {
	got := evaluateCurve(nil, 5)
	if !math.IsNaN(got) {
		t.Errorf("evaluateCurve(nil, 5) = %v, want NaN", got)
	}
}
