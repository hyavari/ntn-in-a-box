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
	ntnrecorder "github.com/hyavari/ntn-in-a-box/internal/recorder"
	"github.com/hyavari/ntn-in-a-box/internal/tle"
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
	profilePath := fs.String("profile", "", "Path to a YAML profile file")
	addr := fs.String("addr", "", "Optionally expose the API host (host:port); bare :port binds 127.0.0.1")
	tuiFlag := fs.Bool("tui", false, "Show a live TUI dashboard instead of scrolling output")
	recordPath := fs.String("record", "", "Record bus events to a JSONL file")

	// TLE flags.
	tlePath := fs.String("tle", "", "Path to TLE file (mutually exclusive with --profile)")
	tleLat := fs.Float64("lat", 0, "Observer latitude in degrees (required with --tle)")
	tleLon := fs.Float64("lon", 0, "Observer longitude in degrees (required with --tle)")
	tleAlt := fs.Float64("alt", 0, "Observer altitude in km")
	tleLinkModel := fs.String("link-model", "", "Path to custom link model YAML")
	tleElevMin := fs.Float64("elev-min", 10, "Minimum elevation angle in degrees")
	tleSat := fs.String("sat", "", "Select satellite by name or NORAD ID")
	tleStartAt := fs.String("start-at", "", `"next-pass" or RFC3339 timestamp`)
	tleSpeed := fs.Float64("speed", 1.0, "Gap time acceleration factor")
	tlePasses := fs.Int("passes", 10, "Number of passes to pre-compute")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate mutual exclusion.
	if *profilePath != "" && *tlePath != "" {
		return errors.New("flags --tle and --profile are mutually exclusive")
	}
	if *profilePath == "" && *tlePath == "" {
		return errors.New("--profile or --tle is required\n\nUsage: ntnbox run --profile <path> -- <cmd> [args...]\n       ntnbox run --tle <path> --lat <deg> --lon <deg> -- <cmd> [args...]")
	}

	// Everything after "--" is the user's command.
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		return errors.New("no command specified after --\n\nUsage: ntnbox run --profile <path> -- <cmd> [args...]")
	}

	// Build the evaluator and related state depending on mode.
	var (
		eval         condition.Eval
		seqEval      *tle.SequenceEvaluator // non-nil in TLE mode
		p            *profile.Profile       // nil in TLE mode
		lookaheadSec float64
		initialState condition.LinkState
		profileName  string
		tleModel     *tle.LinkModel // non-nil in TLE mode; reused by TUI
	)

	if *tlePath != "" {
		// TLE mode.
		// Check --lat and --lon were actually provided.
		latSet, lonSet := false, false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "lat" {
				latSet = true
			}
			if f.Name == "lon" {
				lonSet = true
			}
		})
		if !latSet || !lonSet {
			return errors.New("--lat and --lon are required when using --tle")
		}

		sats, err := tle.ParseFile(*tlePath)
		if err != nil {
			return err
		}

		sat, err := tle.SelectSatellite(sats, *tleSat)
		if err != nil {
			return err
		}

		if *tleSat == "" && len(sats) > 1 {
			fmt.Fprintf(os.Stderr, "ntnbox: using satellite %q (NORAD ID %d) — use --sat to select a different one\n",
				sat.Name, sat.NoradID)
		}

		// TLE age warning.
		if age, err := tle.TLEAge(sat, time.Now()); err == nil && age > 14*24*time.Hour {
			fmt.Fprintf(os.Stderr, "ntnbox: warning: TLE is %.0f days old (accuracy degrades after ~14 days)\n", age.Hours()/24)
		}

		// Load link model.
		var model tle.LinkModel
		if *tleLinkModel != "" {
			m, err := tle.LoadLinkModel(*tleLinkModel)
			if err != nil {
				return err
			}
			model = *m
		} else {
			model = tle.DefaultLinkModel()
		}
		tleModel = &model

		// Predict passes.
		obs := tle.Observer{LatDeg: *tleLat, LonDeg: *tleLon, AltKm: *tleAlt}
		cfg := tle.PredictConfig{
			MinElevDeg: *tleElevMin,
			Count:      *tlePasses,
			MaxSearch:  48 * time.Hour,
		}

		fmt.Fprintf(os.Stderr, "ntnbox: predicting passes for %q from (%.4f°, %.4f°)...\n", sat.Name, *tleLat, *tleLon)

		passes, err := tle.PredictPasses(sat, obs, time.Now(), cfg)
		if err != nil {
			return fmt.Errorf("predicting passes: %w", err)
		}
		if len(passes) == 0 {
			return fmt.Errorf("no visible passes found for %q from (%.4f°, %.4f°) in the next 48h — try lowering --elev-min",
				sat.Name, *tleLat, *tleLon)
		}

		// Determine start time.
		var startAt time.Time
		switch *tleStartAt {
		case "", "next-pass":
			startAt = passes[0].Rise.Add(-30 * time.Second)
		default:
			t, err := time.Parse(time.RFC3339, *tleStartAt)
			if err != nil {
				return fmt.Errorf("invalid --start-at time: %w", err)
			}
			startAt = t
		}

		var seqErr error
		seqEval, seqErr = tle.NewSequenceEvaluator(passes, model, tle.SequenceConfig{
			Speed:        *tleSpeed,
			StartAt:      startAt,
			LookaheadSec: 30,
			Observer:     obs,
			Sat:          sat,
		})
		if seqErr != nil {
			return fmt.Errorf("creating TLE evaluator: %w", seqErr)
		}

		eval = seqEval
		lookaheadSec = seqEval.LookaheadSec()
		profileName = fmt.Sprintf("tle:%s", sat.Name)

		// Initial state: use the link model at minimum elevation.
		delay, jitter, loss, bw := model.Interpolate(model.MinElevDeg)
		initialState = condition.LinkState{
			DelayMs:       delay,
			JitterMs:      jitter,
			LossPct:       loss,
			BandwidthKbps: bw,
		}

		fmt.Fprintf(os.Stderr, "ntnbox: %d passes predicted (next: %s, max elev: %.1f°)\n",
			len(passes), passes[0].Rise.Format(time.RFC3339), passes[0].MaxElevDeg)
		if *tleSpeed > 1 {
			fmt.Fprintf(os.Stderr, "ntnbox: gap acceleration: %.0fx\n", *tleSpeed)
		}
	} else {
		// Profile mode (existing behavior).
		var err error
		p, err = profile.LoadFile(*profilePath)
		if err != nil {
			return fmt.Errorf("loading profile: %w", err)
		}

		profileName = p.Name
		lookaheadSec = p.Schedule.LookaheadSec
		initialState = condition.LinkState{
			DelayMs:       p.Curves.DelayMs[0].Value,
			JitterMs:      p.Curves.JitterMs[0].Value,
			LossPct:       p.Curves.LossPct[0].Value,
			BandwidthKbps: p.Curves.BandwidthKbps[0].Value,
		}
	}

	// Set up signal handling.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Create kernel components.
	registry := device.NewRegistry()
	dev, err := registry.Register("sandbox-0", device.TypeVirtualUE, profileName)
	if err != nil {
		return fmt.Errorf("registering device: %w", err)
	}

	// In profile mode, create the standard evaluator.
	if p != nil && eval == nil {
		condEval, err := condition.NewEvaluator(*p, dev.CreatedAt)
		if err != nil {
			return fmt.Errorf("creating evaluator: %w", err)
		}
		eval = condEval
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

	// Set up initial qdisc with the initial link state values.
	if err := netemCtrl.Setup(ctx, initialState); err != nil {
		return fmt.Errorf("setting up netem: %w", err)
	}

	// Create Dev Sandbox module.
	sandbox := devsandbox.New(netemCtrl)
	sandbox.Emit(bus)

	// Subscribe module to bus events.
	bus.SubscribeCoverage(sandbox.OnCoverageEvent)
	bus.SubscribeLinkState(sandbox.OnLinkState)

	// Optionally record bus events.
	if *recordPath != "" {
		rec, err := ntnrecorder.New(*recordPath, eval)
		if err != nil {
			return fmt.Errorf("creating recorder: %w", err)
		}
		defer rec.Close()
		bus.SubscribeCoverage(rec.OnCoverage)
		bus.SubscribeLinkState(rec.OnLinkState)
		fmt.Fprintf(os.Stderr, "ntnbox: recording to %s\n", *recordPath)
	}

	// Optionally start the API host.
	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()

	if *addr != "" {
		var profiles []*profile.Profile
		if p != nil {
			profiles = append(profiles, p)
		}
		// Build session info for the GUI.
		var sessInfo *apihost.SessionInfo
		if seqEval != nil {
			orbitPoints := tle.ComputeOrbitPoints(seqEval.SatData(), seqEval.SimTime(), 200)
			sessInfo = &apihost.SessionInfo{
				Mode:           "tle",
				SatelliteName:  seqEval.SatData().Name,
				ObserverLatDeg: seqEval.Observer().LatDeg,
				ObserverLonDeg: seqEval.Observer().LonDeg,
				ObserverAltKm:  seqEval.Observer().AltKm,
				OrbitPoints:    orbitPoints,
			}
		} else if p != nil {
			sessInfo = &apihost.SessionInfo{
				Mode:        "profile",
				ProfileName: p.Name,
			}
		}
		srv := newAPIHost(bus, registry, eval, sessInfo, profiles...)
		sandbox.RegisterRoutes(srv)
		// Late POST /devices need their own driver so messaging can flush on window_opened.
		// Must be set before listenAPIHost to avoid a registration race.
		srv.OnDeviceRegistered(func(id string, deviceEval condition.Eval) {
			if id == "sandbox-0" {
				return
			}
			go driver.New(driver.Config{
				Evaluator:    deviceEval,
				Bus:          bus,
				DeviceID:     id,
				LookaheadSec: lookaheadSec,
			}).Run(loopCtx)
		})
		listenAPIHost(srv, *addr, eval)
	}

	// Start driver loop for sandbox-0.
	loop := driver.New(driver.Config{
		Evaluator:    eval,
		Bus:          bus,
		DeviceID:     "sandbox-0",
		LookaheadSec: lookaheadSec,
	})
	go loop.Run(loopCtx)

	// TUI mode: the TUI owns the terminal and manages the child process.
	if *tuiFlag {
		tuiProfile := p
		if tuiProfile == nil {
			// In TLE mode, generate a profile from the first pass for TUI display.
			if seqEval != nil {
				passes := seqEval.Passes()
				if len(passes) > 0 && tleModel != nil {
					firstProfile, err := tle.GenerateProfile(passes[0], *tleModel, tle.GenerateOpts{
						LookaheadSec: seqEval.LookaheadSec(),
						GapSec:       60,
						Index:        0,
						SatName:      passes[0].Satellite,
					})
					if err == nil {
						tuiProfile = firstProfile
					}
				}
			}
			if tuiProfile == nil {
				tuiProfile = &profile.Profile{Name: profileName}
			}
		}
		return ntntui.Run(ctx, ntntui.Config{
			Bus:       bus,
			Evaluator: eval,
			Profile:   tuiProfile,
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
			fmt.Fprintln(os.Stderr, cli.CoverageOpened(profileName, cov.UntilNextTransitionSec))
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
	fmt.Fprintf(os.Stderr, "ntnbox: running %v (profile: %s)\n", cmdArgs, profileName)
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
