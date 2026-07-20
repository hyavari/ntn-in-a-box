package netem

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

// Executor abstracts shell command execution so the controller can be
// tested without running real tc/ip commands.
type Executor interface {
	// Run executes a command with the given arguments and returns any
	// error. The context can be used for cancellation/timeout.
	Run(ctx context.Context, name string, args ...string) error
}

// ExecReal is the production Executor that shells out via os/exec.
type ExecReal struct{}

// Run executes the command via os/exec.
func (ExecReal) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

const defaultControlExemptIP = "10.200.0.1"

// Controller translates LinkState values into tc qdisc commands for a
// specific device inside a network namespace.
//
// Traffic to ControlExemptIP (the veth gateway / API host) bypasses
// netem so condition/SSE control-plane calls keep working during
// out-of-coverage 100% loss.
type Controller struct {
	// Netns is the network namespace name (e.g. "ntnbox-ue-1").
	Netns string

	// Device is the interface name inside the namespace (e.g. "veth-inner").
	Device string

	// ControlExemptIP is never shaped. Empty defaults to 10.200.0.1.
	ControlExemptIP string

	// Exec is the command executor (real or mock).
	Exec Executor
}

func (c *Controller) exemptIP() string {
	if c.ControlExemptIP != "" {
		return c.ControlExemptIP
	}
	return defaultControlExemptIP
}

// Setup installs a prio root qdisc, netem on band 1:2, and a filter that
// sends ControlExemptIP traffic to the unshaped band 1:1.
//
// priomap sends every TOS class to band 1 (class 1:2 / netem). Only the
// explicit dst filter uses band 0 (class 1:1), so non-API traffic cannot
// bypass shaping via the default prio map.
// Must be called once before Apply or SetFullLoss.
func (c *Controller) Setup(ctx context.Context, state condition.LinkState) error {
	// 16 entries → band 1 for all priority classes (tc-prio priomap).
	priomap := []string{
		"1", "1", "1", "1", "1", "1", "1", "1",
		"1", "1", "1", "1", "1", "1", "1", "1",
	}
	prioArgs := []string{
		"netns", "exec", c.Netns,
		"tc", "qdisc", "add", "dev", c.Device, "root", "handle", "1:",
		"prio", "bands", "2", "priomap",
	}
	prioArgs = append(prioArgs, priomap...)

	steps := [][]string{
		prioArgs,
		append([]string{
			"netns", "exec", c.Netns,
			"tc", "qdisc", "add", "dev", c.Device, "parent", "1:2", "handle", "20:", "netem",
		}, netemParams(state)...),
		{
			"netns", "exec", c.Netns,
			"tc", "filter", "add", "dev", c.Device, "protocol", "ip", "parent", "1:", "prio", "1",
			"u32", "match", "ip", "dst", c.exemptIP() + "/32", "flowid", "1:1",
		},
	}
	for _, args := range steps {
		if err := c.Exec.Run(ctx, "ip", args...); err != nil {
			return err
		}
	}
	return nil
}

// Apply updates the netem qdisc with new impairment values.
func (c *Controller) Apply(ctx context.Context, state condition.LinkState) error {
	args := append([]string{
		"netns", "exec", c.Netns,
		"tc", "qdisc", "change", "dev", c.Device, "parent", "1:2", "handle", "20:", "netem",
	}, netemParams(state)...)
	return c.Exec.Run(ctx, "ip", args...)
}

// SetFullLoss sets 100% packet loss — used when coverage is lost.
// Control-plane traffic to ControlExemptIP remains unshaped.
func (c *Controller) SetFullLoss(ctx context.Context) error {
	args := []string{
		"netns", "exec", c.Netns,
		"tc", "qdisc", "change", "dev", c.Device, "parent", "1:2", "handle", "20:", "netem",
		"loss", "100%",
	}
	return c.Exec.Run(ctx, "ip", args...)
}

// Teardown removes the root qdisc (prio + children + filters).
func (c *Controller) Teardown(ctx context.Context) error {
	args := []string{
		"netns", "exec", c.Netns,
		"tc", "qdisc", "del", "dev", c.Device, "root",
	}
	return c.Exec.Run(ctx, "ip", args...)
}

func netemParams(state condition.LinkState) []string {
	delay := fmt.Sprintf("%.0fms", state.DelayMs)
	jitter := fmt.Sprintf("%.0fms", state.JitterMs)
	loss := fmt.Sprintf("%.2f%%", state.LossPct)
	rate := fmt.Sprintf("%.0fkbit", state.BandwidthKbps)
	return []string{
		"delay", delay, jitter,
		"loss", loss,
		"rate", rate,
	}
}
