package module

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// recordingModule captures every hook call, to prove wiring actually
// invokes and passes through each hook rather than just type-checking.
type recordingModule struct {
	coverageCalls  int
	linkStateCalls int
	registrar      RouteRegistrar
	adapter        IMSAdapter
	emitter        Emitter
}

func (m *recordingModule) RegisterRoutes(host RouteRegistrar)     { m.registrar = host }
func (m *recordingModule) OnCoverageEvent(eventbus.CoverageEvent) { m.coverageCalls++ }
func (m *recordingModule) OnLinkState(eventbus.LinkStateEvent)    { m.linkStateCalls++ }
func (m *recordingModule) DeliverVia(adapter IMSAdapter)          { m.adapter = adapter }
func (m *recordingModule) Emit(emitter Emitter)                   { m.emitter = emitter }

var _ Module = &recordingModule{}

func TestModule_OnCoverageEventWiresDirectlyToEventBus(t *testing.T) {
	mod := &recordingModule{}
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	// The actual point of this test: a Module's method values must be
	// directly assignable as eventbus handlers, with no adapter/
	// wrapper glue code required.
	bus.SubscribeCoverage(mod.OnCoverageEvent)
	bus.SubscribeLinkState(mod.OnLinkState)

	bus.PublishCoverageEvent(eventbus.CoverageEvent{Kind: eventbus.KindWindowOpened, At: time.Now()})
	bus.PublishLinkState(condition.LinkState{DelayMs: 40}, time.Now())

	if mod.coverageCalls != 1 {
		t.Errorf("coverageCalls = %d, want 1", mod.coverageCalls)
	}
	if mod.linkStateCalls != 1 {
		t.Errorf("linkStateCalls = %d, want 1", mod.linkStateCalls)
	}
}

type fakeRegistrar struct {
	handled []string
}

func (f *fakeRegistrar) Handle(pattern string, _ http.Handler) {
	f.handled = append(f.handled, pattern)
}

type fakeIMSAdapter struct {
	submitted []OutboundMessage
}

func (f *fakeIMSAdapter) Submit(_ context.Context, msg OutboundMessage, onReceipt ReceiptFunc) (MessageID, error) {
	f.submitted = append(f.submitted, msg)
	id := MessageID("fake-1")
	if onReceipt != nil {
		onReceipt(Receipt{MessageID: id, Status: StatusQueued, At: time.Now()})
	}
	return id, nil
}

func TestModule_RegistrationHooksReceiveUsableHandles(t *testing.T) {
	mod := &recordingModule{}
	reg := &fakeRegistrar{}
	adapter := &fakeIMSAdapter{}
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle) // satisfies Emitter

	mod.RegisterRoutes(reg)
	mod.DeliverVia(adapter)
	mod.Emit(bus)

	if mod.registrar == nil {
		t.Error("expected RegisterRoutes to retain a usable RouteRegistrar")
	}
	mod.registrar.Handle("/test", nil)
	if len(reg.handled) != 1 || reg.handled[0] != "/test" {
		t.Errorf("expected the retained registrar to actually register routes, got %v", reg.handled)
	}

	if mod.adapter == nil {
		t.Error("expected DeliverVia to retain a usable IMSAdapter")
	}
	var receipts []Receipt
	if _, err := mod.adapter.Submit(context.Background(), OutboundMessage{To: "device-1", Body: []byte("hi")}, func(r Receipt) {
		receipts = append(receipts, r)
	}); err != nil {
		t.Fatalf("Submit returned an error: %v", err)
	}
	if len(receipts) != 1 || receipts[0].Status != StatusQueued {
		t.Errorf("expected a queued receipt, got %v", receipts)
	}

	if mod.emitter == nil {
		t.Fatal("expected Emit to retain a usable Emitter")
	}
	var observed []eventbus.ObservabilityEvent
	bus.SubscribeObservability(func(ev eventbus.ObservabilityEvent) { observed = append(observed, ev) })
	mod.emitter.Emit(eventbus.ObservabilityEvent{Name: "test_event", At: time.Now()})
	if len(observed) != 1 || observed[0].Name != "test_event" {
		t.Errorf("expected the retained emitter to actually push observability events, got %v", observed)
	}
}

func TestNoOpModule_SatisfiesModuleAndDoesNothing(t *testing.T) {
	mod := NoOpModule{}
	reg := &fakeRegistrar{}

	// Must not panic, including with a nil adapter/emitter, since
	// NoOpModule ignores every argument.
	mod.RegisterRoutes(reg)
	mod.OnCoverageEvent(eventbus.CoverageEvent{})
	mod.OnLinkState(eventbus.LinkStateEvent{})
	mod.DeliverVia(nil)
	mod.Emit(nil)

	if len(reg.handled) != 0 {
		t.Errorf("expected NoOpModule to register no routes, got %v", reg.handled)
	}
}
