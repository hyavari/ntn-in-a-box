package tui

import (
	"strings"
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

func TestAdapter_FocusDeviceFiltersPeers(t *testing.T) {
	sender := &mockSender{}
	adapter := NewAdapter(sender, nil)
	adapter.SetFocusDevice("sandbox-0")

	adapter.OnCoverage(eventbus.CoverageEvent{
		Kind:     eventbus.KindWindowOpened,
		DeviceID: "sandbox-1",
		At:       time.Now(),
	})
	if sender.len() != 0 {
		t.Fatalf("peer coverage should be ignored, got %d", sender.len())
	}

	adapter.OnLinkState(eventbus.LinkStateEvent{
		DeviceID: "sandbox-1",
		State:    condition.LinkState{DelayMs: 1},
		At:       time.Now(),
	})
	if sender.len() != 0 {
		t.Fatalf("peer linkstate should be ignored, got %d", sender.len())
	}

	adapter.OnCoverage(eventbus.CoverageEvent{
		Kind:     eventbus.KindWindowOpened,
		DeviceID: "sandbox-0",
		At:       time.Now(),
	})
	if sender.len() != 1 {
		t.Fatalf("primary coverage should pass, got %d", sender.len())
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

func TestAdapter_OnMessageOmitsBody(t *testing.T) {
	sender := &mockSender{}
	adapter := NewAdapter(sender, nil)

	adapter.OnMessage(eventbus.MessageEvent{
		ID:     "msg-abc",
		From:   "sandbox-0",
		To:     "cloud",
		Status: "queued",
		Body:   "SECRET",
		At:     time.Now(),
	})

	if sender.len() != 1 {
		t.Fatalf("expected 1 message, got %d", sender.len())
	}
	msg, ok := sender.get(0).(MessageLifecycleMsg)
	if !ok {
		t.Fatalf("expected MessageLifecycleMsg, got %T", sender.get(0))
	}
	if msg.ID != "msg-abc" || msg.Status != "queued" {
		t.Errorf("unexpected msg: %+v", msg)
	}
}

func TestModel_UpsertMessage(t *testing.T) {
	m := NewModel(profile.Profile{Name: "t"}, 10)
	updated, _ := m.Update(MessageLifecycleMsg{
		ID: "msg-1", From: "sandbox-0", To: "cloud", Status: "queued",
	})
	m = updated.(Model)
	updated, _ = m.Update(MessageLifecycleMsg{
		ID: "msg-1", From: "sandbox-0", To: "cloud", Status: "delivered",
	})
	m = updated.(Model)
	if len(m.messages) != 1 {
		t.Fatalf("len=%d want 1", len(m.messages))
	}
	if m.messages[0].Status != "delivered" {
		t.Errorf("status=%s want delivered", m.messages[0].Status)
	}
	out := m.renderMessages(40)
	if !strings.Contains(out, "delivered") {
		t.Errorf("render missing status: %q", out)
	}
	if strings.Contains(out, "SECRET") {
		t.Error("body must not appear in render")
	}
}

func TestModel_MessageFollow(t *testing.T) {
	m := NewModel(profile.Profile{Name: "t"}, 10)
	// Fill past visible window.
	for i := 0; i < messageVisibleRows+3; i++ {
		updated, _ := m.Update(MessageLifecycleMsg{
			ID: "msg-" + itoa(i), From: "a", To: "b", Status: "queued",
		})
		m = updated.(Model)
	}
	if !m.messageFollow {
		t.Fatal("expected messageFollow after appends")
	}
	maxScroll := len(m.messages) - messageVisibleRows
	if m.messageScroll != maxScroll {
		t.Fatalf("scroll=%d want %d (follow bottom)", m.messageScroll, maxScroll)
	}
	out := m.renderMessages(80)
	if !strings.Contains(out, "msg-"+itoa(messageVisibleRows+2)) {
		t.Errorf("newest not visible: %q", out)
	}

	// Scroll up pauses follow.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(Model)
	if m.messageFollow {
		t.Fatal("K should disable messageFollow")
	}
	pausedScroll := m.messageScroll

	updated, _ = m.Update(MessageLifecycleMsg{
		ID: "msg-new", From: "a", To: "b", Status: "queued",
	})
	m = updated.(Model)
	if m.messageScroll != pausedScroll {
		t.Fatalf("paused follow should not jump on append: scroll=%d was %d", m.messageScroll, pausedScroll)
	}
}

func TestModel_CycleFocus(t *testing.T) {
	m := NewModel(profile.Profile{Name: "t"}, 10)
	m.deviceIDs = []string{"sf", "nyc"}
	m.focusDeviceID = "sf"
	var got string
	m.onFocusChange = func(id string) { got = id }

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(Model)
	if m.focusDeviceID != "nyc" || got != "nyc" {
		t.Fatalf("focus=%q callback=%q want nyc", m.focusDeviceID, got)
	}
	if m.hasLink {
		t.Error("sparklines/link should clear on focus change")
	}
	if m.inCoverage || m.remainingSec != 0 {
		t.Error("coverage should clear until focus refresh arrives")
	}
}

func TestAdapter_SetEvaluator(t *testing.T) {
	sender := &mockSender{}
	primary := &stubEval{inCov: true, until: 10}
	peer := &stubEval{inCov: false, until: 99}
	adapter := NewAdapter(sender, primary)
	adapter.SetFocusDevice("sf")

	adapter.OnCoverage(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowOpened, DeviceID: "sf", At: time.Now(),
	})
	msg := sender.get(0).(CoverageMsg)
	if !msg.InCoverage || msg.UntilNextTransition != 10 {
		t.Fatalf("primary enrich: %+v", msg)
	}

	adapter.SetFocusDevice("nyc")
	adapter.SetEvaluator(peer)
	adapter.OnCoverage(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowClosed, DeviceID: "nyc", At: time.Now(),
	})
	msg2 := sender.get(1).(CoverageMsg)
	if msg2.InCoverage || msg2.UntilNextTransition != 99 {
		t.Fatalf("peer enrich: %+v", msg2)
	}
}

type stubEval struct {
	inCov bool
	until float64
}

func (s *stubEval) Evaluate(time.Time) (condition.LinkState, condition.CoverageState) {
	return condition.LinkState{}, condition.CoverageState{
		InCoverage:             s.inCov,
		UntilNextTransitionSec: s.until,
	}
}
