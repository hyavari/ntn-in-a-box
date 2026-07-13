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
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/apihost"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/driver"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/imsadapter"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
	"github.com/hyavari/ntn-in-a-box/internal/module/messaging"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	profilePath := fs.String("profile", "", "Path to a YAML profile file")
	tlePath := fs.String("tle", "", "Path to TLE file (mutually exclusive with --profile)")
	addr := fs.String("addr", "127.0.0.1:8080", "Listen address (host:port); use 0.0.0.0:8080 to expose on LAN")
	noDevice := fs.Bool("no-device", false, "Do not auto-register sandbox devices (legacy API-only)")
	numDevices := fs.Int("devices", 1, "Number of sandbox devices to register (sandbox-0..); profile mode only")
	phaseSec := fs.Float64("phase-sec", 0, "Phase offset seconds between device evaluator epochs; profile mode only")

	tleLat := fs.Float64("lat", 0, "Observer latitude (TLE; or use --observer)")
	tleLon := fs.Float64("lon", 0, "Observer longitude (TLE; or use --observer)")
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

	if *profilePath != "" && *tlePath != "" {
		return errors.New("flags --tle and --profile are mutually exclusive")
	}
	if *profilePath == "" && *tlePath == "" {
		return errors.New("--profile or --tle is required\n\nUsage: ntnbox serve --profile <path> [--devices N] [--phase-sec S]\n       ntnbox serve --tle <path> --lat <deg> --lon <deg>\n       ntnbox serve --tle <path> --observer id=lat,lon [--observer …]")
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

	registry := device.NewRegistry()
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	type deviceEval struct {
		id   string
		eval condition.Eval
	}
	var deviceEvals []deviceEval
	var primaryEval condition.Eval
	var sessInfo *apihost.SessionInfo
	var profiles []*profile.Profile
	var lookaheadSec float64
	modeLabel := ""

	if *tlePath != "" {
		if *noDevice {
			return errors.New("--no-device is not supported with --tle")
		}
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
		tb, err := bootstrapTLE(tleBootstrapOpts{
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
		profileName := fmt.Sprintf("tle:%s", tb.SatName)
		for _, d := range tb.Devices {
			if _, regErr := registry.Register(d.ID, device.TypeVirtualUE, profileName); regErr != nil {
				return fmt.Errorf("registering %s: %w", d.ID, regErr)
			}
			deviceEvals = append(deviceEvals, deviceEval{id: d.ID, eval: d.Eval})
		}
		primaryEval = tb.Primary
		lookaheadSec = tb.LookaheadSec
		sessInfo = tb.sessionInfo()
		modeLabel = profileName
	} else {
		p, err := profile.LoadFile(*profilePath)
		if err != nil {
			return fmt.Errorf("loading profile: %w", err)
		}
		profiles = []*profile.Profile{p}
		lookaheadSec = p.Schedule.LookaheadSec
		modeLabel = p.Name
		sessInfo = &apihost.SessionInfo{
			Mode:        "profile",
			ProfileName: p.Name,
		}

		if !*noDevice {
			base := time.Now()
			for i := 0; i < *numDevices; i++ {
				id := fmt.Sprintf("sandbox-%d", i)
				if _, regErr := registry.Register(id, device.TypeVirtualUE, p.Name); regErr != nil {
					return fmt.Errorf("registering %s: %w", id, regErr)
				}
				epoch := base.Add(time.Duration(float64(i)**phaseSec) * time.Second)
				ev, evalErr := condition.NewEvaluator(*p, epoch)
				if evalErr != nil {
					return fmt.Errorf("creating evaluator for %s: %w", id, evalErr)
				}
				deviceEvals = append(deviceEvals, deviceEval{id: id, eval: ev})
				if i == 0 {
					primaryEval = ev
				}
			}
		}
	}

	srv := apihost.New(apihost.Config{
		Profiles:    profiles,
		Registry:    registry,
		Bus:         bus,
		Evaluator:   primaryEval,
		SessionInfo: sessInfo,
	})
	for _, de := range deviceEvals {
		srv.RegisterEvaluator(de.id, de.eval)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	msgMod := messaging.New(messaging.Config{
		DeviceExists: func(id string) bool {
			_, err := registry.Get(id)
			return err == nil
		},
		InCoverage: func(id string) bool {
			if id == messaging.CloudRecipient {
				return true
			}
			ev := srv.DeviceEvaluator(id)
			if ev == nil {
				return false
			}
			_, cov := ev.Evaluate(time.Now())
			return cov.InCoverage
		},
		Bus: bus,
	})
	msgMod.DeliverVia(imsadapter.NewMockAdapter(imsadapter.MockConfig{}))
	msgMod.RegisterRoutes(srv)
	bus.SubscribeCoverage(msgMod.OnCoverageEvent)
	srv.SetStoreAndForward(true)
	defer msgMod.Close()

	primaryID := ""
	if len(deviceEvals) > 0 {
		primaryID = deviceEvals[0].id
	}
	startDriver := func(id string, eval condition.Eval) {
		// Only the primary observer publishes satellite_position (one orbital track).
		publishPos := primaryID == "" || id == primaryID
		loop := driver.New(driver.Config{
			Evaluator:       eval,
			Bus:             bus,
			DeviceID:        id,
			LookaheadSec:    lookaheadSec,
			PublishPosition: boolPtr(publishPos),
		})
		go loop.Run(ctx)
	}
	for _, de := range deviceEvals {
		startDriver(de.id, de.eval)
	}
	srv.OnDeviceRegistered(func(id string, eval condition.Eval) {
		startDriver(id, eval)
	})

	*addr = normalizeListenAddr(*addr)

	fmt.Fprintf(os.Stderr, "ntnbox: serving on %s (%s)\n", *addr, modeLabel)
	if len(deviceEvals) > 0 {
		if *tlePath != "" {
			fmt.Fprintf(os.Stderr, "ntnbox: devices=%d (TLE observers)  messaging ready\n", len(deviceEvals))
		} else {
			fmt.Fprintf(os.Stderr, "ntnbox: devices=%d phase-sec=%.0f  messaging ready\n", len(deviceEvals), *phaseSec)
		}
		fmt.Fprintf(os.Stderr, "ntnbox: GUI http://localhost:%s/ui\n", addrPort(*addr))
	} else {
		fmt.Fprintf(os.Stderr, "ntnbox: --no-device: register devices via POST /devices\n")
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

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
