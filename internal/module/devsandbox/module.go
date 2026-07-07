package devsandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/pkg/module"
)

// Shaper is the minimal interface the module needs to apply traffic
// shaping. Satisfied by *netem.Controller.
type Shaper interface {
	Apply(ctx context.Context, state condition.LinkState) error
	SetFullLoss(ctx context.Context) error
}

// Module is the Dev Sandbox capability module. It receives coverage
// and link-state events from the kernel's event bus and drives a
// Shaper (netem controller) to shape traffic accordingly.
//
// Safe for concurrent use — OnCoverageEvent and OnLinkState may be
// called from different goroutines per the module contract.
type Module struct {
	shaper  Shaper
	emitter module.Emitter

	mu         sync.Mutex
	lastState  condition.LinkState
	hasState   bool
	inCoverage bool
}

// Compile-time check that Module satisfies pkg/module.Module.
var _ module.Module = (*Module)(nil)

// New creates a Dev Sandbox module that drives the given shaper.
func New(shaper Shaper) *Module {
	return &Module{shaper: shaper}
}

// RegisterRoutes adds the sandbox's HTTP endpoints to the API host.
func (m *Module) RegisterRoutes(host module.RouteRegistrar) {
	host.Handle("GET /sandbox/status", http.HandlerFunc(m.handleStatus))
}

// OnCoverageEvent reacts to coverage transitions.
//   - window_closed: set 100% loss (simulate link disappearing)
//   - window_opened: resume curve-driven shaping with last known state
func (m *Module) OnCoverageEvent(ev eventbus.CoverageEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch ev.Kind {
	case eventbus.KindWindowClosed:
		m.inCoverage = false
		_ = m.shaper.SetFullLoss(context.Background())
		m.emitEvent("coverage_lost", ev.At)

	case eventbus.KindWindowOpened:
		m.inCoverage = true
		if m.hasState {
			_ = m.shaper.Apply(context.Background(), m.lastState)
		}
		m.emitEvent("coverage_gained", ev.At)
	}
}

// OnLinkState applies updated impairment values to the shaper.
func (m *Module) OnLinkState(ev eventbus.LinkStateEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastState = ev.State
	m.hasState = true

	if m.inCoverage {
		_ = m.shaper.Apply(context.Background(), ev.State)
	}
}

// DeliverVia is a no-op — Dev Sandbox doesn't deliver messages.
func (m *Module) DeliverVia(module.IMSAdapter) {}

// Emit stores the emitter for pushing observability events.
func (m *Module) Emit(emitter module.Emitter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitter = emitter
}

func (m *Module) emitEvent(name string, at time.Time) {
	if m.emitter == nil {
		return
	}
	m.emitter.Emit(eventbus.ObservabilityEvent{
		Name:   "devsandbox." + name,
		Fields: map[string]any{"at": at.Format(time.RFC3339)},
		At:     at,
	})
}

type statusResponse struct {
	InCoverage    bool     `json:"in_coverage"`
	DelayMs       *float64 `json:"delay_ms,omitempty"`
	JitterMs      *float64 `json:"jitter_ms,omitempty"`
	LossPct       *float64 `json:"loss_pct,omitempty"`
	BandwidthKbps *float64 `json:"bandwidth_kbps,omitempty"`
}

func (m *Module) handleStatus(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	resp := statusResponse{InCoverage: m.inCoverage}
	if m.hasState && m.inCoverage {
		resp.DelayMs = &m.lastState.DelayMs
		resp.JitterMs = &m.lastState.JitterMs
		resp.LossPct = &m.lastState.LossPct
		resp.BandwidthKbps = &m.lastState.BandwidthKbps
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
