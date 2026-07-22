package tle

import (
	"testing"
	"time"
)

func TestPredictPasses_ISS(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Observer: San Francisco.
	obs := Observer{
		LatDeg: 37.7749,
		LonDeg: -122.4194,
		AltKm:  0,
	}

	// Use a time near the TLE epoch for best accuracy.
	// TLE epoch is 24100.54321 → 2024, day 100.54321
	// Day 100 of 2024 = April 9, 2024.
	startTime := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)

	cfg := PredictConfig{
		MinElevDeg:     10,
		Count:          3,
		MaxSearch:      48 * time.Hour,
		SampleInterval: 5 * time.Second,
	}

	passes, err := PredictPasses(sats[0], obs, startTime, cfg)
	if err != nil {
		t.Fatalf("PredictPasses: %v", err)
	}

	if len(passes) == 0 {
		t.Fatal("expected at least one pass, got 0")
	}

	for i, pass := range passes {
		t.Logf("Pass %d: rise=%s set=%s duration=%s maxElev=%.1f°",
			i+1, pass.Rise.Format(time.RFC3339), pass.Set.Format(time.RFC3339),
			pass.Duration, pass.MaxElevDeg)

		// Basic sanity checks.
		if pass.Duration < 10*time.Second {
			t.Errorf("pass %d: duration too short: %s", i+1, pass.Duration)
		}
		if pass.Duration > 15*time.Minute {
			t.Errorf("pass %d: duration too long: %s (ISS passes are typically <10min)", i+1, pass.Duration)
		}
		if pass.MaxElevDeg < cfg.MinElevDeg {
			t.Errorf("pass %d: max elevation %.1f° < min %.1f°", i+1, pass.MaxElevDeg, cfg.MinElevDeg)
		}
		if pass.MaxElevDeg > 90 {
			t.Errorf("pass %d: max elevation %.1f° > 90°", i+1, pass.MaxElevDeg)
		}
		if !pass.Rise.Before(pass.Set) {
			t.Errorf("pass %d: rise %s not before set %s", i+1, pass.Rise, pass.Set)
		}
		if !pass.MaxElevTime.After(pass.Rise) || !pass.MaxElevTime.Before(pass.Set) {
			t.Errorf("pass %d: maxElevTime %s not within pass window", i+1, pass.MaxElevTime)
		}
		if len(pass.Samples) < 2 {
			t.Errorf("pass %d: too few samples: %d", i+1, len(pass.Samples))
		}

		// Verify samples are time-ordered.
		for j := 1; j < len(pass.Samples); j++ {
			if !pass.Samples[j].T.After(pass.Samples[j-1].T) {
				t.Errorf("pass %d: sample %d not after sample %d", i+1, j, j-1)
			}
		}
	}
}

func TestPredictPasses_NoVisible(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Use an extremely high minimum elevation that won't be reached.
	obs := Observer{LatDeg: 37.7749, LonDeg: -122.4194}
	startTime := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)

	cfg := PredictConfig{
		MinElevDeg: 89, // Near-zenith only - unlikely for most passes
		Count:      1,
		MaxSearch:  2 * time.Hour, // Short search window
	}

	passes, err := PredictPasses(sats[0], obs, startTime, cfg)
	if err != nil {
		t.Fatalf("PredictPasses: %v", err)
	}

	// With 89° min elevation and 2h window, likely no passes.
	// (Not guaranteed, but very likely for ISS)
	t.Logf("passes found with 89° min elev, 2h window: %d", len(passes))
}

func TestPredictPasses_SampleInterval(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	obs := Observer{LatDeg: 37.7749, LonDeg: -122.4194}
	startTime := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)

	cfg := PredictConfig{
		MinElevDeg:     5, // Low elevation to find passes more easily
		Count:          1,
		MaxSearch:      48 * time.Hour,
		SampleInterval: 2 * time.Second, // Finer sampling
	}

	passes, err := PredictPasses(sats[0], obs, startTime, cfg)
	if err != nil {
		t.Fatalf("PredictPasses: %v", err)
	}

	if len(passes) == 0 {
		t.Skip("no passes found (unusual)")
	}

	// With 2s interval over a ~5min pass, expect ~150 samples.
	pass := passes[0]
	expectedMin := int(pass.Duration.Seconds()/2) - 1
	if len(pass.Samples) < expectedMin {
		t.Errorf("expected at least %d samples (2s interval, %s pass), got %d",
			expectedMin, pass.Duration, len(pass.Samples))
	}
}

func TestAge(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// TLE epoch is 24100.54321 → 2024, day 100.
	// Reference time: 2024, day 114 (14 days later).
	refTime := time.Date(2024, 4, 23, 12, 0, 0, 0, time.UTC)

	age, err := Age(sats[0], refTime)
	if err != nil {
		t.Fatalf("Age: %v", err)
	}

	// Should be approximately 14 days.
	days := age.Hours() / 24
	if days < 13 || days > 15 {
		t.Errorf("TLE age = %.1f days, expected ~14", days)
	}
}
