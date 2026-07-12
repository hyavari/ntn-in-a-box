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
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	profilePath := fs.String("profile", "", "Path to a YAML profile file (required)")
	addr := fs.String("addr", ":8080", "Listen address (host:port)")
	noDevice := fs.Bool("no-device", false, "Do not auto-register sandbox-0 (legacy API-only)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *profilePath == "" {
		return errors.New("--profile is required\n\nUsage: ntnbox serve --profile <path> [--addr <host:port>] [--no-device]")
	}

	p, err := profile.LoadFile(*profilePath)
	if err != nil {
		return fmt.Errorf("loading profile: %w", err)
	}

	registry := device.NewRegistry()
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)

	var eval condition.Eval
	if !*noDevice {
		dev, regErr := registry.Register("sandbox-0", device.TypeVirtualUE, p.Name)
		if regErr != nil {
			return fmt.Errorf("registering sandbox-0: %w", regErr)
		}
		ev, evalErr := condition.NewEvaluator(*p, dev.CreatedAt)
		if evalErr != nil {
			return fmt.Errorf("creating evaluator: %w", evalErr)
		}
		eval = ev
	}

	srv := apihost.New(apihost.Config{
		Profiles:  []*profile.Profile{p},
		Registry:  registry,
		Bus:       bus,
		Evaluator: eval,
		SessionInfo: &apihost.SessionInfo{
			Mode:        "profile",
			ProfileName: p.Name,
		},
	})
	if eval != nil {
		srv.RegisterEvaluator("sandbox-0", eval)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if eval != nil {
		loop := driver.New(driver.Config{
			Evaluator:    eval,
			Bus:          bus,
			LookaheadSec: p.Schedule.LookaheadSec,
		})
		go loop.Run(ctx)
	}

	fmt.Fprintf(os.Stderr, "ntnbox: serving on %s (profile: %s)\n", *addr, p.Name)
	if eval != nil {
		fmt.Fprintf(os.Stderr, "ntnbox: device=sandbox-0  condition/lookahead/events ready\n")
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
