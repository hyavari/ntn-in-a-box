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
	profilePath := fs.String("profile", "", "Path to a YAML profile file (required)")
	addr := fs.String("addr", "127.0.0.1:8080", "Listen address (host:port); use 0.0.0.0:8080 to expose on LAN")
	noDevice := fs.Bool("no-device", false, "Do not auto-register sandbox devices (legacy API-only)")
	numDevices := fs.Int("devices", 1, "Number of sandbox devices to register (sandbox-0..)")
	phaseSec := fs.Float64("phase-sec", 0, "Phase offset seconds between device evaluator epochs")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *profilePath == "" {
		return errors.New("--profile is required\n\nUsage: ntnbox serve --profile <path> [--addr <host:port>] [--devices N] [--phase-sec S] [--no-device]")
	}
	if *numDevices < 1 {
		return errors.New("--devices must be >= 1")
	}

	p, err := profile.LoadFile(*profilePath)
	if err != nil {
		return fmt.Errorf("loading profile: %w", err)
	}

	registry := device.NewRegistry()
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	type deviceEval struct {
		id   string
		eval condition.Eval
	}
	var deviceEvals []deviceEval
	var primaryEval condition.Eval

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

	srv := apihost.New(apihost.Config{
		Profiles:  []*profile.Profile{p},
		Registry:  registry,
		Bus:       bus,
		Evaluator: primaryEval,
		SessionInfo: &apihost.SessionInfo{
			Mode:        "profile",
			ProfileName: p.Name,
		},
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

	startDriver := func(id string, eval condition.Eval) {
		loop := driver.New(driver.Config{
			Evaluator:    eval,
			Bus:          bus,
			DeviceID:     id,
			LookaheadSec: p.Schedule.LookaheadSec,
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

	fmt.Fprintf(os.Stderr, "ntnbox: serving on %s (profile: %s)\n", *addr, p.Name)
	if len(deviceEvals) > 0 {
		fmt.Fprintf(os.Stderr, "ntnbox: devices=%d phase-sec=%.0f  messaging ready\n", len(deviceEvals), *phaseSec)
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
