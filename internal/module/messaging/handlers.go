// Package messaging implements store-and-forward messaging between devices and cloud.
package messaging

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type sendRequest struct {
	To          string `json:"to"`
	Body        string `json:"body"`
	ContentType string `json:"content_type"`
}

func (m *Module) handleSend(w http.ResponseWriter, r *http.Request) {
	from := r.PathValue("id")
	if from == "" || from == CloudRecipient {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sender"})
		return
	}
	if m.deviceExists == nil || !m.deviceExists(from) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sender"})
		return
	}

	var req sendRequest
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if req.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to is required"})
		return
	}
	if !m.validRecipient(req.To) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown recipient"})
		return
	}

	msg, err := m.store.Accept(from, req.To, req.Body, req.ContentType)
	if err != nil {
		switch {
		case errors.Is(err, ErrBodyTooLarge), errors.Is(err, ErrContentTypeTooLarge):
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrStoreFull):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"id":     msg.ID,
		"status": "accepted",
	})

	m.publishStatus(msg.ID)

	if m.recipientReady(req.To) {
		m.release(msg.ID)
	}
}

func (m *Module) handleInbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id != CloudRecipient {
		if m.deviceExists == nil || !m.deviceExists(id) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown device"})
			return
		}
	}
	msgs := m.store.Inbox(id)
	type item struct {
		ID          string    `json:"id"`
		From        string    `json:"from"`
		To          string    `json:"to"`
		Body        string    `json:"body"`
		ContentType string    `json:"content_type"`
		Status      Status    `json:"status"`
		AcceptedAt  time.Time `json:"accepted_at"`
		DeliveredAt time.Time `json:"delivered_at,omitempty"`
	}
	out := make([]item, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, item{
			ID:          msg.ID,
			From:        msg.From,
			To:          msg.To,
			Body:        msg.Body,
			ContentType: msg.ContentType,
			Status:      msg.Status,
			AcceptedAt:  msg.AcceptedAt,
			DeliveredAt: msg.DeliveredAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (m *Module) handleGet(w http.ResponseWriter, r *http.Request) {
	mid := r.PathValue("mid")
	msg, ok := m.store.Get(mid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "message not found"})
		return
	}
	writeJSON(w, http.StatusOK, msg)
}
