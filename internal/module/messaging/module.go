package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/pkg/module"
)

const CloudRecipient = "cloud"

// DeviceExists reports whether a device id is registered (not cloud).
type DeviceExists func(id string) bool

// InCoverage reports whether a device is currently in coverage.
// Cloud should return true.
type InCoverage func(id string) bool

// Config wires the messaging module.
type Config struct {
	Store        *Store
	DeviceExists DeviceExists
	InCoverage   InCoverage
	Bus          *eventbus.Bus // optional; for SSE message events
}

// Module implements store-and-forward messaging.
type Module struct {
	store        *Store
	deviceExists DeviceExists
	inCoverage   InCoverage
	bus          *eventbus.Bus

	mu      sync.Mutex
	adapter module.IMSAdapter
	emitter module.Emitter
	ctx     context.Context
	cancel  context.CancelFunc
}

// New creates a messaging module. Call DeliverVia before release paths.
func New(cfg Config) *Module {
	store := cfg.Store
	if store == nil {
		store = NewStore()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Module{
		store:        store,
		deviceExists: cfg.DeviceExists,
		inCoverage:   cfg.InCoverage,
		bus:          cfg.Bus,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Close cancels in-flight IMS receipt callbacks.
func (m *Module) Close() {
	m.cancel()
}

// RegisterRoutes mounts messaging HTTP endpoints.
func (m *Module) RegisterRoutes(host module.RouteRegistrar) {
	host.Handle("POST /devices/{id}/messages", http.HandlerFunc(m.handleSend))
	host.Handle("GET /devices/{id}/messages", http.HandlerFunc(m.handleInbox))
	host.Handle("GET /messages/{mid}", http.HandlerFunc(m.handleGet))
}

// OnCoverageEvent releases queued messages when a recipient window opens.
func (m *Module) OnCoverageEvent(event eventbus.CoverageEvent) {
	if event.Kind != eventbus.KindWindowOpened {
		return
	}
	if event.DeviceID != "" {
		m.flushRecipient(event.DeviceID)
		return
	}
	// Replay and other single-observer publishers omit DeviceID.
	// Flush every recipient that still has queued mail.
	for _, to := range m.store.QueuedRecipients() {
		m.flushRecipient(to)
	}
}

// OnLinkState is a no-op for messaging.
func (m *Module) OnLinkState(eventbus.LinkStateEvent) {}

// DeliverVia sets the IMS adapter used on release.
func (m *Module) DeliverVia(adapter module.IMSAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adapter = adapter
}

// Emit sets the observability emitter (optional).
func (m *Module) Emit(emitter module.Emitter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitter = emitter
}

func (m *Module) flushRecipient(to string) {
	for _, msg := range m.store.PendingFor(to) {
		m.release(msg.ID)
	}
}

func (m *Module) release(id string) {
	m.mu.Lock()
	adapter := m.adapter
	m.mu.Unlock()
	if adapter == nil {
		return
	}
	msg, ok := m.store.ClaimQueued(id, time.Now().UTC())
	if !ok {
		return
	}
	m.publishStatus(id)

	_, err := adapter.Submit(m.ctx, module.OutboundMessage{
		To:   msg.To,
		Body: []byte(msg.Body),
	}, func(r module.Receipt) {
		switch r.Status {
		case module.StatusQueued, module.StatusInFlight:
			// ClaimQueued already moved to in_flight and published.
			return
		case module.StatusDelivered:
			m.store.SetStatus(id, StatusDelivered, r.At.UTC())
			m.publishStatus(id)
		case module.StatusFailed:
			m.store.SetStatus(id, StatusFailed, r.At.UTC())
			m.publishStatus(id)
		}
	})
	if err != nil {
		m.store.SetStatus(id, StatusFailed, time.Now().UTC())
		m.publishStatus(id)
	}
}

func (m *Module) publishStatus(id string) {
	msg, ok := m.store.Get(id)
	if !ok {
		return
	}
	// Lifecycle breadcrumb for operators (no body — keep CLI free of SOS text).
	fmt.Fprintf(os.Stderr, "ntnbox: message %s  %s → %s  %s\n",
		msg.ID, msg.From, msg.To, msg.Status)

	if m.bus == nil {
		return
	}
	// Omit body on the bus/SSE path to avoid leaking content via
	// Access-Control-Allow-Origin: * EventSource subscribers.
	m.bus.PublishMessage(eventbus.MessageEvent{
		ID:     msg.ID,
		From:   msg.From,
		To:     msg.To,
		Status: string(msg.Status),
		At:     time.Now().UTC(),
	})
}

func (m *Module) validRecipient(to string) bool {
	if to == CloudRecipient {
		return true
	}
	if m.deviceExists == nil {
		return false
	}
	return m.deviceExists(to)
}

func (m *Module) recipientReady(to string) bool {
	if to == CloudRecipient {
		return true
	}
	if m.inCoverage == nil {
		return false
	}
	return m.inCoverage(to)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var _ module.Module = (*Module)(nil)
