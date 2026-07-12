package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/apihost"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/imsadapter"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
	"github.com/hyavari/ntn-in-a-box/internal/module/messaging"
)

// newAPIHost builds the API server (messaging wired) but does not listen yet.
// Callers must set OnDeviceRegistered (if needed) before listenAPIHost so
// early POST /devices cannot race past an unset hook.
func newAPIHost(bus *eventbus.Bus, registry *device.Registry, eval condition.Eval, sessInfo *apihost.SessionInfo, profiles ...*profile.Profile) *apihost.Server {
	srv := apihost.New(apihost.Config{
		Profiles:    profiles,
		Registry:    registry,
		Bus:         bus,
		Evaluator:   eval,
		SessionInfo: sessInfo,
	})
	if eval != nil {
		srv.RegisterEvaluator("sandbox-0", eval)
	}

	if bus != nil && registry != nil {
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
	}
	return srv
}

// listenAPIHost starts serving addr in a goroutine.
// Prefer loopback: bare ":port" becomes 127.0.0.1:port; non-loopback binds warn.
func listenAPIHost(srv *apihost.Server, addr string, eval condition.Eval) {
	addr = normalizeListenAddr(addr)
	go func() {
		port := addrPort(addr)
		fmt.Fprintf(os.Stderr, "ntnbox: API listening on %s  device=sandbox-0\n", addr)
		fmt.Fprintf(os.Stderr, "ntnbox: GUI available at http://localhost:%s/ui\n", port)
		if eval != nil {
			fmt.Fprintf(os.Stderr, "ntnbox: condition GET http://localhost:%s/devices/sandbox-0/condition\n", port)
		}
		_ = srv.ListenAndServe(addr)
	}()
}

// startAPIHost is a convenience for callers that need no OnDeviceRegistered hook.
func startAPIHost(addr string, bus *eventbus.Bus, registry *device.Registry, eval condition.Eval, sessInfo *apihost.SessionInfo, profiles ...*profile.Profile) *apihost.Server {
	srv := newAPIHost(bus, registry, eval, sessInfo, profiles...)
	listenAPIHost(srv, addr, eval)
	return srv
}

// dockerHostPublishSpec returns the Docker -p publish spec for the API port.
// Loopback-oriented CLI addrs map to 127.0.0.1:port:port so messaging is not
// exposed on all host interfaces; explicit LAN binds keep port:port.
func dockerHostPublishSpec(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if !strings.Contains(addr, ":") {
			port = addr
			host = ""
		} else {
			return addrPort(addr) + ":" + addrPort(addr)
		}
	}
	if host == "" || host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return net.JoinHostPort("127.0.0.1", port) + ":" + port
	}
	return port + ":" + port
}

// normalizeListenAddr maps ":8080" / "8080" to 127.0.0.1 so messaging is not
// exposed on all interfaces by accident. Explicit 0.0.0.0 / LAN hosts are kept
// with a stderr warning.
func normalizeListenAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// Allow "8080" as port-only.
		if !strings.Contains(addr, ":") {
			host, port = "", addr
		} else {
			return addr
		}
	}
	if host == "" {
		normalized := net.JoinHostPort("127.0.0.1", port)
		fmt.Fprintf(os.Stderr, "ntnbox: --addr %q binds all interfaces; using %s (pass 0.0.0.0:%s for LAN)\n",
			addr, normalized, port)
		return normalized
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		fmt.Fprintf(os.Stderr, "ntnbox: warning: API (incl. messaging bodies) reachable on %s — use 127.0.0.1 for local-only\n", addr)
	}
	return net.JoinHostPort(host, port)
}
