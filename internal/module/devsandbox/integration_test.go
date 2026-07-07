//go:build linux

package devsandbox_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/driver"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/netem"
	"github.com/hyavari/ntn-in-a-box/internal/module/devsandbox/netns"
)

// TestIntegrationFullLoop exercises the full Dev Sandbox pipeline on
// Linux: namespace + netem + driver loop + module, running a real
// command inside the shaped namespace.
//
// Requires:
//   - Linux (build tag)
//   - Root privileges (CAP_NET_ADMIN for netns/tc)
//
// Skipped otherwise.
func TestIntegrationFullLoop(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("skipping: requires root (CAP_NET_ADMIN)")
	}

	// Use a fast profile: 2s window, 4s period, tight curves.
	p := profile.Profile{
		Name: "integration_test",
		Schedule: profile.Schedule{
			Mode:         profile.ModePeriodic,
			PeriodSec:    4,
			WindowSec:    2,
			LookaheadSec: 0.5,
		},
		Curves: profile.Curves{
			DelayMs:       []profile.Point{{OffsetSec: 0, Value: 50}, {OffsetSec: 2, Value: 20}},
			JitterMs:      []profile.Point{{OffsetSec: 0, Value: 5}, {OffsetSec: 2, Value: 2}},
			LossPct:       []profile.Point{{OffsetSec: 0, Value: 0}, {OffsetSec: 2, Value: 0}},
			BandwidthKbps: []profile.Point{{OffsetSec: 0, Value: 10000}, {OffsetSec: 2, Value: 10000}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create evaluator.
	epoch := time.Now()
	eval, err := condition.NewEvaluator(p, epoch)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	// Create namespace.
	ns := netns.New("integ-test", netns.ExecReal{})
	if err := ns.Create(ctx); err != nil {
		t.Fatalf("namespace Create: %v", err)
	}
	defer func() {
		_ = ns.Destroy(context.Background())
	}()

	// Create netem controller.
	ctrl := &netem.Controller{
		Netns:  ns.Name,
		Device: ns.VethInner,
		Exec:   netem.ExecReal{},
	}

	// Set up initial qdisc.
	initialState := condition.LinkState{
		DelayMs:       p.Curves.DelayMs[0].Value,
		JitterMs:      p.Curves.JitterMs[0].Value,
		LossPct:       p.Curves.LossPct[0].Value,
		BandwidthKbps: p.Curves.BandwidthKbps[0].Value,
	}
	if err := ctrl.Setup(ctx, initialState); err != nil {
		t.Fatalf("netem Setup: %v", err)
	}

	// Create module + bus.
	bus := eventbus.New(eventbus.DefaultLinkStateThrottle)
	sandbox := devsandbox.New(ctrl)
	sandbox.Emit(bus)
	bus.SubscribeCoverage(sandbox.OnCoverageEvent)
	bus.SubscribeLinkState(sandbox.OnLinkState)

	// Collect coverage events for verification.
	var mu sync.Mutex
	var covEvents []eventbus.CoverageEvent
	bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) {
		mu.Lock()
		covEvents = append(covEvents, ev)
		mu.Unlock()
	})

	// Start driver loop with a fast tick (50ms).
	loop := driver.New(driver.Config{
		Evaluator:    eval,
		Bus:          bus,
		LookaheadSec: p.Schedule.LookaheadSec,
		Interval:     50 * time.Millisecond,
	})

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	go loop.Run(loopCtx)

	// Run ping inside the namespace — should succeed with added latency.
	cmd := ns.Command("ping", "-c", "3", "-W", "2", ns.InnerAddr())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ping failed: %v\noutput: %s", err, out)
	}

	// Verify ping succeeded (output contains "3 packets transmitted").
	if !strings.Contains(string(out), "3 packets transmitted") {
		t.Errorf("unexpected ping output: %s", out)
	}

	// Let the driver loop run long enough to hit a coverage transition.
	// Wait until we see at least window_opened + window_closing.
	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		n := len(covEvents)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for coverage events; got %d", n)
		case <-time.After(50 * time.Millisecond):
		}
	}

	loopCancel()

	mu.Lock()
	defer mu.Unlock()

	// First event should be window_opened (we start in coverage).
	if covEvents[0].Kind != eventbus.KindWindowOpened {
		t.Errorf("first event = %q, want window_opened", covEvents[0].Kind)
	}

	t.Logf("integration test passed: %d coverage events, ping succeeded inside namespace", len(covEvents))
}
