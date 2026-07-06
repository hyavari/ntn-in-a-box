package module

import "github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"

// NoOpModule is a trivial reference implementation of Module used only
// to confirm that kernel wiring code can register and drive a module
// without errors. It is not a real capability module — Dev Sandbox
// (Step 1) is the first real one.
type NoOpModule struct{}

// Compile-time check that NoOpModule satisfies Module.
var _ Module = NoOpModule{}

// RegisterRoutes is a no-op.
func (NoOpModule) RegisterRoutes(RouteRegistrar) {}

// OnCoverageEvent is a no-op.
func (NoOpModule) OnCoverageEvent(eventbus.CoverageEvent) {}

// OnLinkState is a no-op.
func (NoOpModule) OnLinkState(eventbus.LinkStateEvent) {}

// DeliverVia is a no-op.
func (NoOpModule) DeliverVia(IMSAdapter) {}

// Emit is a no-op.
func (NoOpModule) Emit(Emitter) {}
