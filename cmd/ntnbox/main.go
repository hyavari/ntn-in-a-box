// Command ntnbox is the CLI entrypoint for NTN-in-a-Box.
//
// Subcommands:
//   - serve: starts the kernel API server with a given profile
//   - run: wraps a process in a shaped network namespace (Dev Sandbox)
//   - replay: replays a recorded JSONL session with original timing
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/apihost"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ntnbox: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Top-level: expect a subcommand.
	if len(args) == 0 {
		return errors.New("usage: ntnbox <command> [flags]\n\nCommands:\n  serve    Start the kernel API server\n  run      Run a command under simulated NTN conditions\n  replay   Replay a recorded session")
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "run":
		return runRun(args[1:])
	case "replay":
		return runReplay(args[1:])
	default:
		return fmt.Errorf("unknown command: %s\n\nCommands:\n  serve    Start the kernel API server\n  run      Run a command under simulated NTN conditions\n  replay   Replay a recorded session", args[0])
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	profilePath := fs.String("profile", "", "Path to a YAML profile file (required)")
	addr := fs.String("addr", ":8080", "Listen address (host:port)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *profilePath == "" {
		return errors.New("--profile is required\n\nUsage: ntnbox serve --profile <path> [--addr <host:port>]")
	}

	// Load and validate the profile.
	p, err := profile.LoadFile(*profilePath)
	if err != nil {
		return fmt.Errorf("loading profile: %w", err)
	}

	// Wire up kernel components.
	srv := apihost.New(apihost.Config{
		Profiles: []*profile.Profile{p},
		Registry: device.NewRegistry(),
	})

	fmt.Fprintf(os.Stderr, "ntnbox: serving on %s (profile: %s)\n", *addr, p.Name)

	// Start HTTP server with graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{
		Addr:    *addr,
		Handler: srv.Handler(),
	}

	// Run server in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for shutdown signal or server error.
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "\nntnbox: shutting down...")
		if err := httpSrv.Close(); err != nil {
			return fmt.Errorf("closing server: %w", err)
		}
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	}

	return nil
}
