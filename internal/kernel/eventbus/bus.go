package eventbus

import (
	"math"
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

// Bus is the kernel's in-process pub/sub: coverage transitions,
// throttled link-state updates, and a generic observability emit path.
// Safe for concurrent use.
//
// Scope: a Bus holds throttle state (last published LinkState/time)
// for a single link-state stream. It is not safe to share one Bus
// across multiple devices' link-state updates — their deltas would be
// compared against each other, which is meaningless. Callers with
// multiple devices (see the device registry, Task 7) should create one
// Bus per device/stream rather than multiplexing device IDs through a
// single Bus.
type Bus struct {
	mu sync.Mutex

	coverageSubs      []*CoverageHandler
	linkStateSubs     []*LinkStateHandler
	observabilitySubs []*ObservabilityHandler

	throttle     LinkStateThrottle
	lastLinkAt   time.Time
	lastLinkVal  condition.LinkState
	hasLinkState bool
}

// New returns a Bus that throttles LinkState publication per throttle.
func New(throttle LinkStateThrottle) *Bus {
	return &Bus{throttle: throttle}
}

// SubscribeCoverage registers h to be called for every CoverageEvent.
// Returns an unsubscribe function that removes the handler.
func (b *Bus) SubscribeCoverage(h CoverageHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	ptr := &h
	b.coverageSubs = append(b.coverageSubs, ptr)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, p := range b.coverageSubs {
			if p == ptr {
				b.coverageSubs = append(b.coverageSubs[:i], b.coverageSubs[i+1:]...)
				return
			}
		}
	}
}

// SubscribeLinkState registers h to be called for every LinkStateEvent
// that passes the throttle. Returns an unsubscribe function.
func (b *Bus) SubscribeLinkState(h LinkStateHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	ptr := &h
	b.linkStateSubs = append(b.linkStateSubs, ptr)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, p := range b.linkStateSubs {
			if p == ptr {
				b.linkStateSubs = append(b.linkStateSubs[:i], b.linkStateSubs[i+1:]...)
				return
			}
		}
	}
}

// SubscribeObservability registers h to be called for every
// ObservabilityEvent. Returns an unsubscribe function.
func (b *Bus) SubscribeObservability(h ObservabilityHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	ptr := &h
	b.observabilitySubs = append(b.observabilitySubs, ptr)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, p := range b.observabilitySubs {
			if p == ptr {
				b.observabilitySubs = append(b.observabilitySubs[:i], b.observabilitySubs[i+1:]...)
				return
			}
		}
	}
}

// PublishCoverageEvent notifies all coverage subscribers. Never
// throttled — coverage transitions are discrete and every one matters.
func (b *Bus) PublishCoverageEvent(ev CoverageEvent) {
	b.mu.Lock()
	subs := make([]*CoverageHandler, len(b.coverageSubs))
	copy(subs, b.coverageSubs)
	b.mu.Unlock()

	for _, p := range subs {
		(*p)(ev)
	}
}

// PublishLinkState offers a candidate LinkState for publication at
// instant now. It is actually delivered to subscribers only if it
// passes the throttle (see LinkStateThrottle); otherwise this call is
// a silent no-op. The very first call always publishes.
//
// state must not contain NaN fields — callers should only publish
// while in coverage (see LinkStateEvent's doc comment); use
// PublishCoverageEvent for transitions. A NaN field is treated as a
// contract violation and silently dropped rather than published or
// recorded as the "last published state": letting a NaN through would
// permanently poison every future delta comparison (any comparison
// against NaN is false), silently degrading the bus to heartbeat-only
// publishing from then on.
func (b *Bus) PublishLinkState(state condition.LinkState, now time.Time) {
	if hasNaN(state) {
		return
	}

	b.mu.Lock()
	shouldPublish := !b.hasLinkState ||
		now.Sub(b.lastLinkAt) >= b.throttle.Interval ||
		linkStateDelta(b.lastLinkVal, state) > b.throttle.DeltaThreshold
	if !shouldPublish {
		b.mu.Unlock()
		return
	}
	b.lastLinkVal = state
	b.lastLinkAt = now
	b.hasLinkState = true
	subs := make([]*LinkStateHandler, len(b.linkStateSubs))
	copy(subs, b.linkStateSubs)
	b.mu.Unlock()

	ev := LinkStateEvent{State: state, At: now}
	for _, p := range subs {
		(*p)(ev)
	}
}

// Emit notifies all observability subscribers. Never throttled.
func (b *Bus) Emit(ev ObservabilityEvent) {
	b.mu.Lock()
	subs := make([]*ObservabilityHandler, len(b.observabilitySubs))
	copy(subs, b.observabilitySubs)
	b.mu.Unlock()

	for _, p := range subs {
		(*p)(ev)
	}
}

func hasNaN(s condition.LinkState) bool {
	return math.IsNaN(s.DelayMs) || math.IsNaN(s.JitterMs) ||
		math.IsNaN(s.LossPct) || math.IsNaN(s.BandwidthKbps)
}
