//go:build linux

package main

// replayPlatformGate on Linux is a no-op — native execution.
func replayPlatformGate(_ []string) error {
	return nil
}
