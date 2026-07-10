package tle

import (
	"testing"
	"time"
)

func TestGenerateProfile_Synthetic(t *testing.T) {
	// Create a synthetic pass with known elevation samples.
	rise := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)
	samples := []ElevSample{
		{T: rise, ElevDeg: 10, RangeKm: 2000},
		{T: rise.Add(60 * time.Second), ElevDeg: 30, RangeKm: 1200},
		{T: rise.Add(120 * time.Second), ElevDeg: 60, RangeKm: 800},
		{T: rise.Add(180 * time.Second), ElevDeg: 45, RangeKm: 1000},
		{T: rise.Add(240 * time.Second), ElevDeg: 20, RangeKm: 1500},
		{T: rise.Add(300 * time.Second), ElevDeg: 10, RangeKm: 2000},
	}

	pass := Pass{
		Satellite:   "TEST-SAT",
		NoradID:     99999,
		Rise:        rise,
		Set:         rise.Add(300 * time.Second),
		Duration:    300 * time.Second,
		MaxElevDeg:  60,
		MaxElevTime: rise.Add(120 * time.Second),
		Samples:     samples,
	}

	model := DefaultLinkModel()

	p, err := GenerateProfile(pass, model, GenerateOpts{
		LookaheadSec: 30,
		GapSec:       600,
		Index:        0,
		SatName:      "TEST-SAT",
	})
	if err != nil {
		t.Fatalf("GenerateProfile: %v", err)
	}

	// Check name.
	if p.Name != "tle_test_sat_pass1" {
		t.Errorf("Name = %q, want tle_test_sat_pass1", p.Name)
	}

	// Check schedule.
	if p.Schedule.Mode != "periodic" {
		t.Errorf("Mode = %q, want periodic", p.Schedule.Mode)
	}
	if p.Schedule.WindowSec != 300 {
		t.Errorf("WindowSec = %f, want 300", p.Schedule.WindowSec)
	}
	if p.Schedule.PeriodSec != 900 { // 300 + 600 gap
		t.Errorf("PeriodSec = %f, want 900", p.Schedule.PeriodSec)
	}
	if p.Schedule.LookaheadSec != 30 {
		t.Errorf("LookaheadSec = %f, want 30", p.Schedule.LookaheadSec)
	}

	// Check curve point counts.
	if len(p.Curves.DelayMs) != 6 {
		t.Errorf("DelayMs points = %d, want 6", len(p.Curves.DelayMs))
	}
	if len(p.Curves.JitterMs) != 6 {
		t.Errorf("JitterMs points = %d, want 6", len(p.Curves.JitterMs))
	}
	if len(p.Curves.LossPct) != 6 {
		t.Errorf("LossPct points = %d, want 6", len(p.Curves.LossPct))
	}
	if len(p.Curves.BandwidthKbps) != 6 {
		t.Errorf("BandwidthKbps points = %d, want 6", len(p.Curves.BandwidthKbps))
	}

	// Check offsets are monotonically increasing.
	for i := 1; i < len(p.Curves.DelayMs); i++ {
		if p.Curves.DelayMs[i].OffsetSec <= p.Curves.DelayMs[i-1].OffsetSec {
			t.Errorf("DelayMs offsets not increasing at index %d", i)
		}
	}

	// First point offset should be 0.
	if p.Curves.DelayMs[0].OffsetSec != 0 {
		t.Errorf("first offset = %f, want 0", p.Curves.DelayMs[0].OffsetSec)
	}

	// Last point offset should be 300.
	if p.Curves.DelayMs[5].OffsetSec != 300 {
		t.Errorf("last offset = %f, want 300", p.Curves.DelayMs[5].OffsetSec)
	}

	// Values at first point (10° elevation): delay=150, bw=2000.
	if p.Curves.DelayMs[0].Value != 150 {
		t.Errorf("delay@0s = %f, want 150", p.Curves.DelayMs[0].Value)
	}
	if p.Curves.BandwidthKbps[0].Value != 2000 {
		t.Errorf("bw@0s = %f, want 2000", p.Curves.BandwidthKbps[0].Value)
	}

	// Values at peak (60° elevation): between 45° and 90° model points.
	// Delay: 40 + (60-45)/(90-45) * (25-40) = 40 + 0.333*(-15) = 35
	peakDelay := p.Curves.DelayMs[2].Value
	if peakDelay < 30 || peakDelay > 40 {
		t.Errorf("delay at peak = %f, expected ~35", peakDelay)
	}
}

func TestGenerateProfile_TooFewSamples(t *testing.T) {
	pass := Pass{
		Rise:     time.Now(),
		Set:      time.Now().Add(10 * time.Second),
		Duration: 10 * time.Second,
		Samples:  []ElevSample{{T: time.Now(), ElevDeg: 20, RangeKm: 1000}},
	}
	model := DefaultLinkModel()

	_, err := GenerateProfile(pass, model, GenerateOpts{SatName: "X"})
	if err == nil {
		t.Error("expected error for single-sample pass")
	}
}

func TestGenerateProfile_FromRealPass(t *testing.T) {
	// Use the real ISS predictor to generate a profile.
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	obs := Observer{LatDeg: 37.7749, LonDeg: -122.4194}
	startTime := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)

	passes, err := PredictPasses(sats[0], obs, startTime, PredictConfig{
		MinElevDeg: 5,
		Count:      1,
		MaxSearch:  48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("PredictPasses: %v", err)
	}
	if len(passes) == 0 {
		t.Skip("no passes found")
	}

	model := DefaultLinkModel()
	p, err := GenerateProfile(passes[0], model, GenerateOpts{
		LookaheadSec: 30,
		GapSec:       500,
		Index:        0,
		SatName:      sats[0].Name,
	})
	if err != nil {
		t.Fatalf("GenerateProfile: %v", err)
	}

	t.Logf("Generated profile: %s (window=%.0fs, period=%.0fs, %d curve points)",
		p.Name, p.Schedule.WindowSec, p.Schedule.PeriodSec, len(p.Curves.DelayMs))

	// Verify it validates.
	if err := p.Validate(); err != nil {
		t.Errorf("generated profile validation: %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ISS (ZARYA)", "iss_zarya"},
		{"STARLINK-1007", "starlink_1007"},
		{"", "unknown"},
		{"!!!!", "unknown"},
		{"My Sat 123", "my_sat_123"},
	}
	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
