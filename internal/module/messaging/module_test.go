package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/pkg/module"
)

type syncIMS struct{}

func (syncIMS) Submit(_ context.Context, _ module.OutboundMessage, onReceipt module.ReceiptFunc) (module.MessageID, error) {
	id := module.MessageID("sync-1")
	if onReceipt != nil {
		onReceipt(module.Receipt{MessageID: id, Status: module.StatusInFlight, At: time.Now()})
		onReceipt(module.Receipt{MessageID: id, Status: module.StatusDelivered, At: time.Now()})
	}
	return id, nil
}

func TestStore_AcceptAndInbox(t *testing.T) {
	s := NewStore()
	msg, err := s.Accept("sandbox-0", "cloud", "hi", "")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != StatusQueued {
		t.Fatalf("status = %s", msg.Status)
	}
	if len(s.Inbox("cloud")) != 0 {
		t.Fatal("inbox should be empty until delivered")
	}
	s.SetStatus(msg.ID, StatusDelivered, time.Now())
	inbox := s.Inbox("cloud")
	if len(inbox) != 1 || inbox[0].Body != "hi" {
		t.Fatalf("inbox = %+v", inbox)
	}
}

func TestStore_BodyTooLarge(t *testing.T) {
	s := NewStore()
	_, err := s.Accept("a", "cloud", strings.Repeat("x", MaxBodyBytes+1), "")
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestStore_ClaimQueuedIsAtomic(t *testing.T) {
	s := NewStore()
	msg, err := s.Accept("sandbox-0", "sandbox-1", "hi", "")
	if err != nil {
		t.Fatal(err)
	}
	a, ok1 := s.ClaimQueued(msg.ID, time.Now())
	b, ok2 := s.ClaimQueued(msg.ID, time.Now())
	if !ok1 || a == nil {
		t.Fatal("first claim should succeed")
	}
	if ok2 || b != nil {
		t.Fatal("second claim should fail")
	}
}

func TestStore_Full(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxMessages; i++ {
		msg, err := s.Accept("a", "cloud", "x", "")
		if err != nil {
			t.Fatalf("accept %d: %v", i, err)
		}
		// in_flight is never evicted — fill with non-evictable rows.
		if _, ok := s.ClaimQueued(msg.ID, time.Now()); !ok {
			t.Fatal("claim")
		}
	}
	if _, err := s.Accept("a", "cloud", "x", ""); !errors.Is(err, ErrStoreFull) {
		t.Fatalf("err = %v, want store full", err)
	}
}

func TestStore_EvictsQueuedUnderPressure(t *testing.T) {
	s := NewStore()
	var firstID string
	for i := 0; i < MaxMessages; i++ {
		msg, err := s.Accept("a", "sandbox-1", "x", "")
		if err != nil {
			t.Fatalf("accept %d: %v", i, err)
		}
		if i == 0 {
			firstID = msg.ID
		}
	}
	if _, err := s.Accept("a", "cloud", "new", ""); err != nil {
		t.Fatalf("accept after queued eviction: %v", err)
	}
	if _, ok := s.Get(firstID); ok {
		t.Fatal("oldest queued should have been evicted")
	}
}

func TestStore_EvictsTerminal(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxMessages; i++ {
		msg, err := s.Accept("a", "cloud", "x", "")
		if err != nil {
			t.Fatalf("accept %d: %v", i, err)
		}
		s.SetStatus(msg.ID, StatusDelivered, time.Now())
	}
	if _, err := s.Accept("a", "cloud", "new", ""); err != nil {
		t.Fatalf("accept after eviction: %v", err)
	}
}

func TestModule_EmptyDeviceIDFlushesQueued(t *testing.T) {
	devices := map[string]bool{"sandbox-0": true, "sandbox-1": true}
	cov := map[string]bool{"sandbox-0": true, "sandbox-1": false}
	mod := New(Config{
		DeviceExists: func(id string) bool { return devices[id] },
		InCoverage:   func(id string) bool { return cov[id] },
	})
	mod.DeliverVia(syncIMS{})
	mux := http.NewServeMux()
	mod.RegisterRoutes(mux)

	body := `{"to":"sandbox-1","body":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/devices/sandbox-0/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	mod.OnCoverageEvent(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowOpened,
		At:   time.Now(),
		// DeviceID intentionally empty (replay path).
	})
	msg, _ := mod.store.Get(resp.ID)
	if msg.Status != StatusDelivered {
		t.Fatalf("status = %s want delivered", msg.Status)
	}
}

func TestModule_CloudImmediate(t *testing.T) {
	devices := map[string]bool{"sandbox-0": true}
	mod := New(Config{
		DeviceExists: func(id string) bool { return devices[id] },
		InCoverage:   func(id string) bool { return id == CloudRecipient },
	})
	mod.DeliverVia(syncIMS{})

	mux := http.NewServeMux()
	mod.RegisterRoutes(mux)

	body := `{"to":"cloud","body":"SOS"}`
	req := httptest.NewRequest(http.MethodPost, "/devices/sandbox-0/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/messages/"+resp.ID, nil)
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	var msg Message
	if err := json.NewDecoder(rr2.Body).Decode(&msg); err != nil {
		t.Fatal(err)
	}
	if msg.Status != StatusDelivered {
		t.Fatalf("status = %s want delivered", msg.Status)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/devices/cloud/messages", nil)
	rr3 := httptest.NewRecorder()
	mux.ServeHTTP(rr3, req3)
	var inbox []Message
	if err := json.NewDecoder(rr3.Body).Decode(&inbox); err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 {
		t.Fatalf("inbox len = %d", len(inbox))
	}
}

func TestModule_UE2UEHoldUntilOpen(t *testing.T) {
	devices := map[string]bool{"sandbox-0": true, "sandbox-1": true}
	cov := map[string]bool{"sandbox-0": true, "sandbox-1": false}
	mod := New(Config{
		DeviceExists: func(id string) bool { return devices[id] },
		InCoverage:   func(id string) bool { return cov[id] },
	})
	mod.DeliverVia(syncIMS{})

	mux := http.NewServeMux()
	mod.RegisterRoutes(mux)

	body := `{"to":"sandbox-1","body":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/devices/sandbox-0/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	reqInbox := httptest.NewRequest(http.MethodGet, "/devices/sandbox-1/messages", nil)
	rrInbox := httptest.NewRecorder()
	mux.ServeHTTP(rrInbox, reqInbox)
	var inbox []Message
	_ = json.NewDecoder(rrInbox.Body).Decode(&inbox)
	if len(inbox) != 0 {
		t.Fatalf("expected empty inbox, got %d", len(inbox))
	}

	msg, _ := mod.store.Get(resp.ID)
	if msg.Status != StatusQueued {
		t.Fatalf("status = %s want queued", msg.Status)
	}

	mod.OnCoverageEvent(eventbus.CoverageEvent{
		Kind:     eventbus.KindWindowOpened,
		DeviceID: "sandbox-1",
		At:       time.Now(),
	})

	msg, _ = mod.store.Get(resp.ID)
	if msg.Status != StatusDelivered {
		t.Fatalf("status = %s want delivered", msg.Status)
	}
	inbox = nil
	for _, m := range mod.store.Inbox("sandbox-1") {
		inbox = append(inbox, *m)
	}
	if len(inbox) != 1 {
		t.Fatalf("inbox len = %d", len(inbox))
	}
}
