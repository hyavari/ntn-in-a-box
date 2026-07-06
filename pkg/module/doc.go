// Package module defines the capability module contract: the 5 hooks
// (RegisterRoutes, OnCoverageEvent, OnLinkState, DeliverVia, Emit) that
// every capability module (Dev Sandbox, Messaging/Emergency, Service API)
// implements to plug into the kernel. This package holds only the
// interfaces/contract types; no concrete module lives here.
package module
