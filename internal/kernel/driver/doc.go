// Package driver implements the kernel's driver loop: a goroutine that
// ticks at a fixed interval, evaluates the Condition Engine, detects
// coverage transitions (including lookahead notices), and publishes
// events to the event bus. This is the bridge between the pull-based
// Evaluator and the push-based event bus.
package driver
