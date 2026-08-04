package main

import (
	"context"
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
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/netem"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/netns"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/path"
	"github.com/hyavari/ntn-in-a-box/internal/module/messaging"
)

// setupDualPath configures the terrestrial netem path and returns sandbox
// options. When fallback is false, returns empty options.
func setupDualPath(
	ctx context.Context,
	ns *netns.Namespace,
	bus *eventbus.Bus,
	terrImp profile.TerrestrialImpairments,
	fallback bool,
) (devsandbox.Options, error) {
	if !fallback {
		return devsandbox.Options{}, nil
	}
	terrCtrl := &netem.Controller{
		Netns:           ns.Name,
		Device:          ns.TerrVethInner,
		ControlExemptIP: ns.TerrGateway(),
		Exec:            netem.ExecReal{},
	}
	terrState := condition.LinkState{
		DelayMs:       terrImp.DelayMs,
		JitterMs:      terrImp.JitterMs,
		LossPct:       terrImp.LossPctValue(),
		BandwidthKbps: terrImp.BandwidthKbps,
	}
	if err := terrCtrl.Setup(ctx, terrState); err != nil {
		return devsandbox.Options{}, fmt.Errorf("setting up terrestrial netem: %w", err)
	}
	return devsandbox.Options{
		Router:      ns,
		Paths:       path.New(true),
		Bus:         bus,
		DeviceID:    "sandbox-0",
		SatGateway:  ns.SatGateway(),
		TerrGateway: ns.TerrGateway(),
	}, nil
}

// newAPIHost builds the API server (messaging wired) but does not listen yet.
// Callers must set OnDeviceRegistered (if needed) before listenAPIHost so
// early POST /devices cannot race past an unset hook.
// quietMessaging suppresses stderr message breadcrumbs (for --tui).
func newAPIHost(bus *eventbus.Bus, registry *device.Registry, eval condition.Eval, sessInfo *apihost.SessionInfo, quietMessaging bool, profiles ...*profile.Profile) *apihost.Server {
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
			Bus:   bus,
			Quiet: quietMessaging,
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
	_ = listenAPIHosts(srv, []string{normalizeListenAddr(addr)}, "", eval)
}

// listenAPIHosts binds each addr, logs only successful listens, and serves
// them (shared mux). Bind failures are reported instead of claiming success.
// It returns sandboxAPIBase only when a successful listen covers that base
// (exact match or same-port wildcard); otherwise children should not inherit it.
func listenAPIHosts(srv *apihost.Server, addrs []string, sandboxAPIBase string, eval condition.Eval) string {
	if len(addrs) == 0 {
		return ""
	}
	type bound struct {
		addr string
		ln   net.Listener
	}
	var listeners []bound
	for _, a := range addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ntnbox: API listen %s failed: %v\n", a, err)
			continue
		}
		listeners = append(listeners, bound{addr: a, ln: ln})
	}
	if len(listeners) == 0 {
		fmt.Fprintf(os.Stderr, "ntnbox: API failed to bind any of %v\n", addrs)
		return ""
	}
	effectiveBase := ""
	if sandboxAPIBase != "" {
		want := strings.TrimPrefix(strings.TrimPrefix(sandboxAPIBase, "https://"), "http://")
		boundAddrs := make([]string, len(listeners))
		for i, b := range listeners {
			boundAddrs[i] = b.addr
		}
		if sandboxBaseCovered(want, boundAddrs) {
			effectiveBase = sandboxAPIBase
		} else {
			fmt.Fprintf(os.Stderr, "ntnbox: warning: sandbox API base %s is not listening; not setting NTNBOX_API_BASE\n", sandboxAPIBase)
		}
	}

	port := addrPort(listeners[0].addr)
	for _, b := range listeners {
		fmt.Fprintf(os.Stderr, "ntnbox: API listening on %s  device=sandbox-0\n", b.addr)
	}
	fmt.Fprintf(os.Stderr, "ntnbox: GUI available at http://localhost:%s/ui\n", port)
	if eval != nil {
		fmt.Fprintf(os.Stderr, "ntnbox: condition GET http://localhost:%s/devices/sandbox-0/condition\n", port)
		if effectiveBase != "" {
			fmt.Fprintf(os.Stderr, "ntnbox: sandbox API base %s (set as NTNBOX_API_BASE)\n", effectiveBase)
		}
	}

	go func() {
		for i, b := range listeners {
			if i == len(listeners)-1 {
				if err := srv.Serve(b.ln); err != nil {
					fmt.Fprintf(os.Stderr, "ntnbox: API serve %s: %v\n", b.addr, err)
				}
				return
			}
			go func(b bound) {
				if err := srv.Serve(b.ln); err != nil {
					fmt.Fprintf(os.Stderr, "ntnbox: API serve %s: %v\n", b.addr, err)
				}
			}(b)
		}
	}()
	return effectiveBase
}

// startAPIHost is a convenience for callers that need no OnDeviceRegistered hook.
func startAPIHost(addr string, bus *eventbus.Bus, registry *device.Registry, eval condition.Eval, sessInfo *apihost.SessionInfo, profiles ...*profile.Profile) *apihost.Server {
	srv := newAPIHost(bus, registry, eval, sessInfo, false, profiles...)
	listenAPIHost(srv, addr, eval)
	return srv
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

// sandboxListenPlan is how ntnbox run exposes the API to the host and sandbox.
type sandboxListenPlan struct {
	Addrs   []string // host listen addresses
	APIBase string   // NTNBOX_API_BASE for sandbox children (http://host:port)
}

// planSandboxListen chooses listen addresses and the sandbox API base.
// Loopback stays loopback for host UI; a second bind on the veth gateway
// (10.200.0.1) lets netns clients reach the API without opening 0.0.0.0.
// Explicit 0.0.0.0 / :: keeps all-interfaces bind (LAN). Specific hosts
// also get a gateway bind so NTNBOX_API_BASE stays control-exempt (netem
// only shapes non-gateway destinations). APIBase is always the gateway.
func planSandboxListen(addr string) sandboxListenPlan {
	addr = normalizeListenAddr(addr)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return sandboxListenPlan{Addrs: []string{addr}}
	}
	gateway := net.JoinHostPort("10.200.0.1", port)
	apiBase := "http://" + gateway
	switch host {
	case "127.0.0.1":
		loopback := net.JoinHostPort("127.0.0.1", port)
		fmt.Fprintf(os.Stderr, "ntnbox: dual-binding %s and %s (sandbox via veth gateway; not exposing on LAN)\n",
			loopback, gateway)
		return sandboxListenPlan{
			Addrs:   []string{loopback, gateway},
			APIBase: apiBase,
		}
	case "::1":
		loopback6 := net.JoinHostPort("::1", port)
		fmt.Fprintf(os.Stderr, "ntnbox: dual-binding %s and %s (sandbox via veth gateway; not exposing on LAN)\n",
			loopback6, gateway)
		return sandboxListenPlan{
			Addrs:   []string{loopback6, gateway},
			APIBase: apiBase,
		}
	case "localhost":
		// Bind both families so IPv4- and IPv6-first resolvers of "localhost" work.
		v4 := net.JoinHostPort("127.0.0.1", port)
		v6 := net.JoinHostPort("::1", port)
		fmt.Fprintf(os.Stderr, "ntnbox: binding %s, %s, and %s (sandbox via veth gateway; not exposing on LAN)\n",
			v4, v6, gateway)
		return sandboxListenPlan{
			Addrs:   []string{v4, v6, gateway},
			APIBase: apiBase,
		}
	case "0.0.0.0", "::":
		return sandboxListenPlan{
			Addrs:   []string{addr},
			APIBase: apiBase,
		}
	case "10.200.0.1":
		return sandboxListenPlan{
			Addrs:   []string{addr},
			APIBase: apiBase,
		}
	default:
		fmt.Fprintf(os.Stderr, "ntnbox: also binding %s for control-exempt sandbox API access\n", gateway)
		return sandboxListenPlan{
			Addrs:   []string{addr, gateway},
			APIBase: apiBase,
		}
	}
}

// sandboxBaseCovered reports whether any successful listen covers the
// advertised sandbox host:port. Exact matches count; so do same-port
// wildcard binds (0.0.0.0 / ::), which accept traffic to the veth gateway.
func sandboxBaseCovered(wantHostPort string, listeners []string) bool {
	_, wantPort, err := net.SplitHostPort(wantHostPort)
	if err != nil {
		return false
	}
	for _, addr := range listeners {
		if addr == wantHostPort {
			return true
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil || port != wantPort {
			continue
		}
		if host == "0.0.0.0" || host == "::" {
			return true
		}
	}
	return false
}
