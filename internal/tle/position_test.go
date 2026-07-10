package tle

import (
	"testing"
	"time"
)

func TestSequenceEvaluator_Position(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	obs := Observer{LatDeg: 35.6762, LonDeg: 139.6503}
	startTime := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)

	passes, err := PredictPasses(sats[0], obs, startTime, PredictConfig{
		MinElevDeg: 5,
		Count:      2,
		MaxSearch:  48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("PredictPasses: %v", err)
	}
	if len(passes) == 0 {
		t.Skip("no passes found")
	}

	model := DefaultLinkModel()
	se, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:    1.0,
		StartAt:  passes[0].Rise,
		Observer: obs,
		Sat:      sats[0],
	})
	if err != nil {
		t.Fatalf("NewSequenceEvaluator: %v", err)
	}

	// Advance to pass start.
	wallStart := time.Now()
	se.Advance(wallStart)

	lat, lon, alt, elev, az, rng := se.Position()

	t.Logf("Position: lat=%.2f° lon=%.2f° alt=%.1fkm elev=%.1f° az=%.1f° range=%.1fkm",
		lat, lon, alt, elev, az, rng)

	// Sanity checks.
	if lat < -90 || lat > 90 {
		t.Errorf("latitude %f out of range [-90, 90]", lat)
	}
	if lon < -180 || lon > 360 {
		t.Errorf("longitude %f out of range [-180, 360]", lon)
	}
	if alt < 300 || alt > 500 {
		t.Errorf("altitude %f out of expected ISS range [300, 500] km", alt)
	}
	if rng <= 0 {
		t.Errorf("range %f should be positive", rng)
	}
	// During a pass, elevation should be above minimum.
	if elev < 0 {
		t.Errorf("elevation %f negative at pass start", elev)
	}
	if az < 0 || az > 360 {
		t.Errorf("azimuth %f out of range [0, 360]", az)
	}
}

func TestComputeOrbitPoints(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	refTime := time.Date(2024, 4, 9, 12, 0, 0, 0, time.UTC)
	points := ComputeOrbitPoints(sats[0], refTime, 200)

	if len(points) == 0 {
		t.Fatal("no orbit points computed")
	}
	if len(points) < 180 {
		t.Errorf("expected ~200 orbit points, got %d", len(points))
	}

	t.Logf("Orbit points: %d (first: lat=%.2f° lon=%.2f° alt=%.1fkm)",
		len(points), points[0][0], points[0][1], points[0][2])

	// All points should have valid coordinates.
	for i, p := range points {
		lat, lon, alt := p[0], p[1], p[2]
		if lat < -90 || lat > 90 {
			t.Errorf("point %d: latitude %f out of range", i, lat)
			break
		}
		if lon < -180 || lon > 360 {
			t.Errorf("point %d: longitude %f out of range", i, lon)
			break
		}
		if alt < 300 || alt > 500 {
			t.Errorf("point %d: altitude %f out of ISS range [300, 500]", i, alt)
			break
		}
	}

	// Points should span the globe (not all in one spot).
	minLat, maxLat := points[0][0], points[0][0]
	for _, p := range points {
		if p[0] < minLat {
			minLat = p[0]
		}
		if p[0] > maxLat {
			maxLat = p[0]
		}
	}
	latSpan := maxLat - minLat
	if latSpan < 50 {
		t.Errorf("latitude span %.1f° too narrow (expected >50° for ISS orbit)", latSpan)
	}
	t.Logf("Latitude span: %.1f° to %.1f° (span: %.1f°)", minLat, maxLat, latSpan)
}
