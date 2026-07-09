// Package recorder provides Record and Replay functionality for
// NTN-in-a-Box bus events. Recording serializes coverage and link-state
// events to a JSONL file; replaying reads them back and publishes to
// the bus with original timing.
package recorder

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// EventRecord is a single recorded event in JSONL format.
type EventRecord struct {
	Type string `json:"type"` // "coverage" or "linkstate"
	At   string `json:"at"`

	// Coverage fields (when Type == "coverage").
	Kind                string  `json:"kind,omitempty"`
	InCoverage          bool    `json:"in_coverage,omitempty"`
	ElapsedSec          float64 `json:"elapsed_sec,omitempty"`
	UntilNextTransition float64 `json:"until_next_transition,omitempty"`

	// LinkState fields (when Type == "linkstate").
	DelayMs       float64 `json:"delay_ms,omitempty"`
	JitterMs      float64 `json:"jitter_ms,omitempty"`
	LossPct       float64 `json:"loss_pct,omitempty"`
	BandwidthKbps float64 `json:"bandwidth_kbps,omitempty"`
}

// Recorder subscribes to the event bus and writes events to a JSONL file.
type Recorder struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
	eval Evaluator
}

// Evaluator is the subset of condition.Evaluator the recorder needs.
type Evaluator interface {
	Evaluate(now time.Time) (condition.LinkState, condition.CoverageState)
}

// New creates a Recorder that writes to the given file path.
func New(path string, eval Evaluator) (*Recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &Recorder{
		file: f,
		enc:  json.NewEncoder(f),
		eval: eval,
	}, nil
}

// OnCoverage is a CoverageHandler that records coverage events.
func (r *Recorder) OnCoverage(ev eventbus.CoverageEvent) {
	_, cov := r.eval.Evaluate(ev.At)
	rec := EventRecord{
		Type:                "coverage",
		At:                  ev.At.Format(time.RFC3339Nano),
		Kind:                string(ev.Kind),
		InCoverage:          cov.InCoverage,
		ElapsedSec:          cov.ElapsedSec,
		UntilNextTransition: cov.UntilNextTransitionSec,
	}
	r.write(rec)
}

// OnLinkState is a LinkStateHandler that records link-state events.
func (r *Recorder) OnLinkState(ev eventbus.LinkStateEvent) {
	rec := EventRecord{
		Type:          "linkstate",
		At:            ev.At.Format(time.RFC3339Nano),
		DelayMs:       ev.State.DelayMs,
		JitterMs:      ev.State.JitterMs,
		LossPct:       ev.State.LossPct,
		BandwidthKbps: ev.State.BandwidthKbps,
	}
	r.write(rec)
}

func (r *Recorder) write(rec EventRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = r.enc.Encode(rec)
}

// Close flushes and closes the output file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}
