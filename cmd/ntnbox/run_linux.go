//go:build linux

package main

// runPlatformGate is a no-op on Linux — native netns is supported.
func runPlatformGate(_ []string) error {
	return nil
}
