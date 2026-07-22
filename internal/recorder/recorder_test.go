package recorder

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

type mockEval struct{}

func (mockEval) Evaluate(now time.Time) (condition.LinkState, condition.CoverageState) {
	return condition.LinkState{}, condition.CoverageState{
		InCoverage:             true,
		ElapsedSec:             5.0,
		UntilNextTransitionSec: 85.0,
	}
}

func TestRecorder_WritesEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.jsonl")
	rec, err := New(path, mockEval{})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)

	rec.OnCoverage(eventbus.CoverageEvent{
		Kind:     eventbus.KindWindowOpened,
		At:       now,
		DeviceID: "sandbox-1",
	})

	rec.OnLinkState(eventbus.LinkStateEvent{
		State: condition.LinkState{
			DelayMs:       42,
			JitterMs:      5,
			LossPct:       0.2,
			BandwidthKbps: 20000,
		},
		At:       now.Add(250 * time.Millisecond),
		DeviceID: "sandbox-1",
	})

	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	// Read and verify.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck

	scanner := bufio.NewScanner(f)
	var records []EventRecord

	for scanner.Scan() {
		var r EventRecord
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		records = append(records, r)
	}

	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}

	if records[0].Type != "coverage" {
		t.Errorf("record[0].Type = %q, want coverage", records[0].Type)
	}
	if records[0].Kind != "window_opened" {
		t.Errorf("record[0].Kind = %q, want window_opened", records[0].Kind)
	}
	if !records[0].InCoverage {
		t.Error("record[0].InCoverage should be true")
	}

	if records[0].DeviceID != "sandbox-1" {
		t.Errorf("record[0].DeviceID = %q, want sandbox-1", records[0].DeviceID)
	}
	if records[1].Type != "linkstate" {
		t.Errorf("record[1].Type = %q, want linkstate", records[1].Type)
	}
	if records[1].DelayMs != 42 {
		t.Errorf("record[1].DelayMs = %f, want 42", records[1].DelayMs)
	}
	if records[1].DeviceID != "sandbox-1" {
		t.Errorf("record[1].DeviceID = %q, want sandbox-1", records[1].DeviceID)
	}
}

func TestRecorder_UsesDeviceEvaluator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.jsonl")
	primary := mockEval{}
	rec, err := New(path, primary)
	if err != nil {
		t.Fatal(err)
	}
	peer := mockEvalOut{}
	rec.RegisterDevice("sandbox-1", peer)

	now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	rec.OnCoverage(eventbus.CoverageEvent{
		Kind:     eventbus.KindWindowClosed,
		At:       now,
		DeviceID: "sandbox-1",
	})
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	var r EventRecord
	if err := json.NewDecoder(f).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.DeviceID != "sandbox-1" {
		t.Fatalf("DeviceID=%q", r.DeviceID)
	}
	if r.InCoverage {
		t.Fatal("expected peer evaluator out-of-coverage")
	}
}

type mockEvalOut struct{}

func (mockEvalOut) Evaluate(now time.Time) (condition.LinkState, condition.CoverageState) {
	return condition.LinkState{}, condition.CoverageState{
		InCoverage:             false,
		ElapsedSec:             1,
		UntilNextTransitionSec: 99,
	}
}

func TestReplayer_PublishesDeviceID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deviceid.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"coverage","kind":"window_opened","device_id":"sandbox-1","at":"2026-07-08T10:00:00Z","in_coverage":true}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"linkstate","device_id":"sandbox-1","delay_ms":42,"jitter_ms":5,"loss_pct":0.2,"bandwidth_kbps":20000,"at":"2026-07-08T10:00:00.1Z"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New(eventbus.LinkStateThrottle{Interval: 0, DeltaThreshold: 0})
	var covEvents []eventbus.CoverageEvent
	var linkEvents []eventbus.LinkStateEvent
	bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) { covEvents = append(covEvents, ev) })
	bus.SubscribeLinkState(func(ev eventbus.LinkStateEvent) { linkEvents = append(linkEvents, ev) })

	replayer := NewReplayer(path, bus, 1000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := replayer.Run(ctx); err != nil {
		t.Fatal(err)
	}

	if len(covEvents) != 1 || covEvents[0].DeviceID != "sandbox-1" {
		t.Fatalf("coverage DeviceID = %v, want sandbox-1", covEvents)
	}
	if len(linkEvents) != 1 || linkEvents[0].DeviceID != "sandbox-1" {
		t.Fatalf("linkstate DeviceID = %v, want sandbox-1", linkEvents)
	}
}

func TestReplayer_PublishesInOrder(t *testing.T) {
	// Write a test recording.
	path := filepath.Join(t.TempDir(), "replay.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"coverage","kind":"window_opened","at":"2026-07-08T10:00:00Z","in_coverage":true,"elapsed_sec":0,"until_next_transition":90}` + "\n",
		`{"type":"linkstate","delay_ms":150,"jitter_ms":40,"loss_pct":10,"bandwidth_kbps":2000,"at":"2026-07-08T10:00:00.1Z"}` + "\n",
		`{"type":"linkstate","delay_ms":42,"jitter_ms":5,"loss_pct":0.2,"bandwidth_kbps":20000,"at":"2026-07-08T10:00:00.2Z"}` + "\n",
		`{"type":"coverage","kind":"window_closed","at":"2026-07-08T10:00:00.3Z","in_coverage":false,"elapsed_sec":0,"until_next_transition":510}` + "\n",
	}
	for _, line := range lines {
		if _, err := f.WriteString(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New(eventbus.LinkStateThrottle{Interval: 0, DeltaThreshold: 0})

	var covEvents []eventbus.CoverageEvent
	var linkEvents []eventbus.LinkStateEvent
	var obsEvents []eventbus.ObservabilityEvent
	bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) { covEvents = append(covEvents, ev) })
	bus.SubscribeLinkState(func(ev eventbus.LinkStateEvent) { linkEvents = append(linkEvents, ev) })
	bus.SubscribeObservability(func(ev eventbus.ObservabilityEvent) { obsEvents = append(obsEvents, ev) })

	replayer := NewReplayer(path, bus, 1000) // 1000x speed for fast test
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := replayer.Run(ctx); err != nil {
		t.Fatal(err)
	}

	if len(covEvents) != 2 {
		t.Fatalf("got %d coverage events, want 2", len(covEvents))
	}
	if covEvents[0].Kind != eventbus.KindWindowOpened {
		t.Errorf("event[0].Kind = %q, want window_opened", covEvents[0].Kind)
	}
	if covEvents[1].Kind != eventbus.KindWindowClosed {
		t.Errorf("event[1].Kind = %q, want window_closed", covEvents[1].Kind)
	}
	if len(obsEvents) != 1 || obsEvents[0].Name != eventbus.ObsReplayDone {
		t.Errorf("expected 1 observability event (replay_done), got %d", len(obsEvents))
	}
	if len(linkEvents) != 2 {
		t.Fatalf("got %d link events, want 2", len(linkEvents))
	}
	if linkEvents[0].State.DelayMs != 150 {
		t.Errorf("link[0].DelayMs = %f, want 150", linkEvents[0].State.DelayMs)
	}
	if linkEvents[1].State.DelayMs != 42 {
		t.Errorf("link[1].DelayMs = %f, want 42", linkEvents[1].State.DelayMs)
	}
}

func TestReplayer_SkipsBadLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"this is not json\n",
		`{"type":"linkstate","delay_ms":42,"jitter_ms":5,"loss_pct":0.2,"bandwidth_kbps":20000,"at":"2026-07-08T10:00:00Z"}` + "\n",
		"{bad json again}\n",
	} {
		if _, err := f.WriteString(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New(eventbus.LinkStateThrottle{Interval: 0, DeltaThreshold: 0})
	var count int
	bus.SubscribeLinkState(func(ev eventbus.LinkStateEvent) { count++ })

	replayer := NewReplayer(path, bus, 1000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = replayer.Run(ctx)
	if count != 1 {
		t.Errorf("got %d events (expected 1, bad lines skipped)", count)
	}
}

func TestReplayer_Cancellation(t *testing.T) {
	// Long recording that should be interrupted.
	path := filepath.Join(t.TempDir(), "long.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 100 {
		ts := time.Date(2026, 7, 8, 10, 0, i, 0, time.UTC).Format(time.RFC3339Nano)
		if _, err := f.WriteString(fmt.Sprintf(`{"type":"linkstate","delay_ms":42,"jitter_ms":5,"loss_pct":0.2,"bandwidth_kbps":20000,"at":"%s"}`+"\n", ts)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New(eventbus.LinkStateThrottle{Interval: 0, DeltaThreshold: 0})
	var count int
	bus.SubscribeLinkState(func(ev eventbus.LinkStateEvent) { count++ })

	ctx, cancel := context.WithCancel(context.Background())
	replayer := NewReplayer(path, bus, 1) // real-time speed

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_ = replayer.Run(ctx)
	// Should have been cancelled well before all 100 events (1s apart).
	if count >= 50 {
		t.Errorf("expected cancellation to stop early, got %d events", count)
	}
}
