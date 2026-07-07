//go:build !linux && !darwin

package main

import "errors"

// runPlatformGate on unsupported platforms returns an error.
func runPlatformGate(_ []string) error {
	return errors.New("ntnbox run requires Linux network namespaces; not supported on this platform")
}
