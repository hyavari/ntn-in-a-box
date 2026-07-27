// Package voice provides illustrative voice-grade link estimates for NTN demos.
// Values are engineering approximations, not ITU E-model calibrated MOS.
package voice

import "sort"

// Sample is one in-coverage estimate from delay/jitter/loss.
type Sample struct {
	MouthToEarMs       float64
	JitterBufferStress float64 // 0..1
	PLCPressure        float64 // 0..1
	MOS                float64 // 1.0..4.5
}

// Estimates aggregates Samples for a report block.
type Estimates struct {
	MouthToEarMsP50       float64
	MouthToEarMsP95       float64
	MOSAvg                float64
	JitterBufferStressAvg float64
	PLCPressureAvg        float64
	InCoverageSampleCount int
}

// Estimate maps link impairments to a voice-grade sample.
// delayMs is treated as RTT-ish profile delay; mouth-to-ear uses half plus a
// fixed 40 ms jitter-buffer/codec pad.
func Estimate(delayMs, jitterMs, lossPct float64) Sample {
	m2e := delayMs/2 + 40
	stress := clamp(jitterMs/80, 0, 1)
	plc := clamp(lossPct/10, 0, 1)
	mos := clamp(4.5-0.004*max(m2e-150, 0)-1.2*plc-0.6*stress, 1.0, 4.5)
	return Sample{
		MouthToEarMs:       m2e,
		JitterBufferStress: stress,
		PLCPressure:        plc,
		MOS:                mos,
	}
}

// Aggregate computes percentile and average estimates. Empty input yields zeros.
func Aggregate(samples []Sample) Estimates {
	n := len(samples)
	if n == 0 {
		return Estimates{}
	}
	m2e := make([]float64, n)
	var mosSum, stressSum, plcSum float64
	for i, s := range samples {
		m2e[i] = s.MouthToEarMs
		mosSum += s.MOS
		stressSum += s.JitterBufferStress
		plcSum += s.PLCPressure
	}
	sort.Float64s(m2e)
	return Estimates{
		MouthToEarMsP50:       nearestRank(m2e, 50),
		MouthToEarMsP95:       nearestRank(m2e, 95),
		MOSAvg:                mosSum / float64(n),
		JitterBufferStressAvg: stressSum / float64(n),
		PLCPressureAvg:        plcSum / float64(n),
		InCoverageSampleCount: n,
	}
}

func nearestRank(sorted []float64, pct int) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := (n - 1) * pct / 100
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
