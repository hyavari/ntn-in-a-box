package voice

import (
	"math"
	"testing"
)

func TestEstimate_KnownInputs(t *testing.T) {
	s := Estimate(600, 20, 1)
	assertNear(t, "m2e", s.MouthToEarMs, 340)
	assertNear(t, "stress", s.JitterBufferStress, 0.25)
	assertNear(t, "plc", s.PLCPressure, 0.1)
	// 4.5 - 0.004*(340-150) - 1.2*0.1 - 0.6*0.25 = 4.5 - 0.76 - 0.12 - 0.15 = 3.47
	assertNear(t, "mos", s.MOS, 3.47)
}

func TestEstimate_Clamps(t *testing.T) {
	s := Estimate(0, 1000, 100)
	if s.JitterBufferStress != 1 {
		t.Fatalf("stress = %v, want 1", s.JitterBufferStress)
	}
	if s.PLCPressure != 1 {
		t.Fatalf("plc = %v, want 1", s.PLCPressure)
	}
	if s.MOS < 1 || s.MOS > 4.5 {
		t.Fatalf("mos = %v, want in [1, 4.5]", s.MOS)
	}

	s2 := Estimate(100, -10, -5)
	if s2.JitterBufferStress != 0 || s2.PLCPressure != 0 {
		t.Fatalf("negative impairments should clamp to 0: %+v", s2)
	}
}

func TestAggregate_Empty(t *testing.T) {
	e := Aggregate(nil)
	if e.InCoverageSampleCount != 0 {
		t.Fatalf("count = %d, want 0", e.InCoverageSampleCount)
	}
}

func TestAggregate_P50P95(t *testing.T) {
	samples := make([]Sample, 0, 20)
	for i := 0; i < 20; i++ {
		// delay such that m2e = 40 + i (delay = 2*(m2e-40))
		m2e := float64(40 + i)
		delay := 2 * (m2e - 40)
		samples = append(samples, Estimate(delay, 0, 0))
	}
	e := Aggregate(samples)
	if e.InCoverageSampleCount != 20 {
		t.Fatalf("count = %d", e.InCoverageSampleCount)
	}
	// nearest-rank: idx = (19)*50/100 = 9 → 49; idx = 19*95/100 = 18 → 58
	assertNear(t, "p50", e.MouthToEarMsP50, 49)
	assertNear(t, "p95", e.MouthToEarMsP95, 58)
}

func TestProfileCapable(t *testing.T) {
	for _, name := range []string{"lband_geo", "geo_steady", "geo_blockage"} {
		if !ProfileCapable(name) {
			t.Errorf("%s should be capable", name)
		}
	}
	for _, name := range []string{"nbiot_ntn", "leo_pass_90s", "d2c_burst", "sos_burst", ""} {
		if ProfileCapable(name) {
			t.Errorf("%s should not be capable", name)
		}
	}
}

func assertNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}
