package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/cli"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/apihost"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/driver"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/netem"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/netns"
	ntntui "github.com/hyavari/ntn-in-a-box/internal/tui"
)

// errProxyComplete signals that the Docker proxy ran the command
// successfully — not a real error, just a control-flow sentinel.
var errProxyComplete = errors.New("proxy complete")

func runRun(args []string) error {
	// Platform gate: on macOS, proxies via Docker and returns
	// errProxyComplete on success. On Linux, returns nil (fall through
	// to native execution). On other platforms, returns an error.
	if err := runPlatformGate(args); err != nil {
		if errors.Is(err, errProxyComplete) {
			return nil
		}
		return err
	}

	// Parse flags up to "--" separator.
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	profilePath := fs.String("profile", "", "Path to a YAML profile file (required)")
	addr := fs.String("addr", "", "Optionally expose the API host (host:port)")
	tuiFlag := fs.Bool("tui", false, "Show a live TUI dashboard instead of scrolling output")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *profilePath == "" {
		return errors.New("--profile is required\n\nUsage: ntnbox run --profile <path> [--addr <host:port>] -- <cmd> [args...]")
	}

	// Everything after "--" is the user's command.
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		return errors.New("no command specified after --\n\nUsage: ntnbox run --profile <path> [--addr <host:port>] -- <cmd> [args...]")
	}

	// Load profile.
	p, err := profile.LoadFile(*profilePath)
	if err != nil {
		return fmt.Errorf("loading profile: %w", err)
	}

	// Set up signal handling.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Create kernel components.
	registry := device.NewRegistry()
	dev, err := registry.Register("sandbox-0", device.TypeVirtualUE, p.Name)
	if err != nil {
		return fmt.Errorf("registering device: %w", err)
	}

	eval, err := condition.NewEvaluator(*p, dev.CreatedAt)
	if err != nil {
		return fmt.Errorf("creating evaluator: %w", err)
	}

	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	// Create namespace.
	nsExec := netns.ExecReal{}
	ns := netns.New(dev.ID, nsExec)

	fmt.Fprintf(os.Stderr, "ntnbox: creating network namespace %s\n", ns.Name)
	if err := ns.Create(ctx); err != nil {
		return fmt.Errorf("creating namespace: %w", err)
	}
	defer func() {
		fmt.Fprintf(os.Stderr, "ntnbox: destroying namespace %s\n", ns.Name)
		_ = ns.Destroy(context.Background())
	}()

	// Create netem controller for the namespace.
	netemCtrl := &netem.Controller{
		Netns:  ns.Name,
		Device: ns.VethInner,
		Exec:   netem.ExecReal{},
	}

	// Set up initial qdisc with the profile's first curve values.
	initialState := condition.LinkState{
		DelayMs:       p.Curves.DelayMs[0].Value,
		JitterMs:      p.Curves.JitterMs[0].Value,
		LossPct:       p.Curves.LossPct[0].Value,
		BandwidthKbps: p.Curves.BandwidthKbps[0].Value,
	}
	if err := netemCtrl.Setup(ctx, initialState); err != nil {
		return fmt.Errorf("setting up netem: %w", err)
	}

	// Create Dev Sandbox module.
	sandbox := devsandbox.New(netemCtrl)
	sandbox.Emit(bus)

	// Subscribe module to bus events.
	bus.SubscribeCoverage(sandbox.OnCoverageEvent)
	bus.SubscribeLinkState(sandbox.OnLinkState)

	// Optionally start the API host.
	if *addr != "" {
		srv := apihost.New(apihost.Config{
			Profiles:  []*profile.Profile{p},
			Registry:  registry,
			Bus:       bus,
			Evaluator: eval,
		})
		srv.RegisterEvaluator(dev.ID, eval)
		sandbox.RegisterRoutes(srv)
		go func() {
			fmt.Fprintf(os.Stderr, "ntnbox: API listening on %s\n", *addr)
			fmt.Fprintf(os.Stderr, "ntnbox: GUI available at http://localhost:%s/ui\n", addrPort(*addr))
			_ = srv.ListenAndServe(*addr)
		}()
	}

	// Start driver loop.
	loop := driver.New(driver.Config{
		Evaluator:    eval,
		Bus:          bus,
		LookaheadSec: p.Schedule.LookaheadSec,
	})

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	go loop.Run(loopCtx)

	// TUI mode: the TUI owns the terminal and manages the child process.
	if *tuiFlag {
		return ntntui.Run(ctx, ntntui.Config{
			Bus:       bus,
			Evaluator: eval,
			Profile:   p,
			Addr:      *addr,
			CmdFn: func() *exec.Cmd {
				return ns.Command(cmdArgs[0], cmdArgs[1:]...)
			},
		})
	}

	// Non-TUI mode: scrolling output (existing behavior).

	// Subscribe a stderr logger for coverage transitions.
	bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) {
		switch ev.Kind {
		case eventbus.KindWindowOpened:
			_, cov := eval.Evaluate(ev.At)
			fmt.Fprintln(os.Stderr, cli.CoverageOpened(p.Name, cov.UntilNextTransitionSec))
		case eventbus.KindWindowClosed:
			_, cov := eval.Evaluate(ev.At)
			fmt.Fprintln(os.Stderr, cli.CoverageClosed(cov.UntilNextTransitionSec))
		case eventbus.KindWindowClosing:
			_, cov := eval.Evaluate(ev.At)
			fmt.Fprintln(os.Stderr, cli.CoverageClosing(cov.UntilNextTransitionSec))
		case eventbus.KindWindowOpening:
			_, cov := eval.Evaluate(ev.At)
			fmt.Fprintln(os.Stderr, cli.CoverageOpening(cov.UntilNextTransitionSec))
		}
	})

	// Launch user command inside the namespace.
	fmt.Fprintf(os.Stderr, "ntnbox: running %v (profile: %s)\n", cmdArgs, p.Name)
	cmd := ns.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting command: %w", err)
	}

	// Wait for command to finish or signal.
	cmdDone := make(chan error, 1)
	go func() {
		cmdDone <- cmd.Wait()
	}()

	select {
	case err := <-cmdDone:
		loopCancel()
		if err != nil {
			return fmt.Errorf("command failed: %w", err)
		}
		return nil

	case <-ctx.Done():
		// Signal received — kill the child process.
		fmt.Fprintln(os.Stderr, "\nntnbox: interrupted, stopping command...")
		_ = cmd.Process.Signal(syscall.SIGTERM)

		// Give it a moment to exit gracefully.
		select {
		case <-cmdDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-cmdDone
		}
		return nil
	}
}

// addrPort extracts the port from an address like ":8080" or "0.0.0.0:8080".
func addrPort(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i+1:]
		}
	}
	return addr
}
