package module

import (
	"net/http"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// Module is the capability module contract: the 5 hooks a module
// implements to plug into the kernel, matching the design doc exactly.
//
// RegisterRoutes, DeliverVia, and Emit are each called once, at
// registration time, handing the module a capability it can use
// later. OnCoverageEvent and OnLinkState are called repeatedly by the
// kernel as events occur, for the lifetime of the module.
//
// Concurrency: hook methods may be invoked concurrently — e.g.
// OnCoverageEvent and OnLinkState from different kernel goroutines, or
// an IMSAdapter's onReceipt callback firing on its own goroutine while
// a link-state update arrives. A Module implementation is responsible
// for synchronizing any state it keeps across its own hook methods;
// nothing here serializes calls into a single Module instance.
//
// If a module needs something outside these 5 hooks, that need
// belongs in the kernel instead of the module — see the design doc's
// kernel-vs-module placement rule.
type Module interface {
	// RegisterRoutes lets the module add its own REST/SDK endpoints to
	// the kernel's API host. Called once at registration time.
	RegisterRoutes(host RouteRegistrar)

	// OnCoverageEvent is called every time a coverage transition (or
	// its lookahead notice) fires.
	OnCoverageEvent(event eventbus.CoverageEvent)

	// OnLinkState is called every time a throttled link-state update
	// fires (see eventbus.LinkStateThrottle). Both OnCoverageEvent and
	// OnLinkState have signatures matching eventbus's handler types
	// exactly, so a Module's method values can be passed directly to
	// Bus.SubscribeCoverage / Bus.SubscribeLinkState with no adapter
	// glue code: bus.SubscribeCoverage(mod.OnCoverageEvent).
	OnLinkState(state eventbus.LinkStateEvent)

	// DeliverVia hands the module a handle to the kernel's pluggable
	// IMS Adapter (mock or real backend), for delivering messages to a
	// device. Called once at registration time; modules that don't
	// deliver messages (e.g. Dev Sandbox) can implement this as a
	// no-op.
	DeliverVia(adapter IMSAdapter)

	// Emit hands the module a handle for pushing metrics/events to
	// kernel observability. Called once at registration time.
	Emit(emitter Emitter)
}

// RouteRegistrar is the minimal capability a module needs from the
// kernel's API host to register its own HTTP routes.
//
// pkg/module deliberately does not import internal/kernel/apihost
// (which doesn't have a real implementation yet — see Task 9) to
// avoid coupling the module contract to a package that hasn't been
// designed. This interface matches the standard library's
// http.ServeMux.Handle signature, so whatever the real API host turns
// out to be can satisfy it trivially (e.g. by embedding a ServeMux).
type RouteRegistrar interface {
	Handle(pattern string, handler http.Handler)
}

// Emitter is the minimal capability a module needs to push metrics/
// events to kernel observability. eventbus.Bus already satisfies this
// interface via its own Emit method — no adapter needed.
type Emitter interface {
	Emit(event eventbus.ObservabilityEvent)
}
