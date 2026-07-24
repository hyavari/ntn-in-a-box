package report

import (
	"math"
	"testing"
	"time"

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
	assertNear(t, "in_sec", r.Coverage.InSec, 22)         // 0-10 + 18-30
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

func assertNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.05 {
		t.Fatalf("%s = %v, want ~%v", name, got, want)
	}
}
