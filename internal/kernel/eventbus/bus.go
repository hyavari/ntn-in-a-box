package eventbus

import (
	"math"
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

type linkThrottleState struct {
	lastAt  time.Time
	lastVal condition.LinkState
	has     bool
}

// Bus is the kernel's in-process pub/sub: coverage transitions,
// throttled link-state updates, and a generic observability emit path.
// Safe for concurrent use.
type Bus struct {
	mu sync.Mutex

	coverageSubs      []*CoverageHandler
	linkStateSubs     []*LinkStateHandler
	observabilitySubs []*ObservabilityHandler
	positionSubs      []*SatellitePositionHandler
	messageSubs       []*MessageHandler

	throttle LinkStateThrottle
	// Per-device link-state throttle (empty DeviceID uses key "").
	linkByDevice map[string]*linkThrottleState
}

// New returns a Bus that throttles LinkState publication per throttle.
func New(throttle LinkStateThrottle) *Bus {
	return &Bus{
		throttle:     throttle,
		linkByDevice: make(map[string]*linkThrottleState),
	}
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
	b.PublishLinkStateEvent(LinkStateEvent{State: state, At: now})
}

// PublishLinkStateEvent is like PublishLinkState but includes DeviceID
// (and any other LinkStateEvent fields) on the published event.
// Throttle state is tracked per DeviceID so multi-device sessions do not
// suppress each other's linkstate updates.
func (b *Bus) PublishLinkStateEvent(ev LinkStateEvent) {
	if hasNaN(ev.State) {
		return
	}

	b.mu.Lock()
	st := b.linkByDevice[ev.DeviceID]
	if st == nil {
		st = &linkThrottleState{}
		b.linkByDevice[ev.DeviceID] = st
	}
	shouldPublish := !st.has ||
		ev.At.Sub(st.lastAt) >= b.throttle.Interval ||
		linkStateDelta(st.lastVal, ev.State) > b.throttle.DeltaThreshold
	if !shouldPublish {
		b.mu.Unlock()
		return
	}
	st.lastVal = ev.State
	st.lastAt = ev.At
	st.has = true
	subs := make([]*LinkStateHandler, len(b.linkStateSubs))
	copy(subs, b.linkStateSubs)
	b.mu.Unlock()

	for _, p := range subs {
		(*p)(ev)
	}
}

// SubscribeMessage registers h for MessageEvent publications.
func (b *Bus) SubscribeMessage(h MessageHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	ptr := &h
	b.messageSubs = append(b.messageSubs, ptr)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, p := range b.messageSubs {
			if p == ptr {
				b.messageSubs = append(b.messageSubs[:i], b.messageSubs[i+1:]...)
				return
			}
		}
	}
}

// PublishMessage notifies all message subscribers. Never throttled.
func (b *Bus) PublishMessage(ev MessageEvent) {
	b.mu.Lock()
	subs := make([]*MessageHandler, len(b.messageSubs))
	copy(subs, b.messageSubs)
	b.mu.Unlock()

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

// SubscribeSatellitePosition registers h to be called for every
// SatellitePositionEvent. Returns an unsubscribe function.
func (b *Bus) SubscribeSatellitePosition(h SatellitePositionHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	ptr := &h
	b.positionSubs = append(b.positionSubs, ptr)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, p := range b.positionSubs {
			if p == ptr {
				b.positionSubs = append(b.positionSubs[:i], b.positionSubs[i+1:]...)
				return
			}
		}
	}
}

// PublishSatellitePosition notifies all position subscribers.
// Never throttled (already rate-limited by the driver to ~1/s).
func (b *Bus) PublishSatellitePosition(ev SatellitePositionEvent) {
	b.mu.Lock()
	subs := make([]*SatellitePositionHandler, len(b.positionSubs))
	copy(subs, b.positionSubs)
	b.mu.Unlock()

	for _, p := range subs {
		(*p)(ev)
	}
}

func hasNaN(s condition.LinkState) bool {
	return math.IsNaN(s.DelayMs) || math.IsNaN(s.JitterMs) ||
		math.IsNaN(s.LossPct) || math.IsNaN(s.BandwidthKbps)
}
