package recorder

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// Replayer reads a JSONL recording and publishes events to the bus
// with original timing (scaled by Speed).
type Replayer struct {
	path  string
	bus   *eventbus.Bus
	speed float64 // 1.0 = real time, 10.0 = 10x faster
	onProgress func(elapsed, total time.Duration) // optional progress callback
}

// NewReplayer creates a Replayer for the given recording file.
// Speed controls playback rate (1.0 = real time).
func NewReplayer(path string, bus *eventbus.Bus, speed float64) *Replayer {
	if speed <= 0 {
		speed = 1.0
	}
	return &Replayer{path: path, bus: bus, speed: speed}
}

// OnProgress sets a callback that fires periodically with replay progress.
func (r *Replayer) OnProgress(fn func(elapsed, total time.Duration)) {
	r.onProgress = fn
}

// Run reads the recording and publishes events to the bus, sleeping
// between events to maintain original timing (divided by speed).
// Blocks until the file is exhausted or ctx is cancelled.
func (r *Replayer) Run(ctx context.Context) error {
	// First pass: determine total duration for progress reporting.
	totalDuration := r.scanDuration()

	f, err := os.Open(r.path)
	if err != nil {
		return fmt.Errorf("replay: open %s: %w", r.path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var firstAt, lastAt time.Time
	first := true

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		var rec EventRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}

		at, err := time.Parse(time.RFC3339Nano, rec.At)
		if err != nil {
			continue
		}

		if first {
			firstAt = at
		}

		// Sleep to maintain timing.
		if !first {
			delta := at.Sub(lastAt)
			scaledDelta := time.Duration(float64(delta) / r.speed)
			if scaledDelta > 0 {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(scaledDelta):
				}
			}
		}
		first = false
		lastAt = at

		// Report progress.
		if r.onProgress != nil {
			elapsed := at.Sub(firstAt)
			r.onProgress(elapsed, totalDuration)
		}

		// Publish to bus.
		now := time.Now()
		switch rec.Type {
		case "coverage":
			r.bus.PublishCoverageEvent(eventbus.CoverageEvent{
				Kind:                eventbus.CoverageEventKind(rec.Kind),
				At:                  now,
				InCoverage:          rec.InCoverage,
				ElapsedSec:          rec.ElapsedSec,
				UntilNextTransition: rec.UntilNextTransition,
			})
		case "linkstate":
			r.bus.PublishLinkState(condition.LinkState{
				DelayMs:       rec.DelayMs,
				JitterMs:      rec.JitterMs,
				LossPct:       rec.LossPct,
				BandwidthKbps: rec.BandwidthKbps,
			}, now)
		}
	}

	// Final progress.
	if r.onProgress != nil {
		r.onProgress(totalDuration, totalDuration)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Signal replay completion via the observability channel so it
	// doesn't fan out to coverage subscribers (which would incorrectly
	// trigger coverage-state logic).
	r.bus.Emit(eventbus.ObservabilityEvent{
		Name: eventbus.ObsReplayDone,
		At:   time.Now(),
	})

	return nil
}

// scanDuration reads the file to determine the time span between
// first and last event.
func (r *Replayer) scanDuration() time.Duration {
	f, err := os.Open(r.path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var firstAt, lastAt time.Time
	first := true

	for scanner.Scan() {
		var rec EventRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, rec.At)
		if err != nil {
			continue
		}
		if first {
			firstAt = at
			first = false
		}
		lastAt = at
	}

	if first {
		return 0
	}
	return lastAt.Sub(firstAt)
}
