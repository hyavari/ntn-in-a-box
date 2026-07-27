package report

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

type scriptSampler struct {
	points []samplePoint
}

type samplePoint struct {
	at         time.Time
	in         bool
	inBlockage bool
}

func (s *scriptSampler) Sample(now time.Time) (bool, bool) {
	var last samplePoint
	for _, p := range s.points {
		if !now.Before(p.at) {
			last = p
		}
	}
	return last.in, last.inBlockage
}

func TestAggregator_MidWindowBlockage(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	sam := &scriptSampler{points: []samplePoint{
		{at: start, in: true},
		{at: start.Add(10 * time.Second), in: false, inBlockage: true},
		{at: start.Add(18 * time.Second), in: true},
	}}

	agg := New(Config{
		Sampler:   sam,
		Profile:   "geo_blockage",
		Start:     start,
		TickEvery: -1,
	})

	// Drive samples without fabricating window_opened (blockage ≠ scheduled close).
	agg.sampleNow(start.Add(10 * time.Second))
	agg.sampleNow(start.Add(18 * time.Second))

	end := start.Add(30 * time.Second)
	r := agg.Finalize(end)

	if r.Coverage.Opens != 0 || r.Coverage.Closes != 0 {
		t.Fatalf("opens/closes = %d/%d, want 0/0", r.Coverage.Opens, r.Coverage.Closes)
	}
	assertNear(t, "in_sec", r.Coverage.InSec, 22)          // 0-10 + 18-30
	assertNear(t, "blocked_sec", r.Coverage.BlockedSec, 8) // 10-18
	assertNear(t, "out_sec", r.Coverage.OutSec, 0)
	assertNear(t, "pct_sum", r.Coverage.InPct+r.Coverage.BlockedPct+r.Coverage.OutPct, 100)
	if r.Messaging.Present {
		t.Fatal("messaging.present should be false")
	}
}

func TestAggregator_WindowCloseCountsOut(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	sam := &scriptSampler{points: []samplePoint{
		{at: start, in: true},
		{at: start.Add(5 * time.Second), in: false, inBlockage: false},
	}}
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	agg := New(Config{
		Bus:       bus,
		Sampler:   sam,
		Profile:   "leo",
		Start:     start,
		TickEvery: -1,
	})
	bus.PublishCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: start})
	bus.PublishCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowClosed, At: start.Add(5 * time.Second)})
	r := agg.Finalize(start.Add(10 * time.Second))

	if r.Coverage.Opens != 1 || r.Coverage.Closes != 1 {
		t.Fatalf("opens/closes = %d/%d, want 1/1", r.Coverage.Opens, r.Coverage.Closes)
	}
	assertNear(t, "in_sec", r.Coverage.InSec, 5)
	assertNear(t, "out_sec", r.Coverage.OutSec, 5)
	assertNear(t, "blocked_sec", r.Coverage.BlockedSec, 0)
}

func TestAggregator_MessagingRates(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	agg := New(Config{
		Bus:       bus,
		Sampler:   &scriptSampler{points: []samplePoint{{at: start, in: true}}},
		Profile:   "p",
		Start:     start,
		TickEvery: -1,
	})
	bus.PublishMessage(eventbus.MessageEvent{ID: "m1", Status: "queued", At: start})
	bus.PublishMessage(eventbus.MessageEvent{ID: "m1", Status: "delivered", At: start.Add(time.Second)})
	bus.PublishMessage(eventbus.MessageEvent{ID: "m2", Status: "failed", At: start.Add(2 * time.Second)})
	bus.PublishMessage(eventbus.MessageEvent{ID: "m3", Status: "in_flight", At: start.Add(3 * time.Second)})

	r := agg.Finalize(start.Add(4 * time.Second))
	if !r.Messaging.Present {
		t.Fatal("present")
	}
	if r.Messaging.Unique != 3 || r.Messaging.Delivered != 1 || r.Messaging.Failed != 1 || r.Messaging.Open != 1 {
		t.Fatalf("messaging = %+v", r.Messaging)
	}
	assertNear(t, "delivery_rate", r.Messaging.DeliveryRate, 1.0/3.0)
}

func TestAggregator_BlockageEventsDoNotCountOpensCloses(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	sam := &scriptSampler{points: []samplePoint{
		{at: start, in: true},
		{at: start.Add(10 * time.Second), in: false, inBlockage: true},
		{at: start.Add(18 * time.Second), in: true},
	}}
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	agg := New(Config{
		Bus:       bus,
		Sampler:   sam,
		Profile:   "geo_blockage",
		Start:     start,
		TickEvery: -1,
	})
	// Driver emits window_* on InCoverage flips (including blockage).
	bus.PublishCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: start})
	bus.PublishCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowClosed, At: start.Add(10 * time.Second)})
	bus.PublishCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: start.Add(18 * time.Second)})
	r := agg.Finalize(start.Add(30 * time.Second))

	if r.Coverage.Opens != 1 || r.Coverage.Closes != 0 {
		t.Fatalf("opens/closes = %d/%d, want 1/0 (initial open only; blockage ignored)", r.Coverage.Opens, r.Coverage.Closes)
	}
	assertNear(t, "blocked_sec", r.Coverage.BlockedSec, 8)
}

func TestAggregator_IgnoresOtherDevice(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	agg := New(Config{
		Bus:       bus,
		Sampler:   &scriptSampler{points: []samplePoint{{at: start, in: true}}},
		DeviceID:  "sandbox-0",
		Start:     start,
		TickEvery: -1,
	})
	bus.PublishCoverageEvent(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowClosed, DeviceID: "sandbox-1", At: start.Add(time.Second),
	})
	r := agg.Finalize(start.Add(2 * time.Second))
	if r.Coverage.Closes != 0 {
		t.Fatalf("closes = %d, want 0", r.Coverage.Closes)
	}
}

type scriptLinkSampler struct {
	scriptSampler
	delay, jitter, loss float64
	nan                 bool
}

func (s *scriptLinkSampler) SampleLink(now time.Time) (condition.LinkState, bool, bool) {
	in, blocked := s.Sample(now)
	if s.nan || !in {
		nan := math.NaN()
		return condition.LinkState{DelayMs: nan, JitterMs: nan, LossPct: nan}, in, blocked
	}
	return condition.LinkState{DelayMs: s.delay, JitterMs: s.jitter, LossPct: s.loss}, in, blocked
}

func TestAggregator_VoiceNotCapable(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	agg := New(Config{
		Sampler: &scriptLinkSampler{
			scriptSampler: scriptSampler{points: []samplePoint{{at: start, in: true}}},
			delay:         600, jitter: 20, loss: 1,
		},
		Start:        start,
		TickEvery:    -1,
		VoiceCapable: false,
	})
	agg.sampleNow(start)
	r := agg.Finalize(start.Add(time.Second))
	if r.Voice.Capable {
		t.Fatal("voice should not be capable")
	}
	raw, err := json.Marshal(r.Voice)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"capable":false}` {
		t.Fatalf("json = %s", raw)
	}
}

func TestAggregator_VoiceCapableNoSamples(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	agg := New(Config{
		Sampler: &scriptLinkSampler{
			scriptSampler: scriptSampler{points: []samplePoint{{at: start, in: false}}},
		},
		Start:        start,
		TickEvery:    -1,
		VoiceCapable: true,
	})
	r := agg.Finalize(start.Add(time.Second))
	if !r.Voice.Capable {
		t.Fatal("capable")
	}
	if r.Voice.Estimates.InCoverageSampleCount != 0 {
		t.Fatalf("samples = %d", r.Voice.Estimates.InCoverageSampleCount)
	}
	raw, err := json.Marshal(r.Voice)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "mos_avg") || strings.Contains(string(raw), "estimates") {
		t.Fatalf("json should omit empty estimates: %s", raw)
	}
	line := SummaryLine(r, "out.json")
	if strings.Contains(line, "voice=mos") {
		t.Fatalf("summary should not invent MOS 0.0: %s", line)
	}
	if !strings.Contains(line, "voice=n/a") {
		t.Fatalf("summary = %s", line)
	}
}

func TestAggregator_VoiceCallsNotCapable(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	agg := New(Config{
		Bus:          bus,
		Start:        start,
		TickEvery:    -1,
		VoiceCapable: false,
	})
	bus.PublishCall(eventbus.CallEvent{ID: "c1", Status: "started", At: start})
	bus.PublishCall(eventbus.CallEvent{ID: "c1", Status: "completed", At: start.Add(time.Second)})
	r := agg.Finalize(start.Add(2 * time.Second))
	if r.Voice.Capable {
		t.Fatal("capable should stay false")
	}
	if !r.Voice.Calls.Present || r.Voice.Calls.Completed != 1 || r.Voice.Calls.Attempted != 1 {
		t.Fatalf("calls = %+v", r.Voice.Calls)
	}
	raw, err := json.Marshal(r.Voice)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"capable":false`) || !strings.Contains(string(raw), `"calls"`) {
		t.Fatalf("json should keep calls when not capable: %s", raw)
	}
	line := SummaryLine(r, "out.json")
	if !strings.Contains(line, "voice=calls 1/1") {
		t.Fatalf("summary = %s", line)
	}
}

func TestAggregator_VoiceEstimatesNoCalls(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	sam := &scriptLinkSampler{
		scriptSampler: scriptSampler{points: []samplePoint{{at: start, in: true}}},
		delay:         600, jitter: 20, loss: 1,
	}
	agg := New(Config{
		Sampler:      sam,
		Profile:      "lband_geo",
		Start:        start,
		TickEvery:    -1,
		VoiceCapable: true,
	})
	agg.sampleNow(start)
	agg.sampleNow(start.Add(time.Second))
	r := agg.Finalize(start.Add(2 * time.Second))
	if !r.Voice.Capable {
		t.Fatal("capable")
	}
	if r.Voice.Estimates.InCoverageSampleCount != 2 {
		t.Fatalf("samples = %d, want 2", r.Voice.Estimates.InCoverageSampleCount)
	}
	assertNear(t, "m2e_p50", r.Voice.Estimates.MouthToEarMsP50, 340)
	assertNear(t, "mos", r.Voice.Estimates.MOSAvg, 3.47)
	if r.Voice.Calls.Present {
		t.Fatal("calls should be absent")
	}
	line := SummaryLine(r, "out.json")
	if !strings.Contains(line, "voice=mos") {
		t.Fatalf("summary missing voice: %s", line)
	}
}

func TestAggregator_VoiceCallsAndNaNSkip(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	sam := &scriptLinkSampler{
		scriptSampler: scriptSampler{points: []samplePoint{
			{at: start, in: true},
			{at: start.Add(2 * time.Second), in: false, inBlockage: true},
		}},
		delay: 600, jitter: 20, loss: 1,
	}
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	agg := New(Config{
		Bus:          bus,
		Sampler:      sam,
		Start:        start,
		TickEvery:    -1,
		VoiceCapable: true,
	})
	agg.sampleNow(start) // counted
	sam.nan = true
	agg.sampleNow(start.Add(time.Second)) // NaN skip even if flagged in
	sam.nan = false
	agg.sampleNow(start.Add(2 * time.Second)) // out of coverage skip

	bus.PublishCall(eventbus.CallEvent{ID: "c1", Status: "started", At: start})
	bus.PublishCall(eventbus.CallEvent{ID: "c1", Status: "completed", At: start.Add(time.Second)})
	bus.PublishCall(eventbus.CallEvent{ID: "c2", Status: "started", At: start})
	bus.PublishCall(eventbus.CallEvent{ID: "c2", Status: "dropped", At: start.Add(2 * time.Second)})

	r := agg.Finalize(start.Add(3 * time.Second))
	if r.Voice.Estimates.InCoverageSampleCount != 1 {
		t.Fatalf("samples = %d, want 1", r.Voice.Estimates.InCoverageSampleCount)
	}
	if !r.Voice.Calls.Present || r.Voice.Calls.Attempted != 2 {
		t.Fatalf("calls = %+v", r.Voice.Calls)
	}
	if r.Voice.Calls.Completed != 1 || r.Voice.Calls.Dropped != 1 {
		t.Fatalf("calls = %+v", r.Voice.Calls)
	}
	assertNear(t, "completion", r.Voice.Calls.CompletionRate, 0.5)
	assertNear(t, "drop", r.Voice.Calls.DropOnCloseRate, 0.5)
}

func TestAggregator_CallDeviceIDFilterAndOpen(t *testing.T) {
	start := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	agg := New(Config{
		Bus:          bus,
		Sampler:      &scriptSampler{points: []samplePoint{{at: start, in: true}}},
		DeviceID:     "sandbox-0",
		Start:        start,
		TickEvery:    -1,
		VoiceCapable: true,
	})
	bus.PublishCall(eventbus.CallEvent{ID: "mine", DeviceID: "sandbox-0", Status: "started", At: start})
	bus.PublishCall(eventbus.CallEvent{ID: "other", DeviceID: "sandbox-1", Status: "completed", At: start})
	bus.PublishCall(eventbus.CallEvent{ID: "open1", DeviceID: "sandbox-0", Status: "started", At: start})

	r := agg.Finalize(start.Add(time.Second))
	if r.Voice.Calls.Attempted != 2 {
		t.Fatalf("attempted = %d, want 2 (foreign device filtered)", r.Voice.Calls.Attempted)
	}
	if r.Voice.Calls.Open != 2 || r.Voice.Calls.Completed != 0 {
		t.Fatalf("calls = %+v, want open=2 completed=0", r.Voice.Calls)
	}
}

func assertNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.05 {
		t.Fatalf("%s = %v, want ~%v", name, got, want)
	}
}
