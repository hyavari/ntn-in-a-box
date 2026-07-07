// Package devsandbox implements the Dev Sandbox capability module: the
// first real module plugging into the kernel's 5-hook contract. It
// drives a netem controller to shape traffic inside a network namespace
// based on coverage events and link-state updates from the kernel's
// event bus.
package devsandbox
