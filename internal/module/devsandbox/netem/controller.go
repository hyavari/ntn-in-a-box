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

// Controller translates LinkState values into tc qdisc commands for a
// specific device inside a network namespace.
type Controller struct {
	// Netns is the network namespace name (e.g. "ntnbox-ue-1").
	Netns string

	// Device is the interface name inside the namespace (e.g. "veth-inner").
	Device string

	// Exec is the command executor (real or mock).
	Exec Executor
}

// Setup adds the initial netem qdisc to the device inside the namespace.
// Must be called once before Apply or SetFullLoss.
func (c *Controller) Setup(ctx context.Context, state condition.LinkState) error {
	args := c.buildArgs("add", state)
	return c.Exec.Run(ctx, "ip", args...)
}

// Apply updates the netem qdisc with new impairment values.
func (c *Controller) Apply(ctx context.Context, state condition.LinkState) error {
	args := c.buildArgs("change", state)
	return c.Exec.Run(ctx, "ip", args...)
}

// SetFullLoss sets 100% packet loss — used when coverage is lost.
// All other parameters are zeroed to avoid confusing interactions.
func (c *Controller) SetFullLoss(ctx context.Context) error {
	args := []string{
		"netns", "exec", c.Netns,
		"tc", "qdisc", "change", "dev", c.Device, "root", "netem",
		"loss", "100%",
	}
	return c.Exec.Run(ctx, "ip", args...)
}

// Teardown removes the netem qdisc. Typically unnecessary if the
// namespace is being deleted (which removes everything), but provided
// for completeness.
func (c *Controller) Teardown(ctx context.Context) error {
	args := []string{
		"netns", "exec", c.Netns,
		"tc", "qdisc", "del", "dev", c.Device, "root",
	}
	return c.Exec.Run(ctx, "ip", args...)
}

func (c *Controller) buildArgs(action string, state condition.LinkState) []string {
	// Format: ip netns exec <ns> tc qdisc <action> dev <dev> root netem
	//   delay <d>ms <j>ms loss <l>% rate <bw>kbit
	delay := fmt.Sprintf("%.0fms", state.DelayMs)
	jitter := fmt.Sprintf("%.0fms", state.JitterMs)
	loss := fmt.Sprintf("%.2f%%", state.LossPct)
	rate := fmt.Sprintf("%.0fkbit", state.BandwidthKbps)

	return []string{
		"netns", "exec", c.Netns,
		"tc", "qdisc", action, "dev", c.Device, "root", "netem",
		"delay", delay, jitter,
		"loss", loss,
		"rate", rate,
	}
}
