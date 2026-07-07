package imsadapter

import (
	"context"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyavari/ntn-in-a-box/pkg/module"
)

// MockConfig configures the mock IMS adapter's failure injection and
// timing behavior.
type MockConfig struct {
	// FailRate is the probability [0.0, 1.0] that a message transitions
	// to "failed" instead of "delivered" after the in-flight phase.
	// 0 means always deliver; 1 means always fail.
	FailRate float64

	// InFlightDelay is how long a message stays in the "in_flight"
	// state before transitioning to delivered/failed. Zero means
	// transition immediately (useful for tests).
	InFlightDelay time.Duration

	// QueueDelay is how long a message stays in "queued" before
	// transitioning to "in_flight". Zero means transition immediately.
	QueueDelay time.Duration
}

// MockAdapter is the Step 0 mock IMS backend. It simulates the message
// delivery lifecycle (queued → in-flight → delivered/failed) with
// configurable timing and failure injection, without any real network
// or protocol interaction.
//
// It satisfies pkg/module.IMSAdapter structurally.
//
// Safe for concurrent use.
type MockAdapter struct {
	config  MockConfig
	nextID  atomic.Uint64
	rand    *rand.Rand
	randMu  sync.Mutex
	clock   func() time.Time                     // injectable for testing; defaults to time.Now
	afterFn func(time.Duration) <-chan time.Time // injectable; defaults to time.After
}

// Compile-time check that MockAdapter satisfies module.IMSAdapter.
var _ module.IMSAdapter = (*MockAdapter)(nil)

// NewMockAdapter returns a MockAdapter with the given config.
func NewMockAdapter(cfg MockConfig) *MockAdapter {
	return &MockAdapter{
		config:  cfg,
		rand:    rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
		clock:   time.Now,
		afterFn: time.After,
	}
}

// Submit submits a message for simulated delivery. The receipt callback
// is invoked asynchronously as the message transitions through states.
// Cancelling ctx stops further receipt callbacks.
func (m *MockAdapter) Submit(ctx context.Context, _ module.OutboundMessage, onReceipt module.ReceiptFunc) (module.MessageID, error) {
	id := m.generateID()

	// Immediately report "queued" synchronously — the message is
	// accepted into the mock backend the instant Submit is called.
	if onReceipt != nil {
		onReceipt(module.Receipt{
			MessageID: id,
			Status:    module.StatusQueued,
			At:        m.clock(),
		})
	}

	// Drive the rest of the lifecycle asynchronously.
	if onReceipt != nil {
		go m.driveLifecycle(ctx, id, onReceipt)
	}

	return id, nil
}

func (m *MockAdapter) driveLifecycle(ctx context.Context, id module.MessageID, onReceipt module.ReceiptFunc) {
	// queued → in-flight
	if !m.waitOrCancel(ctx, m.config.QueueDelay) {
		return
	}
	onReceipt(module.Receipt{
		MessageID: id,
		Status:    module.StatusInFlight,
		At:        m.clock(),
	})

	// in-flight → delivered/failed
	if !m.waitOrCancel(ctx, m.config.InFlightDelay) {
		return
	}

	status := module.StatusDelivered
	if m.shouldFail() {
		status = module.StatusFailed
	}
	onReceipt(module.Receipt{
		MessageID: id,
		Status:    status,
		At:        m.clock(),
	})
}

// waitOrCancel waits for d or ctx cancellation. Returns true if the
// wait completed, false if ctx was cancelled. A zero duration does not
// wait at all.
func (m *MockAdapter) waitOrCancel(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	select {
	case <-m.afterFn(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *MockAdapter) shouldFail() bool {
	if m.config.FailRate <= 0 {
		return false
	}
	if m.config.FailRate >= 1 {
		return true
	}
	m.randMu.Lock()
	r := m.rand.Float64()
	m.randMu.Unlock()
	return r < m.config.FailRate
}

func (m *MockAdapter) generateID() module.MessageID {
	n := m.nextID.Add(1)
	// Simple monotonic IDs for the mock — no need for UUIDs.
	return module.MessageID("mock-" + uitoa(n))
}

// uitoa is a minimal uint64-to-string without importing strconv.
func uitoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
