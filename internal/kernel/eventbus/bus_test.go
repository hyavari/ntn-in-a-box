package eventbus

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

var testStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestPublishLinkState_FirstCallAlwaysPublishes(t *testing.T) {
	b := New(LinkStateThrottle{Interval: time.Hour, DeltaThreshold: 0.05})
	var received []LinkStateEvent
	b.SubscribeLinkState(func(ev LinkStateEvent) { received = append(received, ev) })

	b.PublishLinkState(condition.LinkState{DelayMs: 40}, testStart)

	if len(received) != 1 {
		t.Fatalf("expected the first PublishLinkState call to always publish, got %d events", len(received))
	}
}

func TestPublishLinkState_TinyChangesWithinIntervalAreSuppressed(t *testing.T) {
	b := New(LinkStateThrottle{Interval: time.Second, DeltaThreshold: 0.05})
	var received []LinkStateEvent
	b.SubscribeLinkState(func(ev LinkStateEvent) { received = append(received, ev) })

	b.PublishLinkState(condition.LinkState{DelayMs: 40}, testStart)
	// 1% change, well under the 5% threshold, and well within the 1s interval.
	b.PublishLinkState(condition.LinkState{DelayMs: 40.4}, testStart.Add(100*time.Millisecond))
	b.PublishLinkState(condition.LinkState{DelayMs: 40.8}, testStart.Add(200*time.Millisecond))

	if len(received) != 1 {
		t.Fatalf("expected tiny changes within the interval to be suppressed, got %d events (want 1, just the first)", len(received))
	}
}

func TestPublishLinkState_BigJumpAlwaysPublishes(t *testing.T) {
	b := New(LinkStateThrottle{Interval: time.Hour, DeltaThreshold: 0.05})
	var received []LinkStateEvent
	b.SubscribeLinkState(func(ev LinkStateEvent) { received = append(received, ev) })

	b.PublishLinkState(condition.LinkState{DelayMs: 40}, testStart)
	// A large jump (150ms vs 40ms), well within the 1-hour interval, but
	// far exceeding the 5% delta threshold — must publish regardless of
	// how little time has passed.
	b.PublishLinkState(condition.LinkState{DelayMs: 150}, testStart.Add(time.Millisecond))

	if len(received) != 2 {
		t.Fatalf("expected a large delta to bypass the interval throttle, got %d events, want 2", len(received))
	}
}

func TestPublishLinkState_PeriodicHeartbeatWhenStatic(t *testing.T) {
	b := New(LinkStateThrottle{Interval: 100 * time.Millisecond, DeltaThreshold: 0.05})
	var received []LinkStateEvent
	b.SubscribeLinkState(func(ev LinkStateEvent) { received = append(received, ev) })

	state := condition.LinkState{DelayMs: 40, JitterMs: 5, LossPct: 0.1, BandwidthKbps: 20000}
	// Same, unchanging state published every 100ms across 300ms: expect
	// a heartbeat at each interval boundary even though nothing changed.
	b.PublishLinkState(state, testStart)
	b.PublishLinkState(state, testStart.Add(100*time.Millisecond))
	b.PublishLinkState(state, testStart.Add(200*time.Millisecond))
	b.PublishLinkState(state, testStart.Add(300*time.Millisecond))

	if len(received) != 4 {
		t.Fatalf("expected a heartbeat publish every interval for a static value, got %d events, want 4", len(received))
	}
}

func TestPublishLinkState_SuppressedThenResumesAfterInterval(t *testing.T) {
	b := New(LinkStateThrottle{Interval: 100 * time.Millisecond, DeltaThreshold: 0.05})
	var received []LinkStateEvent
	b.SubscribeLinkState(func(ev LinkStateEvent) { received = append(received, ev) })

	b.PublishLinkState(condition.LinkState{DelayMs: 40}, testStart)                             // published (first)
	b.PublishLinkState(condition.LinkState{DelayMs: 40.1}, testStart.Add(50*time.Millisecond))  // suppressed: tiny delta, within interval
	b.PublishLinkState(condition.LinkState{DelayMs: 40.1}, testStart.Add(150*time.Millisecond)) // published: interval elapsed (heartbeat)

	if len(received) != 2 {
		t.Fatalf("got %d events, want 2 (first publish, then heartbeat after the interval elapses)", len(received))
	}
}

func TestPublishCoverageEvent_NeverThrottled(t *testing.T) {
	b := New(DefaultLinkStateThrottle)
	var received []CoverageEvent
	b.SubscribeCoverage(func(ev CoverageEvent) { received = append(received, ev) })

	for i := 0; i < 5; i++ {
		b.PublishCoverageEvent(CoverageEvent{Kind: KindWindowOpened, At: testStart})
	}

	if len(received) != 5 {
		t.Fatalf("expected every CoverageEvent to be delivered (no throttling), got %d, want 5", len(received))
	}
}

func TestPublishCoverageEvent_MultipleSubscribers(t *testing.T) {
	b := New(DefaultLinkStateThrottle)
	var a, c int
	b.SubscribeCoverage(func(_ CoverageEvent) { a++ })
	b.SubscribeCoverage(func(_ CoverageEvent) { c++ })

	b.PublishCoverageEvent(CoverageEvent{Kind: KindWindowClosing, At: testStart})

	if a != 1 || c != 1 {
		t.Fatalf("expected both subscribers to be notified once, got a=%d c=%d", a, c)
	}
}

func TestEmit_DeliversToObservabilitySubscribers(t *testing.T) {
	b := New(DefaultLinkStateThrottle)
	var received []ObservabilityEvent
	b.SubscribeObservability(func(ev ObservabilityEvent) { received = append(received, ev) })

	b.Emit(ObservabilityEvent{Name: "message_queued", Fields: map[string]any{"id": "abc"}, At: testStart})

	if len(received) != 1 || received[0].Name != "message_queued" {
		t.Fatalf("expected the observability event to be delivered, got %+v", received)
	}
}

func TestPublishLinkState_NaNStateIsDroppedNotPublished(t *testing.T) {
	b := New(LinkStateThrottle{Interval: time.Hour, DeltaThreshold: 0.05})
	var received []LinkStateEvent
	b.SubscribeLinkState(func(ev LinkStateEvent) { received = append(received, ev) })

	// Simulates a caller mistakenly publishing an out-of-coverage
	// (NaN) LinkState, violating the documented precondition.
	nan := math.NaN()
	b.PublishLinkState(condition.LinkState{DelayMs: nan, JitterMs: nan, LossPct: nan, BandwidthKbps: nan}, testStart)
	if len(received) != 0 {
		t.Fatalf("expected a NaN LinkState to be silently dropped, got %d events", len(received))
	}

	// A valid publish afterward must still work normally — a dropped
	// NaN must not have poisoned "hasLinkState" into thinking a first
	// publish already happened.
	b.PublishLinkState(condition.LinkState{DelayMs: 40}, testStart.Add(time.Second))
	if len(received) != 1 {
		t.Fatalf("expected the next valid publish to succeed, got %d events, want 1", len(received))
	}
}

func TestPublishLinkState_SingleFieldChangeIsNotHiddenByStaticFields(t *testing.T) {
	b := New(LinkStateThrottle{Interval: time.Hour, DeltaThreshold: 0.05})
	var received []LinkStateEvent
	b.SubscribeLinkState(func(ev LinkStateEvent) { received = append(received, ev) })

	b.PublishLinkState(condition.LinkState{DelayMs: 40, JitterMs: 5, LossPct: 0.1, BandwidthKbps: 20000}, testStart)
	// Only BandwidthKbps changes significantly (20000 -> 2000, a 90%
	// drop); the other three fields are unchanged. max() over all four
	// relative deltas must still catch it.
	b.PublishLinkState(condition.LinkState{DelayMs: 40, JitterMs: 5, LossPct: 0.1, BandwidthKbps: 2000}, testStart.Add(time.Millisecond))

	if len(received) != 2 {
		t.Fatalf("expected a significant change in a single field to publish, got %d events, want 2", len(received))
	}
}

func TestPublishLinkState_ConcurrentPublishAndSubscribeIsRaceFree(t *testing.T) {
	b := New(LinkStateThrottle{Interval: time.Millisecond, DeltaThreshold: 0.05})

	var mu sync.Mutex
	count := 0
	b.SubscribeLinkState(func(_ LinkStateEvent) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				b.PublishLinkState(condition.LinkState{DelayMs: float64(i*100 + j)}, testStart.Add(time.Duration(j)*time.Millisecond))
			}
		}(i)
	}
	// Concurrently add another subscriber and publish coverage/
	// observability events, to exercise every public method under
	// concurrent access (run with `go test -race` to verify).
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.SubscribeLinkState(func(_ LinkStateEvent) {})
		for j := 0; j < 20; j++ {
			b.PublishCoverageEvent(CoverageEvent{Kind: KindWindowOpened, At: testStart})
			b.Emit(ObservabilityEvent{Name: "tick", At: testStart})
		}
	}()
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if count == 0 {
		t.Fatal("expected at least some publishes to be delivered")
	}
}
