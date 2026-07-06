// Package apihost implements the kernel's minimal HTTP server: health
// check, profile listing/lookup, device registration, and current
// condition state (coverage + link state) per device. Uses the standard
// library net/http; no router framework needed at this scale.
package apihost
