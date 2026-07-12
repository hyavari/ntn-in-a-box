// Command ntnbox is the CLI entrypoint for NTN-in-a-Box.
//
// Subcommands:
//   - serve: starts the kernel API server with a given profile
//   - run: wraps a process in a shaped network namespace (Dev Sandbox)
//   - replay: replays a recorded JSONL session with original timing
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ntnbox: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ntnbox <command> [flags]\n\nCommands:\n  serve    Start the kernel API server\n  run      Run a command under simulated NTN conditions\n  replay   Replay a recorded session\n  tle      TLE utilities (generate profiles from orbital data)")
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "run":
		return runRun(args[1:])
	case "replay":
		return runReplay(args[1:])
	case "tle":
		return runTLE(args[1:])
	default:
		return fmt.Errorf("unknown command: %s\n\nCommands:\n  serve    Start the kernel API server\n  run      Run a command under simulated NTN conditions\n  replay   Replay a recorded session\n  tle      TLE utilities (generate profiles from orbital data)", args[0])
	}
}
