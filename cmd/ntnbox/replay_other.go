//go:build !darwin && !linux

package main

import "errors"

func replayPlatformGate(_ []string) error {
	return errors.New("ntnbox replay is only supported on Linux and macOS (via Docker)")
}
