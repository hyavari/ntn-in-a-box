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

// shaperTimeout is the maximum time a shaper command (tc/ip) is
// allowed to run before being cancelled.
const shaperTimeout = 5 * time.Second

// shaperCmd represents a command to send to the shaper goroutine.
type shaperCmd struct {
	fullLoss bool
	state    condition.LinkState
	gen      uint64 // generation: stale commands are dropped
}

// Module is the Dev Sandbox capability module. It receives coverage
// and link-state events from the kernel's event bus and drives a
// Shaper (netem controller) to shape traffic accordingly.
//
// Safe for concurrent use — OnCoverageEvent and OnLinkState may be
// called from different goroutines per the module contract.
//
// Shaper commands are serialized through a channel to prevent
// interleaving (e.g. a late Apply undoing a SetFullLoss).
type Module struct {
	shaper  Shaper
	shapeCh chan shaperCmd

	mu         sync.Mutex
	emitter    module.Emitter
	lastState  condition.LinkState
	hasState   bool
	inCoverage bool
	gen        uint64 // incremented on coverage transitions
}

// Compile-time check that Module satisfies pkg/module.Module.
var _ module.Module = (*Module)(nil)

// New creates a Dev Sandbox module that drives the given shaper.
// A background goroutine processes shaper commands sequentially.
func New(shaper Shaper) *Module {
	m := &Module{
		shaper:  shaper,
		shapeCh: make(chan shaperCmd, 16),
	}
	go m.shaperLoop()
	return m
}

// shaperLoop processes shaper commands sequentially. Stale commands
// (generation < current) are dropped to prevent interleaving.
func (m *Module) shaperLoop() {
	var currentGen uint64
	for cmd := range m.shapeCh {
		if cmd.gen < currentGen {
			continue // stale command, skip
		}
		currentGen = cmd.gen
		ctx, cancel := context.WithTimeout(context.Background(), shaperTimeout)
		if cmd.fullLoss {
			_ = m.shaper.SetFullLoss(ctx)
		} else {
			_ = m.shaper.Apply(ctx, cmd.state)
		}
		cancel()
	}
}

// RegisterRoutes adds the sandbox's HTTP endpoints to the API host.
func (m *Module) RegisterRoutes(host module.RouteRegistrar) {
	host.Handle("GET /sandbox/status", http.HandlerFunc(m.handleStatus))
}

// OnCoverageEvent reacts to coverage transitions.
//   - window_closed: set 100% loss (simulate link disappearing)
//   - window_opened: resume curve-driven shaping with last known state
//
// Only the primary sandbox namespace is shaped; peer DeviceIDs are ignored
// so multi-device messaging drivers cannot flap netem for sandbox-0.
func (m *Module) OnCoverageEvent(ev eventbus.CoverageEvent) {
	if ev.DeviceID != "" && ev.DeviceID != "sandbox-0" {
		return
	}
	m.mu.Lock()

	switch ev.Kind {
	case eventbus.KindWindowClosed:
		m.inCoverage = false
		m.gen++
		gen := m.gen
		emitter := m.emitter
		m.mu.Unlock()

		m.shapeCh <- shaperCmd{fullLoss: true, gen: gen}
		if emitter != nil {
			emitter.Emit(eventbus.ObservabilityEvent{
				Name:   "devsandbox.coverage_lost",
				Fields: map[string]any{"at": ev.At.Format(time.RFC3339)},
				At:     ev.At,
			})
		}

	case eventbus.KindWindowOpened:
		m.inCoverage = true
		m.gen++
		gen := m.gen
		state := m.lastState
		hasState := m.hasState
		emitter := m.emitter
		m.mu.Unlock()

		if hasState {
			m.shapeCh <- shaperCmd{state: state, gen: gen}
		}
		if emitter != nil {
			emitter.Emit(eventbus.ObservabilityEvent{
				Name:   "devsandbox.coverage_gained",
				Fields: map[string]any{"at": ev.At.Format(time.RFC3339)},
				At:     ev.At,
			})
		}

	case eventbus.KindWindowOpening, eventbus.KindWindowClosing:
		m.mu.Unlock()
	default:
		m.mu.Unlock()
	}
}

// OnLinkState applies updated impairment values to the shaper.
func (m *Module) OnLinkState(ev eventbus.LinkStateEvent) {
	if ev.DeviceID != "" && ev.DeviceID != "sandbox-0" {
		return
	}
	m.mu.Lock()
	m.lastState = ev.State
	m.hasState = true
	inCoverage := m.inCoverage
	gen := m.gen
	m.mu.Unlock()

	if inCoverage {
		m.shapeCh <- shaperCmd{state: ev.State, gen: gen}
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
		d := m.lastState.DelayMs
		j := m.lastState.JitterMs
		l := m.lastState.LossPct
		b := m.lastState.BandwidthKbps
		resp.DelayMs = &d
		resp.JitterMs = &j
		resp.LossPct = &l
		resp.BandwidthKbps = &b
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
