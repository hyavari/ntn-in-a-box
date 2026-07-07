package imsadapter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/pkg/module"
)

func TestHappyPathDelivery(t *testing.T) {
	m := NewMockAdapter(MockConfig{})

	var receipts []module.Receipt
	var mu sync.Mutex
	done := make(chan struct{})

	onReceipt := func(r module.Receipt) {
		mu.Lock()
		receipts = append(receipts, r)
		if r.Status == module.StatusDelivered || r.Status == module.StatusFailed {
			close(done)
		}
		mu.Unlock()
	}

	id, err := m.Submit(context.Background(), module.OutboundMessage{
		To:   "ue-1",
		Body: []byte("hello"),
	}, onReceipt)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if id == "" {
		t.Fatal("Submit returned empty MessageID")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delivery")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(receipts) != 3 {
		t.Fatalf("got %d receipts, want 3", len(receipts))
	}

	// Verify state transitions: queued → in_flight → delivered.
	wantStatuses := []module.DeliveryStatus{
		module.StatusQueued,
		module.StatusInFlight,
		module.StatusDelivered,
	}
	for i, want := range wantStatuses {
		if receipts[i].Status != want {
			t.Errorf("receipt[%d].Status = %q, want %q", i, receipts[i].Status, want)
		}
		if receipts[i].MessageID != id {
			t.Errorf("receipt[%d].MessageID = %q, want %q", i, receipts[i].MessageID, id)
		}
		if receipts[i].At.IsZero() {
			t.Errorf("receipt[%d].At is zero", i)
		}
	}

	// Timestamps should be non-decreasing.
	for i := 1; i < len(receipts); i++ {
		if receipts[i].At.Before(receipts[i-1].At) {
			t.Errorf("receipt[%d].At (%v) is before receipt[%d].At (%v)",
				i, receipts[i].At, i-1, receipts[i-1].At)
		}
	}
}

func TestFailureInjection(t *testing.T) {
	m := NewMockAdapter(MockConfig{FailRate: 1.0}) // always fail

	var receipts []module.Receipt
	var mu sync.Mutex
	done := make(chan struct{})

	onReceipt := func(r module.Receipt) {
		mu.Lock()
		receipts = append(receipts, r)
		if r.Status == module.StatusDelivered || r.Status == module.StatusFailed {
			close(done)
		}
		mu.Unlock()
	}

	_, err := m.Submit(context.Background(), module.OutboundMessage{
		To:   "ue-1",
		Body: []byte("will fail"),
	}, onReceipt)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failure")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(receipts) != 3 {
		t.Fatalf("got %d receipts, want 3", len(receipts))
	}
	if receipts[2].Status != module.StatusFailed {
		t.Errorf("final status = %q, want %q", receipts[2].Status, module.StatusFailed)
	}
}

func TestContextCancellationStopsReceipts(t *testing.T) {
	// Use a delay long enough that cancellation will fire first.
	m := NewMockAdapter(MockConfig{
		QueueDelay: 500 * time.Millisecond,
	})

	var receipts []module.Receipt
	var mu sync.Mutex

	ctx, cancel := context.WithCancel(context.Background())

	onReceipt := func(r module.Receipt) {
		mu.Lock()
		receipts = append(receipts, r)
		mu.Unlock()
	}

	_, err := m.Submit(ctx, module.OutboundMessage{
		To:   "ue-1",
		Body: []byte("cancel me"),
	}, onReceipt)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// The "queued" receipt is synchronous — we already have it.
	// Cancel before the queue delay elapses.
	cancel()

	// Give the goroutine time to notice cancellation.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should only have the initial "queued" receipt.
	if len(receipts) != 1 {
		t.Errorf("got %d receipts after cancel, want 1 (only queued)", len(receipts))
	}
	if receipts[0].Status != module.StatusQueued {
		t.Errorf("receipt[0].Status = %q, want %q", receipts[0].Status, module.StatusQueued)
	}
}

func TestNilReceiptFunc(t *testing.T) {
	m := NewMockAdapter(MockConfig{})

	// Should not panic with nil onReceipt.
	id, err := m.Submit(context.Background(), module.OutboundMessage{
		To:   "ue-1",
		Body: []byte("no callback"),
	}, nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if id == "" {
		t.Fatal("Submit returned empty MessageID")
	}
}

func TestUniqueMessageIDs(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	ids := make(map[module.MessageID]bool)

	for range 100 {
		id, err := m.Submit(context.Background(), module.OutboundMessage{
			To:   "ue-1",
			Body: []byte("x"),
		}, nil)
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if ids[id] {
			t.Fatalf("duplicate MessageID: %q", id)
		}
		ids[id] = true
	}
}

func TestConcurrentSubmit(t *testing.T) {
	m := NewMockAdapter(MockConfig{})
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n)

	for range n {
		go func() {
			defer wg.Done()
			done := make(chan struct{})
			_, err := m.Submit(context.Background(), module.OutboundMessage{
				To:   "ue-1",
				Body: []byte("concurrent"),
			}, func(r module.Receipt) {
				if r.Status == module.StatusDelivered || r.Status == module.StatusFailed {
					close(done)
				}
			})
			if err != nil {
				t.Errorf("Submit: %v", err)
				return
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Errorf("timed out waiting for delivery")
			}
		}()
	}

	wg.Wait()
}

func TestDelayTiming(t *testing.T) {
	// Use a controlled clock to verify receipt timestamps reflect delays.
	var mu sync.Mutex
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	m := NewMockAdapter(MockConfig{
		QueueDelay:    10 * time.Millisecond,
		InFlightDelay: 20 * time.Millisecond,
	})
	// Override clock to advance on each call.
	callCount := 0
	m.clock = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		// queued at t=0, in-flight at t=10ms, delivered at t=30ms
		switch callCount {
		case 1:
			return now
		case 2:
			return now.Add(10 * time.Millisecond)
		case 3:
			return now.Add(30 * time.Millisecond)
		default:
			return now.Add(time.Duration(callCount) * time.Second)
		}
	}

	var receipts []module.Receipt
	var receiptMu sync.Mutex
	done := make(chan struct{})

	onReceipt := func(r module.Receipt) {
		receiptMu.Lock()
		receipts = append(receipts, r)
		if r.Status == module.StatusDelivered || r.Status == module.StatusFailed {
			close(done)
		}
		receiptMu.Unlock()
	}

	_, err := m.Submit(context.Background(), module.OutboundMessage{
		To:   "ue-1",
		Body: []byte("timed"),
	}, onReceipt)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	receiptMu.Lock()
	defer receiptMu.Unlock()

	if len(receipts) != 3 {
		t.Fatalf("got %d receipts, want 3", len(receipts))
	}

	// Verify timestamps match the injected clock.
	if !receipts[0].At.Equal(now) {
		t.Errorf("queued.At = %v, want %v", receipts[0].At, now)
	}
	if !receipts[1].At.Equal(now.Add(10 * time.Millisecond)) {
		t.Errorf("in_flight.At = %v, want %v", receipts[1].At, now.Add(10*time.Millisecond))
	}
	if !receipts[2].At.Equal(now.Add(30 * time.Millisecond)) {
		t.Errorf("delivered.At = %v, want %v", receipts[2].At, now.Add(30*time.Millisecond))
	}
}
