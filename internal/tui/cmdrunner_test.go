package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCmdRunner_CapturesOutput(t *testing.T) {
	sender := &collectSender{}
	cr := NewCmdRunner(sender, "echo", []string{"hello world"})

	if err := cr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for exit message.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for CmdExitedMsg")
		default:
		}
		if sender.hasType(CmdExitedMsg{}) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	lines := sender.ofType(OutputLineMsg{})
	if len(lines) == 0 {
		t.Fatal("expected at least one OutputLineMsg")
	}
	got := lines[0].(OutputLineMsg).Line
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestCmdRunner_ExitCode(t *testing.T) {
	sender := &collectSender{}
	cr := NewCmdRunner(sender, "sh", []string{"-c", "exit 42"})

	if err := cr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for CmdExitedMsg")
		default:
		}
		if sender.hasType(CmdExitedMsg{}) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	exits := sender.ofType(CmdExitedMsg{})
	if len(exits) != 1 {
		t.Fatalf("expected 1 CmdExitedMsg, got %d", len(exits))
	}
	msg := exits[0].(CmdExitedMsg)
	if msg.Code != 42 {
		t.Errorf("exit code = %d, want 42", msg.Code)
	}
}

func TestCmdRunner_StripsANSI(t *testing.T) {
	sender := &collectSender{}
	// Echo a string with ANSI color codes.
	cr := NewCmdRunner(sender, "printf", []string{"\033[31mred\033[0m"})

	if err := cr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for CmdExitedMsg")
		default:
		}
		if sender.hasType(CmdExitedMsg{}) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	lines := sender.ofType(OutputLineMsg{})
	if len(lines) == 0 {
		t.Fatal("expected at least one OutputLineMsg")
	}
	got := lines[0].(OutputLineMsg).Line
	if got != "red" {
		t.Errorf("got %q, want %q", got, "red")
	}
}

// collectSender captures all sent messages for assertion.
type collectSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (cs *collectSender) Send(msg tea.Msg) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.msgs = append(cs.msgs, msg)
}

func (cs *collectSender) hasType(example tea.Msg) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, m := range cs.msgs {
		if sameType(m, example) {
			return true
		}
	}
	return false
}

func (cs *collectSender) ofType(example tea.Msg) []tea.Msg {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var result []tea.Msg
	for _, m := range cs.msgs {
		if sameType(m, example) {
			result = append(result, m)
		}
	}
	return result
}

func sameType(a, b tea.Msg) bool {
	switch a.(type) {
	case OutputLineMsg:
		_, ok := b.(OutputLineMsg)
		return ok
	case CmdExitedMsg:
		_, ok := b.(CmdExitedMsg)
		return ok
	case CoverageMsg:
		_, ok := b.(CoverageMsg)
		return ok
	case LinkStateMsg:
		_, ok := b.(LinkStateMsg)
		return ok
	}
	return false
}
