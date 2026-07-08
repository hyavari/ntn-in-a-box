package tui

import (
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// mockSender captures messages sent by the adapter.
type mockSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (ms *mockSender) Send(msg tea.Msg) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.msgs = append(ms.msgs, msg)
}

func (ms *mockSender) get(i int) tea.Msg {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.msgs[i]
}

func (ms *mockSender) len() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return len(ms.msgs)
}

func testEvaluator(t *testing.T) *condition.Evaluator {
	t.Helper()
	p := profile.Profile{
		Name: "test",
		Schedule: profile.Schedule{
			Mode:         profile.ModePeriodic,
			PeriodSec:    60,
			WindowSec:    30,
			LookaheadSec: 5,
		},
		Curves: profile.Curves{
			DelayMs:       []profile.Point{{OffsetSec: 0, Value: 100}},
			JitterMs:      []profile.Point{{OffsetSec: 0, Value: 10}},
			LossPct:       []profile.Point{{OffsetSec: 0, Value: 0}},
			BandwidthKbps: []profile.Point{{OffsetSec: 0, Value: 500}},
		},
	}
	eval, err := condition.NewEvaluator(p, time.Now())
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	return eval
}

func TestAdapter_OnCoverage(t *testing.T) {
	sender := &mockSender{}
	eval := testEvaluator(t)
	adapter := NewAdapter(sender, eval)

	now := time.Now()
	adapter.OnCoverage(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowOpened,
		At:   now,
	})

	if sender.len() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.len())
	}

	msg, ok := sender.get(0).(CoverageMsg)
	if !ok {
		t.Fatalf("expected CoverageMsg, got %T", sender.get(0))
	}
	if msg.Kind != eventbus.KindWindowOpened {
		t.Errorf("Kind = %q, want %q", msg.Kind, eventbus.KindWindowOpened)
	}
	if !msg.InCoverage {
		t.Error("expected InCoverage=true for window_opened at epoch")
	}
}

func TestAdapter_OnLinkState(t *testing.T) {
	sender := &mockSender{}
	eval := testEvaluator(t)
	adapter := NewAdapter(sender, eval)

	now := time.Now()
	adapter.OnLinkState(eventbus.LinkStateEvent{
		State: condition.LinkState{
			DelayMs:       150,
			JitterMs:      12,
			LossPct:       0.5,
			BandwidthKbps: 256,
		},
		At: now,
	})

	if sender.len() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.len())
	}

	msg, ok := sender.get(0).(LinkStateMsg)
	if !ok {
		t.Fatalf("expected LinkStateMsg, got %T", sender.get(0))
	}
	if msg.State.DelayMs != 150 {
		t.Errorf("DelayMs = %f, want 150", msg.State.DelayMs)
	}
	if msg.State.BandwidthKbps != 256 {
		t.Errorf("BandwidthKbps = %f, want 256", msg.State.BandwidthKbps)
	}
}
