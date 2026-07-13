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
	numDevices := fs.Int("devices", 1, "Number of sandbox devices (sandbox-0..); profile mode only")
	phaseSec := fs.Float64("phase-sec", 0, "Phase offset seconds between device epochs; profile mode only")

	// TLE flags.
	tlePath := fs.String("tle", "", "Path to TLE file (mutually exclusive with --profile)")
	tleLat := fs.Float64("lat", 0, "Observer latitude in degrees (required with --tle unless --observer)")
	tleLon := fs.Float64("lon", 0, "Observer longitude in degrees (required with --tle unless --observer)")
	tleAlt := fs.Float64("alt", 0, "Observer altitude in km")
	tleLinkModel := fs.String("link-model", "", "Path to custom link model YAML")
	tleElevMin := fs.Float64("elev-min", 10, "Minimum elevation angle in degrees")
	tleSat := fs.String("sat", "", "Select satellite by name or NORAD ID")
	tleStartAt := fs.String("start-at", "", `"next-pass" or RFC3339 timestamp`)
	tleSpeed := fs.Float64("speed", 1.0, "Gap time acceleration factor")
	tlePasses := fs.Int("passes", 10, "Number of passes to pre-compute")
	var observerFlags stringList
	fs.Var(&observerFlags, "observer", "TLE observer id=lat,lon (repeatable)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate mutual exclusion.
	if *profilePath != "" && *tlePath != "" {
		return errors.New("flags --tle and --profile are mutually exclusive")
	}
	if *profilePath == "" && *tlePath == "" {
		return errors.New("--profile or --tle is required\n\nUsage: ntnbox run --profile <path> [--devices N] [--phase-sec S] -- <cmd> [args...]\n       ntnbox run --tle <path> --lat <deg> --lon <deg> -- <cmd> [args...]\n       ntnbox run --tle <path> --observer id=lat,lon [--observer …] -- <cmd> [args...]")
	}
	if *numDevices < 1 {
		return errors.New("--devices must be >= 1")
	}

	parsedObservers, err := parseObserverFlags(observerFlags)
	if err != nil {
		return err
	}
	if err := rejectObserverDeviceMix(parsedObservers, *numDevices, *phaseSec); err != nil {
		return err
	}
	if *profilePath != "" && len(parsedObservers) > 0 {
		return errors.New("--observer requires --tle (not --profile)")
	}
	if *tlePath != "" && (*numDevices != 1 || *phaseSec != 0) {
		return errors.New("--devices / --phase-sec are profile-mode only (use --observer for TLE multi-device)")
	}

	// Everything after "--" is the user's command.
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		return errors.New("no command specified after --\n\nUsage: ntnbox run --profile <path> -- <cmd> [args...]")
	}

	// Build the evaluator and related state depending on mode.
	type deviceEval struct {
		id   string
		eval condition.Eval
	}
	var (
		eval         condition.Eval
		seqEval      *tle.SequenceEvaluator // non-nil in TLE mode (primary)
		tb           *tleBootstrap          // non-nil multi/single TLE bootstrap
		p            *profile.Profile       // nil in TLE mode
		lookaheadSec float64
		initialState condition.LinkState
		profileName  string
		tleModel     *tle.LinkModel // non-nil in TLE mode; reused by TUI
		deviceEvals  []deviceEval   // profile multi-device or empty (TLE uses tb)
	)

	if *tlePath != "" {
		latSet, lonSet := false, false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "lat" {
				latSet = true
			}
			if f.Name == "lon" {
				lonSet = true
			}
		})
		observers, err := resolveTLEObservers(parsedObservers, *tleLat, *tleLon, latSet && lonSet)
		if err != nil {
			return err
		}
		tb, err = bootstrapTLE(tleBootstrapOpts{
			Path:      *tlePath,
			Sat:       *tleSat,
			LinkModel: *tleLinkModel,
			StartAt:   *tleStartAt,
			AltKm:     *tleAlt,
			ElevMin:   *tleElevMin,
			Speed:     *tleSpeed,
			Passes:    *tlePasses,
			Observers: observers,
		})
		if err != nil {
			return err
		}
		seqEval = tb.Primary
		eval = tb.Primary
		lookaheadSec = tb.LookaheadSec
		profileName = fmt.Sprintf("tle:%s", tb.SatName)
		initialState = tb.InitialState
		tleModel = &tb.Model
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
	if tb != nil {
		for _, d := range tb.Devices {
			if _, err := registry.Register(d.ID, device.TypeVirtualUE, profileName); err != nil {
				return fmt.Errorf("registering device: %w", err)
			}
		}
	} else if p != nil {
		base := time.Now()
		for i := 0; i < *numDevices; i++ {
			id := fmt.Sprintf("sandbox-%d", i)
			if _, err := registry.Register(id, device.TypeVirtualUE, profileName); err != nil {
				return fmt.Errorf("registering device: %w", err)
			}
			epoch := base.Add(time.Duration(float64(i)**phaseSec) * time.Second)
			ev, err := condition.NewEvaluator(*p, epoch)
			if err != nil {
				return fmt.Errorf("creating evaluator for %s: %w", id, err)
			}
			deviceEvals = append(deviceEvals, deviceEval{id: id, eval: ev})
			if i == 0 {
				eval = ev
			}
		}
		if *numDevices > 1 {
			fmt.Fprintf(os.Stderr, "ntnbox: devices=%d phase-sec=%.0f\n", *numDevices, *phaseSec)
		}
	}

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
		if tb != nil {
			for _, d := range tb.Devices {
				rec.RegisterDevice(d.ID, d.Eval)
			}
		} else {
			for _, de := range deviceEvals {
				rec.RegisterDevice(de.id, de.eval)
			}
		}
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
		if tb != nil {
			sessInfo = tb.sessionInfo()
		} else if p != nil {
			sessInfo = &apihost.SessionInfo{
				Mode:        "profile",
				ProfileName: p.Name,
			}
		}
		srv := newAPIHost(bus, registry, eval, sessInfo, *tuiFlag, profiles...)
		if tb != nil {
			for _, d := range tb.Devices {
				srv.RegisterEvaluator(d.ID, d.Eval)
			}
		} else {
			for _, de := range deviceEvals {
				srv.RegisterEvaluator(de.id, de.eval)
			}
		}
		sandbox.RegisterRoutes(srv)
		// Late POST /devices need their own driver so messaging can flush on window_opened.
		// Must be set before listenAPIHost to avoid a registration race.
		known := map[string]bool{"sandbox-0": true}
		for _, de := range deviceEvals {
			known[de.id] = true
		}
		if tb != nil {
			for _, d := range tb.Devices {
				known[d.ID] = true
			}
		}
		srv.OnDeviceRegistered(func(id string, deviceEval condition.Eval) {
			if known[id] {
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

	// Start driver loop(s): primary + peer TLE observers or phase-offset peers.
	if tb != nil {
		for i, d := range tb.Devices {
			go driver.New(driver.Config{
				Evaluator:       d.Eval,
				Bus:             bus,
				DeviceID:        d.ID,
				LookaheadSec:    lookaheadSec,
				PublishPosition: boolPtr(i == 0), // one orbital track for the globe
			}).Run(loopCtx)
		}
	} else {
		for _, de := range deviceEvals {
			go driver.New(driver.Config{
				Evaluator:    de.eval,
				Bus:          bus,
				DeviceID:     de.id,
				LookaheadSec: lookaheadSec,
			}).Run(loopCtx)
		}
	}

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
		focusID := "sandbox-0"
		var deviceIDs []string
		evals := map[string]condition.Eval{}
		if tb != nil && len(tb.Devices) > 0 {
			// Match FocusDeviceID to the evaluator we enrich from (primary).
			focusID = tb.Devices[0].ID
			for _, d := range tb.Devices {
				deviceIDs = append(deviceIDs, d.ID)
				evals[d.ID] = d.Eval
			}
		} else {
			for _, de := range deviceEvals {
				deviceIDs = append(deviceIDs, de.id)
				evals[de.id] = de.eval
			}
			if len(deviceIDs) > 0 {
				focusID = deviceIDs[0]
			}
		}
		return ntntui.Run(ctx, ntntui.Config{
			Bus:           bus,
			Evaluator:     eval,
			Profile:       tuiProfile,
			Addr:          *addr,
			FocusDeviceID: focusID,
			DeviceIDs:     deviceIDs,
			Evals:         evals,
			CmdFn: func() *exec.Cmd {
				return ns.Command(cmdArgs[0], cmdArgs[1:]...)
			},
		})
	}

	// Non-TUI mode: scrolling output (existing behavior).

	// Subscribe a stderr logger for coverage transitions (primary only).
	bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) {
		primaryLogID := "sandbox-0"
		if tb != nil && len(tb.Devices) > 0 {
			primaryLogID = tb.Devices[0].ID
		} else if len(deviceEvals) > 0 {
			primaryLogID = deviceEvals[0].id
		}
		if ev.DeviceID != "" && ev.DeviceID != primaryLogID {
			return
		}
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
