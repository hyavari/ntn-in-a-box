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

// WriteJSON writes r to path with indentation.
func WriteJSON(path string, r Report) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// SummaryLine is a short stderr-friendly summary.
func SummaryLine(r Report, path string) string {
	msg := "messaging=n/a"
	if r.Messaging.Present {
		msg = fmt.Sprintf("messaging=delivered %d/%d (%.0f%%)",
			r.Messaging.Delivered, r.Messaging.Unique, r.Messaging.DeliveryRate*100)
	}
	return fmt.Sprintf("report: coverage in=%.1f%% blocked=%.1f%% out=%.1f%% opens=%d closes=%d %s → %s",
		r.Coverage.InPct, r.Coverage.BlockedPct, r.Coverage.OutPct,
		r.Coverage.Opens, r.Coverage.Closes, msg, path)
}
