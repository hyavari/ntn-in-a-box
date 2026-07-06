package profile

import (
	"math"
	"strings"
	"testing"
)

// validPeriodic returns a minimal, valid periodic profile that tests can
// mutate to exercise one validation rule at a time.
func validPeriodic() Profile {
	return Profile{
		Name: "test",
		Schedule: Schedule{
			Mode:         ModePeriodic,
			PeriodSec:    100,
			WindowSec:    20,
			LookaheadSec: 10,
		},
		Curves: Curves{
			DelayMs:       []Point{{0, 1}, {20, 2}},
			JitterMs:      []Point{{0, 1}, {20, 2}},
			LossPct:       []Point{{0, 1}, {20, 2}},
			BandwidthKbps: []Point{{0, 1}, {20, 2}},
		},
	}
}

func TestValidate_ValidPeriodicProfile(t *testing.T) {
	p := validPeriodic()
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid profile to pass, got: %v", err)
	}
}

func TestValidate_ValidContinuousProfile(t *testing.T) {
	p := Profile{
		Name: "test",
		Schedule: Schedule{
			Mode:      ModeContinuous,
			PeriodSec: 60,
		},
		Curves: Curves{
			DelayMs:       []Point{{0, 1}},
			JitterMs:      []Point{{0, 1}},
			LossPct:       []Point{{0, 1}},
			BandwidthKbps: []Point{{0, 1}},
		},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid profile to pass, got: %v", err)
	}
}

func TestValidate_RejectsInvalidProfiles(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Profile)
	}{
		{
			name:   "missing name",
			modify: func(p *Profile) { p.Name = "" },
		},
		{
			name:   "missing schedule mode",
			modify: func(p *Profile) { p.Schedule.Mode = "" },
		},
		{
			name:   "unknown schedule mode",
			modify: func(p *Profile) { p.Schedule.Mode = "bogus" },
		},
		{
			name:   "non-positive period_sec",
			modify: func(p *Profile) { p.Schedule.PeriodSec = 0 },
		},
		{
			name:   "non-positive window_sec (periodic)",
			modify: func(p *Profile) { p.Schedule.WindowSec = 0 },
		},
		{
			name:   "window_sec exceeds period_sec",
			modify: func(p *Profile) { p.Schedule.WindowSec = 200 },
		},
		{
			name:   "negative lookahead_sec",
			modify: func(p *Profile) { p.Schedule.LookaheadSec = -1 },
		},
		{
			name:   "lookahead_sec exceeds the out-of-coverage gap",
			modify: func(p *Profile) { p.Schedule.LookaheadSec = 81 }, // gap = 100-20 = 80
		},
		{
			name:   "empty curve",
			modify: func(p *Profile) { p.Curves.DelayMs = nil },
		},
		{
			name:   "curve not starting at offset 0",
			modify: func(p *Profile) { p.Curves.DelayMs = []Point{{5, 1}, {20, 2}} },
		},
		{
			name:   "curve not strictly ascending",
			modify: func(p *Profile) { p.Curves.DelayMs = []Point{{0, 1}, {5, 2}, {5, 3}} },
		},
		{
			name:   "curve offset exceeds window_sec",
			modify: func(p *Profile) { p.Curves.DelayMs = []Point{{0, 1}, {200, 2}} },
		},
		{
			name:   "negative curve value",
			modify: func(p *Profile) { p.Curves.DelayMs = []Point{{0, -1}, {20, 2}} },
		},
		{
			name:   "loss_pct over 100",
			modify: func(p *Profile) { p.Curves.LossPct = []Point{{0, 1}, {20, 101}} },
		},
		{
			name:   "NaN curve value",
			modify: func(p *Profile) { p.Curves.DelayMs = []Point{{0, 1}, {20, math.NaN()}} },
		},
		{
			name:   "NaN curve offset",
			modify: func(p *Profile) { p.Curves.DelayMs = []Point{{0, 1}, {math.NaN(), 2}} },
		},
		{
			name:   "curve tail does not reach window_sec boundary",
			modify: func(p *Profile) { p.Curves.DelayMs = []Point{{0, 1}, {10, 2}} }, // window_sec is 20
		},
		{
			name: "negative lookahead_sec in continuous mode is still rejected",
			modify: func(p *Profile) {
				p.Schedule.Mode = ModeContinuous
				p.Schedule.LookaheadSec = -5
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validPeriodic()
			tt.modify(&p)
			if err := p.Validate(); err == nil {
				t.Fatalf("expected an error for case %q, got nil", tt.name)
			}
		})
	}
}

func TestValidate_ContinuousModeIgnoresWindowSec(t *testing.T) {
	// window_sec is meaningless for continuous mode and must not be
	// validated as if it were a periodic profile's window.
	p := Profile{
		Name: "test",
		Schedule: Schedule{
			Mode:      ModeContinuous,
			PeriodSec: 60,
			WindowSec: 999999, // would be invalid for periodic mode; irrelevant here
		},
		Curves: Curves{
			DelayMs:       []Point{{0, 1}},
			JitterMs:      []Point{{0, 1}},
			LossPct:       []Point{{0, 1}},
			BandwidthKbps: []Point{{0, 1}},
		},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected window_sec to be ignored for continuous mode, got: %v", err)
	}
}

func TestValidate_SingleCurvePointIsConstant(t *testing.T) {
	// A single point (offset 0) represents a value that holds constant
	// across the whole range, regardless of how long that range is.
	p := Profile{
		Name: "test",
		Schedule: Schedule{
			Mode:      ModeContinuous,
			PeriodSec: 3600,
		},
		Curves: Curves{
			DelayMs:       []Point{{0, 600}},
			JitterMs:      []Point{{0, 10}},
			LossPct:       []Point{{0, 0.5}},
			BandwidthKbps: []Point{{0, 5000}},
		},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("expected single-point constant curve to be valid, got: %v", err)
	}
}

func TestValidate_MultiPointCurveMustReachBoundary(t *testing.T) {
	p := validPeriodic() // window_sec: 20
	p.Curves.DelayMs = []Point{{0, 1}, {5, 2}, {12, 3}}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected an error for a multi-point curve stopping short of window_sec, got nil")
	}
	if !strings.Contains(err.Error(), "last point must be at offset_sec 20.00") {
		t.Errorf("expected error to mention the required boundary, got: %v", err)
	}
}

func TestValidate_AggregatesMultipleErrors(t *testing.T) {
	// Two independent problems in the same profile: both should show
	// up in the returned error, proving errors.Join aggregates rather
	// than the validator stopping at the first failure.
	p := validPeriodic()
	p.Name = ""
	p.Schedule.PeriodSec = 0

	err := p.Validate()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "name is required") {
		t.Errorf("expected aggregated error to mention missing name, got: %v", msg)
	}
	if !strings.Contains(msg, "period_sec must be > 0") {
		t.Errorf("expected aggregated error to mention invalid period_sec, got: %v", msg)
	}
}
