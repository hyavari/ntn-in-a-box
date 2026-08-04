package devsandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/path"
	"github.com/hyavari/ntn-in-a-box/pkg/module"
)

// Shaper is the minimal interface the module needs to apply traffic
// shaping. Satisfied by *netem.Controller.
type Shaper interface {
	Apply(ctx context.Context, state condition.LinkState) error
	SetFullLoss(ctx context.Context) error
}

// Router switches the namespace default route between gateways.
type Router interface {
	SetDefaultVia(ctx context.Context, gatewayIP string) error
}

// shaperTimeout is the maximum time a shaper/route command (tc/ip) is
// allowed to run before being cancelled.
const shaperTimeout = 5 * time.Second

// shaperCmd represents a command to send to the shaper goroutine
// (single-path mode only).
type shaperCmd struct {
	fullLoss bool
	state    condition.LinkState
	gen      uint64
}

// Module is the Dev Sandbox capability module. It receives coverage
// and link-state events from the kernel's event bus and drives a
// Shaper (netem controller) to shape traffic accordingly.
//
// With terrestrial fallback enabled it reconciles the default route to
// the desired bearer on each coverage edge (shape → route → commit
// bearer / publish handover). Dual-path sat shaping and routing share
// one mutex so link-state applies cannot race fail-closed full loss.
//
// Safe for concurrent use — OnCoverageEvent and OnLinkState may be
// called from different goroutines per the module contract.
type Module struct {
	shaper   Shaper
	router   Router
	paths    *path.Manager
	bus      *eventbus.Bus
	deviceID string
	satGW    string
	terrGW   string

	shapeCh chan shaperCmd // nil in dual-path mode
	satMu   sync.Mutex     // dual-path: serializes sat shape + default route

	mu         sync.Mutex
	emitter    module.Emitter
	lastState  condition.LinkState
	hasState   bool
	inCoverage bool
	gen        uint64
}

// Compile-time check that Module satisfies pkg/module.Module.
var _ module.Module = (*Module)(nil)

// Options configures optional dual-path behavior. Terrestrial netem is
// owned by the wiring layer (setup once); the module only switches routes.
type Options struct {
	Router      Router
	Paths       *path.Manager
	Bus         *eventbus.Bus
	DeviceID    string
	SatGateway  string
	TerrGateway string
}

// New creates a Dev Sandbox module that drives the given satellite shaper.
func New(shaper Shaper) *Module {
	return NewWithOptions(shaper, Options{})
}

// NewWithOptions creates a module with optional terrestrial fallback wiring.
func NewWithOptions(shaper Shaper, opts Options) *Module {
	m := &Module{
		shaper:   shaper,
		router:   opts.Router,
		paths:    opts.Paths,
		bus:      opts.Bus,
		deviceID: opts.DeviceID,
		satGW:    opts.SatGateway,
		terrGW:   opts.TerrGateway,
	}
	if m.deviceID == "" {
		m.deviceID = "sandbox-0"
	}
	if !m.dualPath() {
		m.shapeCh = make(chan shaperCmd, 16)
		go m.shaperLoop()
	}
	return m
}

func (m *Module) dualPath() bool {
	return m.paths != nil && m.paths.Enabled() && m.router != nil
}

// InitBearer installs the initial default route for dual-path mode.
// No handover event is published. When starting on terrestrial, the
// satellite shaper is set to full loss (fail closed).
func (m *Module) InitBearer(inCoverage bool) error {
	if !m.dualPath() {
		return nil
	}
	m.satMu.Lock()
	defer m.satMu.Unlock()

	bearer := m.paths.Init(inCoverage)
	if bearer == path.BearerTerrestrial {
		m.applySatFullLossLocked()
	}
	gw := path.GatewayFor(bearer, m.satGW, m.terrGW)
	ctx, cancel := context.WithTimeout(context.Background(), shaperTimeout)
	defer cancel()
	if err := m.router.SetDefaultVia(ctx, gw); err != nil {
		return fmt.Errorf("initial default route via %s: %w", gw, err)
	}
	m.mu.Lock()
	m.inCoverage = inCoverage
	m.mu.Unlock()
	return nil
}

// SelectedBearer returns the current bearer when dual-path is enabled.
func (m *Module) SelectedBearer() string {
	if m.paths == nil {
		return ""
	}
	return m.paths.Current()
}

func (m *Module) shaperLoop() {
	var currentGen uint64
	for cmd := range m.shapeCh {
		if cmd.gen < currentGen {
			continue
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

func (m *Module) applySatFullLossLocked() {
	ctx, cancel := context.WithTimeout(context.Background(), shaperTimeout)
	defer cancel()
	_ = m.shaper.SetFullLoss(ctx)
}

func (m *Module) applySatStateLocked(state condition.LinkState) {
	ctx, cancel := context.WithTimeout(context.Background(), shaperTimeout)
	defer cancel()
	_ = m.shaper.Apply(ctx, state)
}

// RegisterRoutes adds the sandbox's HTTP endpoints to the API host.
func (m *Module) RegisterRoutes(host module.RouteRegistrar) {
	host.Handle("GET /sandbox/status", http.HandlerFunc(m.handleStatus))
}

// OnCoverageEvent reacts to coverage transitions.
//   - single-path: window_closed → 100% loss; window_opened → resume curves
//   - dual-path: reconcile default route to desired bearer (sync)
func (m *Module) OnCoverageEvent(ev eventbus.CoverageEvent) {
	if ev.DeviceID != "" && ev.DeviceID != m.deviceID {
		return
	}

	switch ev.Kind {
	case eventbus.KindWindowClosed, eventbus.KindWindowOpened:
		if m.dualPath() {
			m.reconcileBearer(ev)
			return
		}
		m.handleSinglePathCoverage(ev)

	case eventbus.KindWindowOpening, eventbus.KindWindowClosing:
		// lookahead only
	}
}

func (m *Module) handleSinglePathCoverage(ev eventbus.CoverageEvent) {
	if ev.Kind == eventbus.KindWindowClosed {
		m.mu.Lock()
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
		return
	}

	m.mu.Lock()
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
}

// reconcileBearer shapes the sat path, switches the default route, then
// commits bearer state and publishes handover. satMu serializes this with
// OnLinkState so SelectedBearer and the live qdisc stay aligned.
func (m *Module) reconcileBearer(ev eventbus.CoverageEvent) {
	inCoverage := ev.InCoverage
	switch ev.Kind {
	case eventbus.KindWindowOpened:
		inCoverage = true
	case eventbus.KindWindowClosed:
		inCoverage = false
	case eventbus.KindWindowOpening, eventbus.KindWindowClosing:
		// keep ev.InCoverage
	}
	inBlockage := ev.InBlockage && !inCoverage
	want := path.Desired(inCoverage)

	m.satMu.Lock()
	defer m.satMu.Unlock()

	current := m.paths.Current()

	m.mu.Lock()
	m.inCoverage = inCoverage
	state := m.lastState
	hasState := m.hasState
	emitter := m.emitter
	m.mu.Unlock()

	if want == current {
		_ = m.paths.TransitionTo(want, inBlockage) // refresh blockage memory
		if want == path.BearerSatellite && hasState {
			m.applySatStateLocked(state)
		} else if want == path.BearerTerrestrial {
			m.applySatFullLossLocked()
		}
		return
	}

	// Shape before route: fail-closed full loss when leaving sat; restore
	// impairments before returning to sat so default-routed traffic is not
	// black-holed on a 100% loss qdisc.
	if want == path.BearerTerrestrial {
		m.applySatFullLossLocked()
	} else if hasState {
		m.applySatStateLocked(state)
	}

	gw := path.GatewayFor(want, m.satGW, m.terrGW)
	ctx, cancel := context.WithTimeout(context.Background(), shaperTimeout)
	err := m.router.SetDefaultVia(ctx, gw)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ntnbox: route switch via %s: %v (bearer unchanged: %s)\n", gw, err, current)
		return
	}

	tr := m.paths.TransitionTo(want, inBlockage)
	if tr == nil {
		return
	}
	if m.bus != nil {
		m.bus.PublishHandover(eventbus.HandoverEvent{
			DeviceID: m.deviceID,
			From:     tr.From,
			To:       tr.To,
			Reason:   tr.Reason,
			At:       ev.At,
		})
	}
	if emitter != nil {
		emitter.Emit(eventbus.ObservabilityEvent{
			Name: "devsandbox.handover",
			Fields: map[string]any{
				"from":   tr.From,
				"to":     tr.To,
				"reason": tr.Reason,
				"at":     ev.At.Format(time.RFC3339),
			},
			At: ev.At,
		})
	}
}

// OnLinkState applies updated impairment values to the satellite shaper
// while satellite coverage is up.
func (m *Module) OnLinkState(ev eventbus.LinkStateEvent) {
	if ev.DeviceID != "" && ev.DeviceID != m.deviceID {
		return
	}
	m.mu.Lock()
	m.lastState = ev.State
	m.hasState = true
	inCoverage := m.inCoverage
	gen := m.gen
	m.mu.Unlock()

	if !inCoverage {
		return
	}
	if m.dualPath() {
		m.satMu.Lock()
		defer m.satMu.Unlock()
		// Re-check: a reconcile may have left satellite while we waited.
		if m.paths.Current() != path.BearerSatellite {
			return
		}
		m.applySatStateLocked(ev.State)
		return
	}
	m.shapeCh <- shaperCmd{state: ev.State, gen: gen}
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
	InCoverage     bool     `json:"in_coverage"`
	SelectedBearer string   `json:"selected_bearer,omitempty"`
	DelayMs        *float64 `json:"delay_ms,omitempty"`
	JitterMs       *float64 `json:"jitter_ms,omitempty"`
	LossPct        *float64 `json:"loss_pct,omitempty"`
	BandwidthKbps  *float64 `json:"bandwidth_kbps,omitempty"`
}

func (m *Module) handleStatus(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	resp := statusResponse{InCoverage: m.inCoverage}
	if bearer := m.SelectedBearer(); bearer != "" {
		resp.SelectedBearer = bearer
	}
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
