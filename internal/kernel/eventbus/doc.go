// Package eventbus implements the kernel's in-process pub/sub: coverage
// events (window open/close, with lookahead) and link-state updates
// (continuous curve values, throttled by delta/interval), plus a generic
// emit path for observability events.
package eventbus
