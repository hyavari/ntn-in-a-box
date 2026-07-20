package netem

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

// mockExec records all commands executed.
type mockExec struct {
	mu   sync.Mutex
	cmds []string
}

func (m *mockExec) Run(_ context.Context, name string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cmds = append(m.cmds, name+" "+strings.Join(args, " "))
	return nil
}

func (m *mockExec) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cmds) == 0 {
		return ""
	}
	return m.cmds[len(m.cmds)-1]
}

func (m *mockExec) all() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.cmds...)
}

func TestSetup(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:  "ntnbox-ue-1",
		Device: "veth-inner",
		Exec:   mock,
	}

	state := condition.LinkState{
		DelayMs:       100,
		JitterMs:      20,
		LossPct:       5,
		BandwidthKbps: 10000,
	}

	if err := ctrl.Setup(context.Background(), state); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	cmds := mock.all()
	if len(cmds) != 3 {
		t.Fatalf("Setup issued %d cmds, want 3:\n%s", len(cmds), strings.Join(cmds, "\n"))
	}
	if !strings.Contains(cmds[0], "root handle 1: prio bands 2 priomap") {
		t.Errorf("Setup[0] prio root with pinned priomap: %s", cmds[0])
	}
	if !strings.Contains(cmds[0], "1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1") {
		t.Errorf("Setup[0] missing all-band-1 priomap: %s", cmds[0])
	}
	wantNetem := "parent 1:2 handle 20: netem delay 100ms 20ms loss 5.00% rate 10000kbit"
	if !strings.Contains(cmds[1], wantNetem) {
		t.Errorf("Setup[1] netem:\n  got:  %s\n  want containing: %s", cmds[1], wantNetem)
	}
	if !strings.Contains(cmds[2], "match ip dst 10.200.0.1/32 flowid 1:1") {
		t.Errorf("Setup[2] exempt filter: %s", cmds[2])
	}
}

func TestApply(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:  "ntnbox-ue-1",
		Device: "veth-inner",
		Exec:   mock,
	}

	state := condition.LinkState{
		DelayMs:       40,
		JitterMs:      5,
		LossPct:       0.2,
		BandwidthKbps: 20000,
	}

	if err := ctrl.Apply(context.Background(), state); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := mock.last()
	want := "ip netns exec ntnbox-ue-1 tc qdisc change dev veth-inner parent 1:2 handle 20: netem delay 40ms 5ms loss 0.20% rate 20000kbit"
	if got != want {
		t.Errorf("Apply command:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestApplyZeroJitter(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:  "ntnbox-ue-1",
		Device: "veth-inner",
		Exec:   mock,
	}

	state := condition.LinkState{
		DelayMs:       50,
		JitterMs:      0,
		LossPct:       1,
		BandwidthKbps: 5000,
	}

	if err := ctrl.Apply(context.Background(), state); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := mock.last()
	want := "ip netns exec ntnbox-ue-1 tc qdisc change dev veth-inner parent 1:2 handle 20: netem delay 50ms 0ms loss 1.00% rate 5000kbit"
	if got != want {
		t.Errorf("Apply (zero jitter):\n  got:  %s\n  want: %s", got, want)
	}
}

func TestApplyZeroLoss(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:  "ntnbox-ue-1",
		Device: "veth-inner",
		Exec:   mock,
	}

	state := condition.LinkState{
		DelayMs:       30,
		JitterMs:      2,
		LossPct:       0,
		BandwidthKbps: 50000,
	}

	if err := ctrl.Apply(context.Background(), state); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := mock.last()
	want := "ip netns exec ntnbox-ue-1 tc qdisc change dev veth-inner parent 1:2 handle 20: netem delay 30ms 2ms loss 0.00% rate 50000kbit"
	if got != want {
		t.Errorf("Apply (zero loss):\n  got:  %s\n  want: %s", got, want)
	}
}

func TestApplyHighBandwidth(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:  "ntnbox-ue-1",
		Device: "veth-inner",
		Exec:   mock,
	}

	state := condition.LinkState{
		DelayMs:       20,
		JitterMs:      3,
		LossPct:       0.1,
		BandwidthKbps: 100000,
	}

	if err := ctrl.Apply(context.Background(), state); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := mock.last()
	want := "ip netns exec ntnbox-ue-1 tc qdisc change dev veth-inner parent 1:2 handle 20: netem delay 20ms 3ms loss 0.10% rate 100000kbit"
	if got != want {
		t.Errorf("Apply (high bandwidth):\n  got:  %s\n  want: %s", got, want)
	}
}

func TestSetFullLoss(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:  "ntnbox-ue-1",
		Device: "veth-inner",
		Exec:   mock,
	}

	if err := ctrl.SetFullLoss(context.Background()); err != nil {
		t.Fatalf("SetFullLoss: %v", err)
	}

	got := mock.last()
	want := "ip netns exec ntnbox-ue-1 tc qdisc change dev veth-inner parent 1:2 handle 20: netem loss 100%"
	if got != want {
		t.Errorf("SetFullLoss command:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestTeardown(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:  "ntnbox-ue-1",
		Device: "veth-inner",
		Exec:   mock,
	}

	if err := ctrl.Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	got := mock.last()
	want := "ip netns exec ntnbox-ue-1 tc qdisc del dev veth-inner root"
	if got != want {
		t.Errorf("Teardown command:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestSequenceSetupApplyFullLossTeardown(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:  "ntnbox-ue-1",
		Device: "veth-inner",
		Exec:   mock,
	}

	ctx := context.Background()
	state := condition.LinkState{DelayMs: 100, JitterMs: 10, LossPct: 5, BandwidthKbps: 2000}

	_ = ctrl.Setup(ctx, state)
	_ = ctrl.Apply(ctx, condition.LinkState{DelayMs: 40, JitterMs: 5, LossPct: 0.2, BandwidthKbps: 20000})
	_ = ctrl.SetFullLoss(ctx)
	_ = ctrl.Teardown(ctx)

	cmds := mock.all()
	// Setup=3, Apply=1, FullLoss=1, Teardown=1
	if len(cmds) != 6 {
		t.Fatalf("got %d commands, want 6:\n%s", len(cmds), strings.Join(cmds, "\n"))
	}
}

func TestControlExemptIPOverride(t *testing.T) {
	mock := &mockExec{}
	ctrl := &Controller{
		Netns:           "ntnbox-ue-1",
		Device:          "veth-inner",
		ControlExemptIP: "10.9.9.1",
		Exec:            mock,
	}
	_ = ctrl.Setup(context.Background(), condition.LinkState{DelayMs: 1, BandwidthKbps: 1000})
	cmds := mock.all()
	if !strings.Contains(cmds[2], "dst 10.9.9.1/32") {
		t.Fatalf("filter should use override IP: %s", cmds[2])
	}
}
