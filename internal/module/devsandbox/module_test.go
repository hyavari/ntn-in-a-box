package devsandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// mockShaper records calls to Apply and SetFullLoss.
type mockShaper struct {
	mu       sync.Mutex
	applies  []condition.LinkState
	fullLoss int
}

func (m *mockShaper) Apply(_ context.Context, state condition.LinkState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applies = append(m.applies, state)
	return nil
}

func (m *mockShaper) SetFullLoss(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fullLoss++
	return nil
}

func (m *mockShaper) applyCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.applies)
}

func (m *mockShaper) lastApply() condition.LinkState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applies[len(m.applies)-1]
}

func (m *mockShaper) fullLossCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fullLoss
}

// waitApply waits until Apply has been called at least n times.
func (m *mockShaper) waitApply(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if m.applyCount() >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for Apply count >= %d (got %d)", n, m.applyCount())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// waitFullLoss waits until SetFullLoss has been called at least n times.
func (m *mockShaper) waitFullLoss(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if m.fullLossCount() >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for SetFullLoss count >= %d (got %d)", n, m.fullLossCount())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestOnLinkStateAppliesWhileInCoverage(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	// Simulate coverage open.
	mod.OnCoverageEvent(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowOpened,
		At:   time.Now(),
	})

	state := condition.LinkState{
		DelayMs: 40, JitterMs: 5, LossPct: 0.2, BandwidthKbps: 20000,
	}
	mod.OnLinkState(eventbus.LinkStateEvent{State: state, At: time.Now()})

	shaper.waitApply(t, 1)
	got := shaper.lastApply()
	if got != state {
		t.Errorf("Apply state = %+v, want %+v", got, state)
	}
}

func TestOnLinkStateSkippedWhenOutOfCoverage(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	// Start out of coverage (initial state).
	mod.OnCoverageEvent(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowClosed,
		At:   time.Now(),
	})
	shaper.waitFullLoss(t, 1)

	state := condition.LinkState{
		DelayMs: 40, JitterMs: 5, LossPct: 0.2, BandwidthKbps: 20000,
	}
	mod.OnLinkState(eventbus.LinkStateEvent{State: state, At: time.Now()})

	// Give a moment for any spurious async call.
	time.Sleep(50 * time.Millisecond)

	// Apply should NOT be called (out of coverage), but state is stored.
	if shaper.applyCount() != 0 {
		t.Errorf("Apply called %d times while out of coverage, want 0", shaper.applyCount())
	}
}

func TestCoverageClosedSetsFullLoss(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	// Open then close.
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: time.Now()})
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowClosed, At: time.Now()})

	shaper.waitFullLoss(t, 1)
}

func TestCoverageOpenedResumesLastState(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	// Open coverage, send a link state, then close, then reopen.
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: time.Now()})

	state := condition.LinkState{
		DelayMs: 100, JitterMs: 10, LossPct: 5, BandwidthKbps: 2000,
	}
	mod.OnLinkState(eventbus.LinkStateEvent{State: state, At: time.Now()})
	shaper.waitApply(t, 1)

	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowClosed, At: time.Now()})
	shaper.waitFullLoss(t, 1)

	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: time.Now()})
	shaper.waitApply(t, 2)

	got := shaper.lastApply()
	if got != state {
		t.Errorf("on reopen, Apply state = %+v, want %+v", got, state)
	}
}

func TestCoverageOpenedNoStateYet(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	// Open coverage without any prior link state — should not call Apply.
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: time.Now()})

	if shaper.applyCount() != 0 {
		t.Errorf("Apply called %d times on open with no prior state, want 0", shaper.applyCount())
	}
}

func TestLookaheadEventsIgnored(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	// Lookahead events should not trigger any shaping change.
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowClosing, At: time.Now()})
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpening, At: time.Now()})

	if shaper.applyCount() != 0 {
		t.Errorf("Apply called %d times on lookahead events, want 0", shaper.applyCount())
	}
	if shaper.fullLossCount() != 0 {
		t.Errorf("SetFullLoss called %d times on lookahead events, want 0", shaper.fullLossCount())
	}
}

func TestStatusEndpointInCoverage(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	mux := http.NewServeMux()
	mod.RegisterRoutes(mux)

	// Simulate in-coverage with link state.
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: time.Now()})
	mod.OnLinkState(eventbus.LinkStateEvent{
		State: condition.LinkState{DelayMs: 40, JitterMs: 5, LossPct: 0.2, BandwidthKbps: 20000},
		At:    time.Now(),
	})
	shaper.waitApply(t, 1)

	req := httptest.NewRequest("GET", "/sandbox/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp statusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !resp.InCoverage {
		t.Error("InCoverage = false, want true")
	}
	if resp.DelayMs == nil || *resp.DelayMs != 40 {
		t.Errorf("DelayMs = %v, want 40", resp.DelayMs)
	}
}

func TestStatusEndpointOutOfCoverage(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	mux := http.NewServeMux()
	mod.RegisterRoutes(mux)

	// Start out of coverage.
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowClosed, At: time.Now()})
	shaper.waitFullLoss(t, 1)

	req := httptest.NewRequest("GET", "/sandbox/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp statusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.InCoverage {
		t.Error("InCoverage = true, want false")
	}
	if resp.DelayMs != nil {
		t.Errorf("DelayMs should be nil when out of coverage, got %v", resp.DelayMs)
	}
}

func TestEmitCalledOnTransitions(t *testing.T) {
	shaper := &mockShaper{}
	mod := New(shaper)

	var mu sync.Mutex
	var events []eventbus.ObservabilityEvent
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	bus.SubscribeObservability(func(ev eventbus.ObservabilityEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	mod.Emit(bus)

	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: time.Now()})
	mod.OnCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowClosed, At: time.Now()})

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 2 {
		t.Fatalf("got %d observability events, want 2", len(events))
	}
	if events[0].Name != "devsandbox.coverage_gained" {
		t.Errorf("event[0].Name = %q, want devsandbox.coverage_gained", events[0].Name)
	}
	if events[1].Name != "devsandbox.coverage_lost" {
		t.Errorf("event[1].Name = %q, want devsandbox.coverage_lost", events[1].Name)
	}
}
