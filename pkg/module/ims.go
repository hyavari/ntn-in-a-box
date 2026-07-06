package module

import (
	"context"
	"time"
)

// IMSAdapter is the minimal capability a module needs to deliver a
// message through the kernel's pluggable IMS backend.
//
// pkg/module deliberately does not import internal/kernel/imsadapter
// (which doesn't have a real implementation yet — see Task 8) to
// avoid coupling the module contract to a package that hasn't been
// designed. Whatever that package's mock/real adapter types turn out
// to be, they satisfy this interface as long as they can Submit a
// message and report receipts.
type IMSAdapter interface {
	// Submit submits a message for delivery. onReceipt, if non-nil, is
	// called (possibly asynchronously, possibly more than once) as the
	// message's delivery status changes — matching the mock IMS
	// backend's queued -> in-flight -> delivered/failed lifecycle
	// (design doc, key decision 6).
	//
	// Cancellation: onReceipt stops being called once ctx is done.
	// There is no separate unsubscribe mechanism — callers that no
	// longer care about a message's receipts should cancel the ctx
	// they passed to Submit rather than expecting Submit itself to
	// return a cancel handle.
	Submit(ctx context.Context, msg OutboundMessage, onReceipt ReceiptFunc) (MessageID, error)
}

// MessageID identifies a submitted message for receipt tracking.
type MessageID string

// OutboundMessage is a message a module wants delivered to a device.
type OutboundMessage struct {
	To   string
	Body []byte
}

// ReceiptFunc is called as a submitted message's delivery status changes.
type ReceiptFunc func(Receipt)

// Receipt reports a submitted message's current delivery status.
type Receipt struct {
	MessageID MessageID
	Status    DeliveryStatus
	At        time.Time
}

// DeliveryStatus is a message's position in the IMS backend's
// (mock or real) delivery lifecycle.
type DeliveryStatus string

// Delivery statuses, matching the mock IMS backend's lifecycle (design
// doc, key decision 6).
const (
	StatusQueued    DeliveryStatus = "queued"
	StatusInFlight  DeliveryStatus = "in_flight"
	StatusDelivered DeliveryStatus = "delivered"
	StatusFailed    DeliveryStatus = "failed"
)
