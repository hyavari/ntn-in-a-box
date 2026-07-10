package main

import (
	"fmt"
	"os"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/apihost"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// startAPIHost starts the API host in a goroutine and returns the
// server so callers can register module routes.
func startAPIHost(addr string, bus *eventbus.Bus, registry *device.Registry, eval condition.Eval, profiles ...*profile.Profile) *apihost.Server {
	srv := apihost.New(apihost.Config{
		Profiles:  profiles,
		Registry:  registry,
		Bus:       bus,
		Evaluator: eval,
	})
	if eval != nil {
		srv.RegisterEvaluator("sandbox-0", eval)
	}
	go func() {
		fmt.Fprintf(os.Stderr, "ntnbox: API listening on %s\n", addr)
		fmt.Fprintf(os.Stderr, "ntnbox: GUI available at http://localhost:%s/ui\n", addrPort(addr))
		_ = srv.ListenAndServe(addr)
	}()
	return srv
}
