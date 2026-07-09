package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/netem"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/netns"
	"github.com/hyavari/ntn-in-a-box/internal/recorder"
	ntntui "github.com/hyavari/ntn-in-a-box/internal/tui"
)

func runReplay(args []string) error {
	// Platform gate.
	if err := replayPlatformGate(args); err != nil {
		if errors.Is(err, errProxyComplete) {
			return nil
		}
		return err
	}

	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	filePath := fs.String("file", "", "Path to a recorded JSONL session file (required)")
	speed := fs.Float64("speed", 1.0, "Playback speed multiplier (e.g. 10 for 10x faster)")
	addr := fs.String("addr", "", "Optionally expose the API host (host:port)")
	tuiFlag := fs.Bool("tui", false, "Show a live TUI dashboard")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *filePath == "" {
		return errors.New("--file is required\n\nUsage: ntnbox replay --file <path> [--speed <N>] -- <cmd> [args...]")
	}

	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		return errors.New("no command specified after --\n\nUsage: ntnbox replay --file <path> [--speed <N>] -- <cmd> [args...]")
	}

	// Set up signal handling.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Create kernel components (no profile/evaluator needed — replayer drives the bus).
	registry := device.NewRegistry()
	dev, err := registry.Register("sandbox-0", device.TypeVirtualUE, "replay")
	if err != nil {
		return fmt.Errorf("registering device: %w", err)
	}
	_ = dev

	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	// Create namespace.
	nsExec := netns.ExecReal{}
	ns := netns.New("sandbox-0", nsExec)

	fmt.Fprintf(os.Stderr, "ntnbox: creating network namespace %s\n", ns.Name)
	if err := ns.Create(ctx); err != nil {
		return fmt.Errorf("creating namespace: %w", err)
	}
	defer func() {
		fmt.Fprintf(os.Stderr, "ntnbox: destroying namespace %s\n", ns.Name)
		_ = ns.Destroy(context.Background())
	}()

	// Create netem controller.
	netemCtrl := &netem.Controller{
		Netns:  ns.Name,
		Device: ns.VethInner,
		Exec:   netem.ExecReal{},
	}

	// Set up initial qdisc with zero impairment.
	initialState := condition.LinkState{
		DelayMs:       0,
		JitterMs:      0,
		LossPct:       0,
		BandwidthKbps: 100000,
	}
	if err := netemCtrl.Setup(ctx, initialState); err != nil {
		return fmt.Errorf("setting up netem: %w", err)
	}

	// Create Dev Sandbox module.
	sandbox := devsandbox.New(netemCtrl)
	sandbox.Emit(bus)
	bus.SubscribeCoverage(sandbox.OnCoverageEvent)
	bus.SubscribeLinkState(sandbox.OnLinkState)

	// Optionally start API host.
	if *addr != "" {
		startAPIHost(*addr, bus, registry, nil)
	}

	fmt.Fprintf(os.Stderr, "ntnbox: replaying %s (speed: %.1fx)\n", *filePath, *speed)

	// TUI mode.
	if *tuiFlag {
		for {
			// Create a fresh replayer for each iteration.
			replayer := recorder.NewReplayer(*filePath, bus, *speed)

			// Use atomic.Value so the progress callback (called from the
			// replayer goroutine) can safely read the sender set by
			// NotifySender (called on the main goroutine).
			var senderVal atomic.Value // stores ntntui.Sender

			// Set up replay progress reporting.
			replayer.OnProgress(func(elapsed, total time.Duration) {
				if s, ok := senderVal.Load().(ntntui.Sender); ok && s != nil {
					s.Send(ntntui.ReplayProgressMsg{
						Elapsed: elapsed,
						Total:   total,
					})
				}
			})

			err := ntntui.Run(ctx, ntntui.Config{
				Bus:       bus,
				Evaluator: nil,
				Profile:   &profile.Profile{Name: "replay"},
				Addr:      *addr,
				IsReplay:  true,
				// NotifySender fires before the child starts, so we
				// start the replayer here to guarantee tuiSender is set
				// before any progress/done messages are sent.
				NotifySender: func(s ntntui.Sender) {
					senderVal.Store(s)
					go func() {
						err := replayer.Run(ctx)
						// Always notify the TUI — with error if replay failed.
						if s, ok := senderVal.Load().(ntntui.Sender); ok && s != nil {
							s.Send(ntntui.ReplayDoneMsg{Err: err})
						}
					}()
				},
				CmdFn: func() *exec.Cmd {
					return ns.Command(cmdArgs[0], cmdArgs[1:]...)
				},
			})

			if errors.Is(err, ntntui.ErrReplayAgain) {
				// User wants to replay — loop around.
				continue
			}
			return err
		}
	}

	// Non-TUI: launch command, wait for replay to finish, then stop.
	replayer := recorder.NewReplayer(*filePath, bus, *speed)
	replayDone := make(chan error, 1)

	replayer.OnProgress(func(elapsed, total time.Duration) {
		pct := 0.0
		if total > 0 {
			pct = float64(elapsed) / float64(total) * 100
		}
		fmt.Fprintf(os.Stderr, "\rntnbox: replay %.0f%% (%s / %s)", pct, elapsed.Truncate(time.Second), total.Truncate(time.Second))
	})

	go func() {
		replayDone <- replayer.Run(ctx)
	}()

	fmt.Fprintf(os.Stderr, "ntnbox: running %v\n", cmdArgs)
	cmd := ns.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting command: %w", err)
	}

	// Wait for replay to finish, command to exit, or signal.
	cmdDone := make(chan error, 1)
	go func() {
		cmdDone <- cmd.Wait()
	}()

	select {
	case err := <-replayDone:
		// Replay finished — give a short window for last events to apply.
		time.Sleep(1 * time.Second)
		fmt.Fprintf(os.Stderr, "\nntnbox: replay complete (%s)\n", *filePath)
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-cmdDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-cmdDone
		}
		if err != nil {
			return fmt.Errorf("replay: %w", err)
		}
		return nil

	case err := <-cmdDone:
		if err != nil {
			return fmt.Errorf("command failed: %w", err)
		}
		return nil

	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "\nntnbox: interrupted, stopping...")
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-cmdDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-cmdDone
		}
		return nil
	}
}
