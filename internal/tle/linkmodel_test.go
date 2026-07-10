package tle

import (
	"math"
	"testing"
)

func TestDefaultLinkModel(t *testing.T) {
	m := DefaultLinkModel()
	if m.Name != "leo_default" {
		t.Errorf("Name = %q, want leo_default", m.Name)
	}
	if m.MinElevDeg != 10 {
		t.Errorf("MinElevDeg = %f, want 10", m.MinElevDeg)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestInterpolate_ExactPoints(t *testing.T) {
	m := DefaultLinkModel()

	// At exactly 10° (first point).
	delay, jitter, loss, bw := m.Interpolate(10)
	assertClose(t, "delay@10°", delay, 150)
	assertClose(t, "jitter@10°", jitter, 40)
	assertClose(t, "loss@10°", loss, 10)
	assertClose(t, "bw@10°", bw, 2000)

	// At exactly 90° (last point).
	delay, jitter, loss, bw = m.Interpolate(90)
	assertClose(t, "delay@90°", delay, 25)
	assertClose(t, "jitter@90°", jitter, 5)
	assertClose(t, "loss@90°", loss, 0.2)
	assertClose(t, "bw@90°", bw, 20000)
}

func TestInterpolate_Between(t *testing.T) {
	m := DefaultLinkModel()

	// Midpoint between 10° and 30° → linear interpolation.
	// Delay: 150 + 0.5*(60-150) = 105
	delay, _, _, _ := m.Interpolate(20)
	assertClose(t, "delay@20°", delay, 105)

	// Bandwidth: 2000 + 0.5*(10000-2000) = 6000
	_, _, _, bw := m.Interpolate(20)
	assertClose(t, "bw@20°", bw, 6000)
}

func TestInterpolate_BelowMin(t *testing.T) {
	m := DefaultLinkModel()

	// Below minimum elevation → clamp to first point values.
	delay, jitter, loss, bw := m.Interpolate(5)
	assertClose(t, "delay@5°", delay, 150)
	assertClose(t, "jitter@5°", jitter, 40)
	assertClose(t, "loss@5°", loss, 10)
	assertClose(t, "bw@5°", bw, 2000)
}

func TestInterpolate_Above90(t *testing.T) {
	m := DefaultLinkModel()

	// Above 90° → clamp to last point values.
	delay, jitter, loss, bw := m.Interpolate(95)
	assertClose(t, "delay@95°", delay, 25)
	assertClose(t, "jitter@95°", jitter, 5)
	assertClose(t, "loss@95°", loss, 0.2)
	assertClose(t, "bw@95°", bw, 20000)
}

func TestLinkModel_Validate(t *testing.T) {
	// Empty curve.
	m := LinkModel{
		Name:       "bad",
		DelayMs:    nil,
		JitterMs:   []ModelPoint{{10, 5}},
		LossPct:    []ModelPoint{{10, 1}},
		BandwidthKbps: []ModelPoint{{10, 1000}},
	}
	if err := m.Validate(); err == nil {
		t.Error("expected error for empty delay_ms curve")
	}

	// Unsorted curve.
	m2 := LinkModel{
		Name:       "bad",
		DelayMs:    []ModelPoint{{30, 60}, {10, 150}}, // wrong order
		JitterMs:   []ModelPoint{{10, 5}},
		LossPct:    []ModelPoint{{10, 1}},
		BandwidthKbps: []ModelPoint{{10, 1000}},
	}
	if err := m2.Validate(); err == nil {
		t.Error("expected error for unsorted delay_ms curve")
	}
}

func assertClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s = %f, want %f", label, got, want)
	}
}
