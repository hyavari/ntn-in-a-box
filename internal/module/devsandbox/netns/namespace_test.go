package netns

import (
	"context"
	"strings"
	"sync"
	"testing"
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

func (m *mockExec) all() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.cmds...)
}

func TestNew(t *testing.T) {
	mock := &mockExec{}
	ns := New("ue-1", mock)

	if ns.Name != "ntnbox-ue-1" {
		t.Errorf("Name = %q, want %q", ns.Name, "ntnbox-ue-1")
	}
	if ns.VethOuter != "vth-ue-1-o" {
		t.Errorf("VethOuter = %q, want %q", ns.VethOuter, "vth-ue-1-o")
	}
	if ns.VethInner != "vth-ue-1-i" {
		t.Errorf("VethInner = %q, want %q", ns.VethInner, "vth-ue-1-i")
	}
	if ns.Subnet != "10.200.0" {
		t.Errorf("Subnet = %q, want %q", ns.Subnet, "10.200.0")
	}
	if ns.InnerAddr() != "10.200.0.2" {
		t.Errorf("InnerAddr = %q, want %q", ns.InnerAddr(), "10.200.0.2")
	}
}

func TestCreateCommandSequence(t *testing.T) {
	mock := &mockExec{}
	ns := New("ue-1", mock)

	if err := ns.Create(context.Background()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	cmds := mock.all()

	// Should be exactly 11 commands (1 cleanup + 10 setup).
	if len(cmds) != 11 {
		t.Fatalf("got %d commands, want 11:\n%s", len(cmds), strings.Join(cmds, "\n"))
	}

	// Verify key commands in order.
	wantContains := []string{
		"ip netns delete ntnbox-ue-1",
		"ip netns add ntnbox-ue-1",
		"ip link add vth-ue-1-o type veth peer name vth-ue-1-i",
		"ip link set vth-ue-1-i netns ntnbox-ue-1",
		"ip addr add 10.200.0.1/30 dev vth-ue-1-o",
		"ip netns exec ntnbox-ue-1 ip addr add 10.200.0.2/30 dev vth-ue-1-i",
		"ip link set vth-ue-1-o up",
		"ip netns exec ntnbox-ue-1 ip link set vth-ue-1-i up",
		"ip netns exec ntnbox-ue-1 ip link set lo up",
		"ip netns exec ntnbox-ue-1 ip route add default via 10.200.0.1",
		"iptables -t nat -A POSTROUTING -s 10.200.0.0/30 -j MASQUERADE",
	}

	for i, want := range wantContains {
		if cmds[i] != want {
			t.Errorf("cmd[%d]:\n  got:  %s\n  want: %s", i, cmds[i], want)
		}
	}
}

func TestDestroy(t *testing.T) {
	mock := &mockExec{}
	ns := New("ue-1", mock)

	if err := ns.Destroy(context.Background()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	cmds := mock.all()

	// Should be 2 commands: iptables delete + ip netns del.
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2:\n%s", len(cmds), strings.Join(cmds, "\n"))
	}

	if cmds[0] != "iptables -t nat -D POSTROUTING -s 10.200.0.0/30 -j MASQUERADE" {
		t.Errorf("destroy cmd[0]:\n  got:  %s\n  want: iptables -t nat -D ...", cmds[0])
	}
	if cmds[1] != "ip netns del ntnbox-ue-1" {
		t.Errorf("destroy cmd[1]:\n  got:  %s\n  want: ip netns del ntnbox-ue-1", cmds[1])
	}
}

func TestCommand(t *testing.T) {
	mock := &mockExec{}
	ns := New("ue-1", mock)

	cmd := ns.Command("curl", "http://example.com")

	// Verify the command is structured correctly.
	if cmd.Path == "" {
		t.Fatal("Command returned cmd with empty Path")
	}

	// Args[0] is the program name, then the arguments.
	wantArgs := []string{"ip", "netns", "exec", "ntnbox-ue-1", "curl", "http://example.com"}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("cmd.Args length = %d, want %d: %v", len(cmd.Args), len(wantArgs), cmd.Args)
	}
	for i, want := range wantArgs {
		if cmd.Args[i] != want {
			t.Errorf("cmd.Args[%d] = %q, want %q", i, cmd.Args[i], want)
		}
	}
}

func TestCreateAndDestroy(t *testing.T) {
	mock := &mockExec{}
	ns := New("phone-1", mock)

	if err := ns.Create(context.Background()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := ns.Destroy(context.Background()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	cmds := mock.all()
	// 1 cleanup + 10 create + 2 destroy = 13 total.
	if len(cmds) != 13 {
		t.Fatalf("got %d commands, want 13", len(cmds))
	}

	// Last command should be netns deletion.
	last := cmds[len(cmds)-1]
	if !strings.Contains(last, "netns del ntnbox-phone-1") {
		t.Errorf("last command should delete namespace: %s", last)
	}
}
