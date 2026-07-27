// Package report aggregates per-run field-data metrics for ntnbox run --report.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Report is the JSON field-data summary written at session end.
type Report struct {
	StartedAt   time.Time      `json:"started_at"`
	EndedAt     time.Time      `json:"ended_at"`
	DurationSec float64        `json:"duration_sec"`
	Profile     string         `json:"profile"`
	Coverage    CoverageStats  `json:"coverage"`
	Messaging   MessagingStats `json:"messaging"`
	Voice       VoiceStats     `json:"voice"`
}

// CoverageStats are wall-clock coverage buckets for the run.
type CoverageStats struct {
	InPct      float64 `json:"in_pct"`
	BlockedPct float64 `json:"blocked_pct"`
	OutPct     float64 `json:"out_pct"`
	InSec      float64 `json:"in_sec"`
	BlockedSec float64 `json:"blocked_sec"`
	OutSec     float64 `json:"out_sec"`
	Opens      int     `json:"opens"`
	Closes     int     `json:"closes"`
}

// MessagingStats summarize store-and-forward lifecycle events.
// Present is false when no MessageEvent was observed.
type MessagingStats struct {
	Present      bool    `json:"present"`
	Unique       int     `json:"unique"`
	Delivered    int     `json:"delivered"`
	Failed       int     `json:"failed"`
	Open         int     `json:"open"`
	DeliveryRate float64 `json:"delivery_rate"`
}

// MarshalJSON omits detail fields when no messaging traffic was observed.
func (m MessagingStats) MarshalJSON() ([]byte, error) {
	if !m.Present {
		return []byte(`{"present":false}`), nil
	}
	type full MessagingStats
	return json.Marshal(full(m))
}

// VoiceStats are voice-grade estimates and optional call session tallies.
type VoiceStats struct {
	Capable   bool           `json:"capable"`
	Estimates VoiceEstimates `json:"estimates"`
	Calls     CallStats      `json:"calls"`
}

// VoiceEstimates are link-derived illustrative voice metrics.
type VoiceEstimates struct {
	MouthToEarMsP50       float64 `json:"mouth_to_ear_ms_p50"`
	MouthToEarMsP95       float64 `json:"mouth_to_ear_ms_p95"`
	MOSAvg                float64 `json:"mos_avg"`
	JitterBufferStressAvg float64 `json:"jitter_buffer_stress_avg"`
	PLCPressureAvg        float64 `json:"plc_pressure_avg"`
	InCoverageSampleCount int     `json:"in_coverage_sample_count"`
}

// CallStats summarize ingested CallEvent lifecycle outcomes.
type CallStats struct {
	Present         bool    `json:"present"`
	Attempted       int     `json:"attempted"`
	Completed       int     `json:"completed"`
	Dropped         int     `json:"dropped"`
	Open            int     `json:"open"`
	CompletionRate  float64 `json:"completion_rate"`
	DropOnCloseRate float64 `json:"drop_on_close_rate"`
}

// MarshalJSON collapses idle non-capable voice to {"capable":false}.
// Call tallies are kept even when the profile is not voice-capable, matching
// call-events ingest (telemetry anytime a device posts). Estimates are omitted
// when capable but no in-coverage samples were collected (avoids mos_avg: 0).
func (v VoiceStats) MarshalJSON() ([]byte, error) {
	if !v.Capable && !v.Calls.Present {
		return []byte(`{"capable":false}`), nil
	}
	if !v.Capable {
		type callsOnly struct {
			Capable bool      `json:"capable"`
			Calls   CallStats `json:"calls"`
		}
		return json.Marshal(callsOnly{Capable: false, Calls: v.Calls})
	}
	if v.Estimates.InCoverageSampleCount == 0 {
		type noEstimates struct {
			Capable bool      `json:"capable"`
			Calls   CallStats `json:"calls"`
		}
		return json.Marshal(noEstimates{Capable: true, Calls: v.Calls})
	}
	type full VoiceStats
	return json.Marshal(full(v))
}

// MarshalJSON omits detail fields when no call events were observed.
func (c CallStats) MarshalJSON() ([]byte, error) {
	if !c.Present {
		return []byte(`{"present":false}`), nil
	}
	type full CallStats
	return json.Marshal(full(c))
}

// WriteJSON writes r to path with indentation.
func WriteJSON(path string, r Report) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	encodeErr := enc.Encode(r)
	if cerr := f.Close(); encodeErr == nil {
		encodeErr = cerr
	}
	return encodeErr
}

// SummaryLine is a short stderr-friendly summary.
func SummaryLine(r Report, path string) string {
	msg := "messaging=n/a"
	if r.Messaging.Present {
		msg = fmt.Sprintf("messaging=delivered %d/%d (%.0f%%)",
			r.Messaging.Delivered, r.Messaging.Unique, r.Messaging.DeliveryRate*100)
	}
	voice := "voice=n/a"
	hasSamples := r.Voice.Estimates.InCoverageSampleCount > 0
	switch {
	case r.Voice.Capable && hasSamples && r.Voice.Calls.Present:
		voice = fmt.Sprintf("voice=mos %.1f calls %d/%d",
			r.Voice.Estimates.MOSAvg, r.Voice.Calls.Completed, r.Voice.Calls.Attempted)
	case r.Voice.Capable && hasSamples:
		voice = fmt.Sprintf("voice=mos %.1f", r.Voice.Estimates.MOSAvg)
	case r.Voice.Calls.Present:
		voice = fmt.Sprintf("voice=calls %d/%d",
			r.Voice.Calls.Completed, r.Voice.Calls.Attempted)
	}
	return fmt.Sprintf("report: coverage in=%.1f%% blocked=%.1f%% out=%.1f%% opens=%d closes=%d %s %s → %s",
		r.Coverage.InPct, r.Coverage.BlockedPct, r.Coverage.OutPct,
		r.Coverage.Opens, r.Coverage.Closes, msg, voice, path)
}
