package tle

import (
	"math"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
	"gopkg.in/yaml.v3"
)

// TestIntegration_EndToEnd exercises the full TLE pipeline:
// parse → predict → generate → validate → SequenceEvaluator → evaluate.
func TestIntegration_EndToEnd(t *testing.T) {
	// Step 1: Parse TLE file.
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(sats) != 1 {
		t.Fatalf("expected 1 satellite, got %d", len(sats))
	}
	sat := sats[0]
	t.Logf("Satellite: %s (NORAD %d)", sat.Name, sat.NoradID)

	// Step 2: Predict passes.
	obs := Observer{LatDeg: 37.7749, LonDeg: -122.4194, AltKm: 0}
	startTime := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)

	passes, err := PredictPasses(sat, obs, startTime, PredictConfig{
		MinElevDeg:     10,
		Count:          3,
		MaxSearch:      48 * time.Hour,
		SampleInterval: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("PredictPasses: %v", err)
	}
	if len(passes) == 0 {
		t.Fatal("no passes predicted")
	}
	t.Logf("Predicted %d passes", len(passes))
	for i, p := range passes {
		t.Logf("  Pass %d: rise=%s duration=%s maxElev=%.1f°",
			i+1, p.Rise.Format(time.RFC3339), p.Duration.Round(time.Second), p.MaxElevDeg)
	}

	// Step 3: Generate profiles and validate.
	model := DefaultLinkModel()
	for i, pass := range passes {
		var gapSec float64
		if i+1 < len(passes) {
			gapSec = passes[i+1].Rise.Sub(pass.Set).Seconds()
		} else {
			gapSec = 60 // Synthetic gap for last pass
		}

		p, err := GenerateProfile(pass, model, GenerateOpts{
			LookaheadSec: 30,
			GapSec:       gapSec,
			Index:        i,
			SatName:      sat.Name,
		})
		if err != nil {
			t.Fatalf("GenerateProfile pass %d: %v", i+1, err)
		}
		if err := p.Validate(); err != nil {
			t.Fatalf("profile validation pass %d: %v", i+1, err)
		}

		// Profile validation is covered; full YAML round-trip is tested
		// separately in TestIntegration_GeneratedProfileLoadable.
	}

	// Step 4: Create SequenceEvaluator.
	seqEval, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:        1.0,
		StartAt:      passes[0].Rise,
		LookaheadSec: 30,
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	// Verify it satisfies condition.Eval.
	var _ condition.Eval = seqEval

	// Step 5: Evaluate at various points.
	wallStart := time.Now()

	// At pass 1 rise (first call).
	seqEval.Advance(wallStart)
	link, cov := seqEval.Evaluate(wallStart)
	if !cov.InCoverage {
		t.Error("expected in-coverage at first pass rise")
	}
	if math.IsNaN(link.DelayMs) {
		t.Error("DelayMs is NaN during coverage")
	}
	if link.DelayMs < 0 || link.DelayMs > 200 {
		t.Errorf("DelayMs = %f, expected 0-200", link.DelayMs)
	}
	if link.BandwidthKbps <= 0 {
		t.Errorf("BandwidthKbps = %f, expected > 0", link.BandwidthKbps)
	}
	t.Logf("At pass start: delay=%.1fms jitter=%.1fms loss=%.1f%% bw=%.0fkbps",
		link.DelayMs, link.JitterMs, link.LossPct, link.BandwidthKbps)

	// Mid-pass: should still be in coverage with potentially different values.
	midWall := wallStart.Add(passes[0].Duration / 2)
	seqEval.Advance(midWall)
	link2, cov2 := seqEval.Evaluate(time.Time{})
	if !cov2.InCoverage {
		t.Error("expected in-coverage at mid-pass")
	}
	t.Logf("At mid-pass: delay=%.1fms jitter=%.1fms loss=%.1f%% bw=%.0fkbps",
		link2.DelayMs, link2.JitterMs, link2.LossPct, link2.BandwidthKbps)

	// After pass 1: should be out of coverage.
	afterPassWall := wallStart.Add(passes[0].Duration + 10*time.Second)
	seqEval.Advance(afterPassWall)
	link3, cov3 := seqEval.Evaluate(time.Time{})
	if cov3.InCoverage {
		t.Error("expected out-of-coverage after pass ends")
	}
	if !math.IsNaN(link3.DelayMs) {
		t.Errorf("DelayMs = %f, expected NaN during gap", link3.DelayMs)
	}
	t.Logf("After pass: inCoverage=%v untilNext=%.0fs", cov3.InCoverage, cov3.UntilNextTransitionSec)
}

// TestIntegration_GeneratedProfileLoadable verifies that a generated
// profile can be loaded by the standard profile.LoadBytes function.
func TestIntegration_GeneratedProfileLoadable(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	obs := Observer{LatDeg: 51.5074, LonDeg: -0.1278} // London
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
		t.Skip("no passes found for London")
	}

	model := DefaultLinkModel()
	p, err := GenerateProfile(passes[0], model, GenerateOpts{
		LookaheadSec: 30,
		GapSec:       600,
		Index:        0,
		SatName:      "ISS",
	})
	if err != nil {
		t.Fatalf("GenerateProfile: %v", err)
	}

	// Marshal to YAML and reload via profile.LoadBytes (full round-trip).
	data, err := yaml.Marshal(p)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}

	loaded, err := profile.LoadBytes(data)
	if err != nil {
		t.Fatalf("profile.LoadBytes round-trip failed: %v", err)
	}

	// Verify key properties survived.
	if loaded.Name == "" {
		t.Error("profile name is empty after round-trip")
	}
	if loaded.Schedule.WindowSec <= 0 {
		t.Error("window_sec is 0 after round-trip")
	}
	if len(loaded.Curves.DelayMs) == 0 {
		t.Error("delay_ms curve is empty after round-trip")
	}
	if loaded.Schedule.PeriodSec != p.Schedule.PeriodSec {
		t.Errorf("PeriodSec = %f, want %f", loaded.Schedule.PeriodSec, p.Schedule.PeriodSec)
	}
	if len(loaded.Curves.DelayMs) != len(p.Curves.DelayMs) {
		t.Errorf("DelayMs point count = %d, want %d", len(loaded.Curves.DelayMs), len(p.Curves.DelayMs))
	}
	t.Logf("Round-trip OK: %s (window=%.0fs, %d curve points)",
		loaded.Name, loaded.Schedule.WindowSec, len(loaded.Curves.DelayMs))
}

// TestIntegration_Starlink verifies the pipeline works with Starlink TLE data.
func TestIntegration_Starlink(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/starlink-single.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if sats[0].Name != "STARLINK-1007" {
		t.Errorf("Name = %q, want STARLINK-1007", sats[0].Name)
	}

	obs := Observer{LatDeg: 40.7128, LonDeg: -74.0060} // New York
	startTime := time.Date(2024, 4, 9, 0, 0, 0, 0, time.UTC)

	passes, err := PredictPasses(sats[0], obs, startTime, PredictConfig{
		MinElevDeg: 10,
		Count:      5,
		MaxSearch:  48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("PredictPasses: %v", err)
	}

	t.Logf("Starlink passes from NYC: %d found", len(passes))
	for i, p := range passes {
		t.Logf("  Pass %d: rise=%s duration=%s maxElev=%.1f°",
			i+1, p.Rise.Format(time.RFC3339), p.Duration.Round(time.Second), p.MaxElevDeg)
	}

	if len(passes) == 0 {
		t.Skip("no Starlink passes found")
	}

	// Build a SequenceEvaluator with speed acceleration.
	model := DefaultLinkModel()
	seqEval, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:        5.0, // 5x gap acceleration
		StartAt:      passes[0].Rise,
		LookaheadSec: 30,
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	// Verify Passes() returns the right count.
	if got := len(seqEval.Passes()); got != len(passes) {
		t.Errorf("Passes() = %d, want %d", got, len(passes))
	}
}
