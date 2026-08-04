package netns

import (
	"context"
	"fmt"
	"os/exec"
)

// Executor abstracts shell command execution so the namespace wrapper
// can be tested without running real ip/iptables commands. Structurally
// identical to netem.Executor — Go's structural typing makes them
// interchangeable without a shared import.
type Executor interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecReal is the production Executor that shells out via os/exec.
type ExecReal struct{}

// Run executes the command via os/exec.
func (ExecReal) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

// Namespace manages a Linux network namespace with a veth pair and NAT
// routing for traffic shaping. When terrestrial fallback is enabled,
// a second veth pair is created and SetDefaultVia switches the active
// default route between satellite and terrestrial gateways.
type Namespace struct {
	// Name is the namespace name (e.g. "ntnbox-sandbox-0").
	Name string

	// VethOuter is the host-side satellite veth interface name.
	VethOuter string

	// VethInner is the namespace-side satellite veth interface name.
	VethInner string

	// Subnet is the /30 subnet prefix for the satellite veth pair
	// (e.g. "10.200.0"). Outer gets .1, inner gets .2.
	Subnet string

	// TerrVethOuter / TerrVethInner / TerrSubnet describe the optional
	// terrestrial egress. Empty TerrSubnet means single-path mode.
	TerrVethOuter string
	TerrVethInner string
	TerrSubnet    string

	// Exec is the command executor (real or mock).
	Exec Executor
}

// New creates a Namespace with sensible defaults derived from a device ID.
func New(deviceID string, exec Executor) *Namespace {
	return &Namespace{
		Name:      "ntnbox-" + deviceID,
		VethOuter: "vth-" + short(deviceID) + "-o",
		VethInner: "vth-" + short(deviceID) + "-i",
		Subnet:    "10.200.0",
		Exec:      exec,
	}
}

// EnableTerrestrial configures a second egress path (10.200.1.0/30 by default).
// Must be called before Create.
func (ns *Namespace) EnableTerrestrial() {
	id := shortTerr(ns.Name)
	ns.TerrVethOuter = "vth-" + id + "t-o"
	ns.TerrVethInner = "vth-" + id + "t-i"
	ns.TerrSubnet = "10.200.1"
}

// DualPath reports whether a terrestrial egress is configured.
func (ns *Namespace) DualPath() bool {
	return ns.TerrSubnet != ""
}

// SatGateway returns the host-side satellite gateway IP.
func (ns *Namespace) SatGateway() string {
	return ns.Subnet + ".1"
}

// TerrGateway returns the host-side terrestrial gateway IP.
func (ns *Namespace) TerrGateway() string {
	return ns.TerrSubnet + ".1"
}

// short truncates an ID to fit within Linux's 15-char interface name
// limit (15 - len("vth-") - len("-o") = 9 chars max).
func short(id string) string {
	const maxLen = 9
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}

// shortTerr truncates for terrestrial names: vth-XXXXXXXt-o (15 chars → 7).
func shortTerr(name string) string {
	id := name
	if len(id) > len("ntnbox-") && id[:len("ntnbox-")] == "ntnbox-" {
		id = id[len("ntnbox-"):]
	}
	const maxLen = 7
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}

// Create sets up the network namespace, veth pair(s), addressing, routing,
// and NAT. Must be called before Exec or before the netem controller
// operates on VethInner.
//
// If a namespace with the same name already exists (e.g. from an
// unclean previous exit), it is destroyed first.
func (ns *Namespace) Create(ctx context.Context) error {
	// Clean up stale namespace from a previous unclean exit.
	_ = ns.Exec.Run(ctx, "ip", "netns", "delete", ns.Name)

	steps := []struct {
		name string
		args []string
	}{
		{"ip", []string{"netns", "add", ns.Name}},

		{"ip", []string{"link", "add", ns.VethOuter, "type", "veth", "peer", "name", ns.VethInner}},
		{"ip", []string{"link", "set", ns.VethInner, "netns", ns.Name}},
		{"ip", []string{"addr", "add", ns.Subnet + ".1/30", "dev", ns.VethOuter}},
		{"ip", []string{"netns", "exec", ns.Name, "ip", "addr", "add", ns.Subnet + ".2/30", "dev", ns.VethInner}},
		{"ip", []string{"link", "set", ns.VethOuter, "up"}},
		{"ip", []string{"netns", "exec", ns.Name, "ip", "link", "set", ns.VethInner, "up"}},
		{"ip", []string{"netns", "exec", ns.Name, "ip", "link", "set", "lo", "up"}},
		{"ip", []string{"netns", "exec", ns.Name, "ip", "route", "add", "default", "via", ns.Subnet + ".1"}},
		{"iptables", []string{"-t", "nat", "-A", "POSTROUTING", "-s", ns.Subnet + ".0/30", "-j", "MASQUERADE"}},
	}

	if ns.DualPath() {
		terr := []struct {
			name string
			args []string
		}{
			{"ip", []string{"link", "add", ns.TerrVethOuter, "type", "veth", "peer", "name", ns.TerrVethInner}},
			{"ip", []string{"link", "set", ns.TerrVethInner, "netns", ns.Name}},
			{"ip", []string{"addr", "add", ns.TerrSubnet + ".1/30", "dev", ns.TerrVethOuter}},
			{"ip", []string{"netns", "exec", ns.Name, "ip", "addr", "add", ns.TerrSubnet + ".2/30", "dev", ns.TerrVethInner}},
			{"ip", []string{"link", "set", ns.TerrVethOuter, "up"}},
			{"ip", []string{"netns", "exec", ns.Name, "ip", "link", "set", ns.TerrVethInner, "up"}},
			{"iptables", []string{"-t", "nat", "-A", "POSTROUTING", "-s", ns.TerrSubnet + ".0/30", "-j", "MASQUERADE"}},
		}
		steps = append(steps, terr...)
	}

	for _, step := range steps {
		if err := ns.Exec.Run(ctx, step.name, step.args...); err != nil {
			_ = ns.Destroy(context.Background())
			return fmt.Errorf("netns create: %s %v: %w", step.name, step.args, err)
		}
	}
	return nil
}

// SetDefaultVia replaces the namespace default route with via gatewayIP.
// Uses `ip route replace` so a failed switch does not leave the netns
// without a default route.
func (ns *Namespace) SetDefaultVia(ctx context.Context, gatewayIP string) error {
	if err := ns.Exec.Run(ctx, "ip", "netns", "exec", ns.Name, "ip", "route", "replace", "default", "via", gatewayIP); err != nil {
		return fmt.Errorf("netns set default via %s: %w", gatewayIP, err)
	}
	return nil
}

// Command returns an *exec.Cmd configured to run inside the namespace.
// The caller is responsible for starting/waiting on the command.
func (ns *Namespace) Command(name string, args ...string) *exec.Cmd {
	fullArgs := append([]string{"netns", "exec", ns.Name, name}, args...)
	return exec.Command("ip", fullArgs...)
}

// Destroy removes the namespace and the NAT rule(s). Deleting the
// namespace also removes the veth pair(s) automatically.
func (ns *Namespace) Destroy(ctx context.Context) error {
	_ = ns.Exec.Run(ctx, "iptables", "-t", "nat", "-D", "POSTROUTING", "-s", ns.Subnet+".0/30", "-j", "MASQUERADE")
	if ns.DualPath() {
		_ = ns.Exec.Run(ctx, "iptables", "-t", "nat", "-D", "POSTROUTING", "-s", ns.TerrSubnet+".0/30", "-j", "MASQUERADE")
	}
	return ns.Exec.Run(ctx, "ip", "netns", "del", ns.Name)
}

// InnerAddr returns the IP address assigned to the inner satellite veth.
func (ns *Namespace) InnerAddr() string {
	return ns.Subnet + ".2"
}
