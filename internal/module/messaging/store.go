package messaging

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

const MaxBodyBytes = 1024
const MaxContentTypeBytes = 128
const MaxMessages = 1000
const MaxRequestBytes = 8192 // HTTP body cap before JSON decode

// Status is the store-facing message lifecycle.
type Status string

const (
	StatusAccepted  Status = "accepted"
	StatusQueued    Status = "queued"
	StatusInFlight  Status = "in_flight"
	StatusDelivered Status = "delivered"
	StatusFailed    Status = "failed"
)

// Message is a stored store-and-forward message.
type Message struct {
	ID          string    `json:"id"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Body        string    `json:"body"`
	ContentType string    `json:"content_type"`
	Status      Status    `json:"status"`
	AcceptedAt  time.Time `json:"accepted_at"`
	QueuedAt    time.Time `json:"queued_at,omitempty"`
	DeliveredAt time.Time `json:"delivered_at,omitempty"`
	FailedAt    time.Time `json:"failed_at,omitempty"`
}

// Store is an in-memory message store. Safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	byID map[string]*Message
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{byID: make(map[string]*Message)}
}

var (
	ErrBodyTooLarge        = errors.New("messaging: body too large")
	ErrEmptyBody           = errors.New("messaging: empty body")
	ErrStoreFull           = errors.New("messaging: store full")
	ErrContentTypeTooLarge = errors.New("messaging: content_type too large")
)

// Accept creates a message in queued state (accepted_at == queued_at).
func (s *Store) Accept(from, to, body, contentType string) (*Message, error) {
	if body == "" {
		return nil, ErrEmptyBody
	}
	if len(body) > MaxBodyBytes {
		return nil, ErrBodyTooLarge
	}
	if contentType == "" {
		contentType = "text/plain"
	}
	if len(contentType) > MaxContentTypeBytes {
		return nil, ErrContentTypeTooLarge
	}
	now := time.Now().UTC()
	id, err := newMessageID()
	if err != nil {
		return nil, err
	}
	m := &Message{
		ID:          id,
		From:        from,
		To:          to,
		Body:        body,
		ContentType: contentType,
		Status:      StatusQueued,
		AcceptedAt:  now,
		QueuedAt:    now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.byID) >= MaxMessages {
		if !evictOneLocked(s.byID) {
			return nil, ErrStoreFull
		}
	}
	s.byID[id] = m
	return cloneMessage(m), nil
}

// Get returns a copy of the message, or false if missing.
func (s *Store) Get(id string) (*Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	return cloneMessage(m), true
}

// Inbox returns delivered messages for recipient, oldest-first.
func (s *Store) Inbox(to string) []*Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Message
	for _, m := range s.byID {
		if m.To == to && m.Status == StatusDelivered {
			out = append(out, cloneMessage(m))
		}
	}
	sortMessages(out)
	return out
}

// PendingFor returns queued messages for recipient, oldest-first.
func (s *Store) PendingFor(to string) []*Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Message
	for _, m := range s.byID {
		if m.To == to && m.Status == StatusQueued {
			out = append(out, cloneMessage(m))
		}
	}
	sortMessages(out)
	return out
}

// ClaimQueued atomically moves a queued message to in_flight.
// Returns a copy of the message and true if the claim succeeded.
func (s *Store) ClaimQueued(id string, at time.Time) (*Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok || m.Status != StatusQueued {
		return nil, false
	}
	m.Status = StatusInFlight
	return cloneMessage(m), true
}

// SetStatus updates status and related timestamps. Returns false if missing.
func (s *Store) SetStatus(id string, st Status, at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.byID[id]
	if !ok {
		return false
	}
	m.Status = st
	switch st {
	case StatusQueued:
		m.QueuedAt = at
	case StatusDelivered:
		m.DeliveredAt = at
	case StatusFailed:
		m.FailedAt = at
	}
	return true
}

// QueuedRecipients returns distinct To values that still have queued messages.
func (s *Store) QueuedRecipients() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]struct{})
	var out []string
	for _, m := range s.byID {
		if m.Status != StatusQueued {
			continue
		}
		if _, ok := seen[m.To]; ok {
			continue
		}
		seen[m.To] = struct{}{}
		out = append(out, m.To)
	}
	sort.Strings(out)
	return out
}

func newMessageID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("messaging: id: %w", err)
	}
	return "msg-" + hex.EncodeToString(b[:]), nil
}

// evictOneLocked removes one message to make room: prefer oldest terminal,
// else oldest queued. Never evicts in_flight. Caller must hold s.mu.
func evictOneLocked(byID map[string]*Message) bool {
	if evictOldestWithStatusesLocked(byID, StatusDelivered, StatusFailed) {
		return true
	}
	return evictOldestWithStatusesLocked(byID, StatusQueued)
}

func evictOldestWithStatusesLocked(byID map[string]*Message, statuses ...Status) bool {
	want := make(map[Status]struct{}, len(statuses))
	for _, st := range statuses {
		want[st] = struct{}{}
	}
	var bestID string
	var bestAt time.Time
	found := false
	for id, m := range byID {
		if _, ok := want[m.Status]; !ok {
			continue
		}
		at := m.AcceptedAt
		switch {
		case m.Status == StatusDelivered && !m.DeliveredAt.IsZero():
			at = m.DeliveredAt
		case m.Status == StatusFailed && !m.FailedAt.IsZero():
			at = m.FailedAt
		}
		if !found || at.Before(bestAt) || (at.Equal(bestAt) && id < bestID) {
			bestID = id
			bestAt = at
			found = true
		}
	}
	if !found {
		return false
	}
	delete(byID, bestID)
	return true
}

func sortMessages(out []*Message) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].AcceptedAt.Equal(out[j].AcceptedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].AcceptedAt.Before(out[j].AcceptedAt)
	})
}

func cloneMessage(m *Message) *Message {
	cp := *m
	return &cp
}
