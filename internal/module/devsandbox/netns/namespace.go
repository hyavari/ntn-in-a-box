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
// routing for traffic shaping.
type Namespace struct {
	// Name is the namespace name (e.g. "ntnbox-ue-1").
	Name string

	// VethOuter is the host-side veth interface name.
	VethOuter string

	// VethInner is the namespace-side veth interface name.
	VethInner string

	// Subnet is the /30 subnet used for the veth pair (e.g. "10.200.0").
	// Outer gets .1, inner gets .2.
	Subnet string

	// Exec is the command executor (real or mock).
	Exec Executor
}

// New creates a Namespace with sensible defaults derived from a device ID.
func New(deviceID string, exec Executor) *Namespace {
	return &Namespace{
		Name:      "ntnbox-" + deviceID,
		VethOuter: "veth-" + deviceID + "-o",
		VethInner: "veth-" + deviceID + "-i",
		Subnet:    "10.200.0",
		Exec:      exec,
	}
}

// Create sets up the network namespace, veth pair, addressing, routing,
// and NAT. Must be called before Exec or before the netem controller
// operates on VethInner.
func (ns *Namespace) Create(ctx context.Context) error {
	steps := []struct {
		name string
		args []string
	}{
		// Create the namespace.
		{"ip", []string{"netns", "add", ns.Name}},

		// Create the veth pair.
		{"ip", []string{"link", "add", ns.VethOuter, "type", "veth", "peer", "name", ns.VethInner}},

		// Move the inner interface into the namespace.
		{"ip", []string{"link", "set", ns.VethInner, "netns", ns.Name}},

		// Assign addresses.
		{"ip", []string{"addr", "add", ns.Subnet + ".1/30", "dev", ns.VethOuter}},
		{"ip", []string{"netns", "exec", ns.Name, "ip", "addr", "add", ns.Subnet + ".2/30", "dev", ns.VethInner}},

		// Bring interfaces up.
		{"ip", []string{"link", "set", ns.VethOuter, "up"}},
		{"ip", []string{"netns", "exec", ns.Name, "ip", "link", "set", ns.VethInner, "up"}},
		{"ip", []string{"netns", "exec", ns.Name, "ip", "link", "set", "lo", "up"}},

		// Default route inside the namespace.
		{"ip", []string{"netns", "exec", ns.Name, "ip", "route", "add", "default", "via", ns.Subnet + ".1"}},

		// Enable NAT so the namespace can reach the internet.
		{"iptables", []string{"-t", "nat", "-A", "POSTROUTING", "-s", ns.Subnet + ".0/30", "-j", "MASQUERADE"}},
	}

	for _, step := range steps {
		if err := ns.Exec.Run(ctx, step.name, step.args...); err != nil {
			// Best-effort cleanup on failure.
			_ = ns.Destroy(context.Background())
			return fmt.Errorf("netns create: %s %v: %w", step.name, step.args, err)
		}
	}
	return nil
}

// Command returns an *exec.Cmd configured to run inside the namespace.
// The caller is responsible for starting/waiting on the command.
func (ns *Namespace) Command(name string, args ...string) *exec.Cmd {
	fullArgs := append([]string{"netns", "exec", ns.Name, name}, args...)
	return exec.Command("ip", fullArgs...)
}

// Destroy removes the namespace and the NAT rule. Deleting the
// namespace also removes the veth pair automatically.
func (ns *Namespace) Destroy(ctx context.Context) error {
	// Remove NAT rule (best-effort, don't fail if it doesn't exist).
	_ = ns.Exec.Run(ctx, "iptables", "-t", "nat", "-D", "POSTROUTING", "-s", ns.Subnet+".0/30", "-j", "MASQUERADE")

	// Delete the namespace (also removes veth pair).
	return ns.Exec.Run(ctx, "ip", "netns", "del", ns.Name)
}

// InnerAddr returns the IP address assigned to the inner veth interface.
func (ns *Namespace) InnerAddr() string {
	return ns.Subnet + ".2"
}
