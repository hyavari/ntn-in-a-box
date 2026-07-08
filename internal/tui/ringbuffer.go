// Package tui implements the terminal dashboard for ntnbox run --tui.
package tui

import "sync"

// RingBuffer is a fixed-capacity circular buffer of strings (output
// lines). Safe for concurrent use.
type RingBuffer struct {
	mu    sync.Mutex
	buf   []string
	cap   int
	head  int // next write position
	count int // number of valid entries
}

// NewRingBuffer creates a ring buffer that holds at most capacity lines.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		buf: make([]string, capacity),
		cap: capacity,
	}
}

// Write appends a line to the buffer. If the buffer is full, the
// oldest line is evicted.
func (rb *RingBuffer) Write(line string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.buf[rb.head] = line
	rb.head = (rb.head + 1) % rb.cap
	if rb.count < rb.cap {
		rb.count++
	}
}

// Len returns the number of lines currently stored.
func (rb *RingBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.count
}

// Lines returns up to count lines starting at logical index start
// (0 = oldest stored line). If start+count exceeds Len(), the
// returned slice is shorter.
func (rb *RingBuffer) Lines(start, count int) []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if start >= rb.count || count <= 0 {
		return nil
	}
	end := start + count
	if end > rb.count {
		end = rb.count
	}

	result := make([]string, 0, end-start)
	// The oldest line's physical index:
	oldest := (rb.head - rb.count + rb.cap) % rb.cap
	for i := start; i < end; i++ {
		idx := (oldest + i) % rb.cap
		result = append(result, rb.buf[idx])
	}
	return result
}

// All returns all stored lines in order (oldest first).
func (rb *RingBuffer) All() []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return nil
	}
	result := make([]string, 0, rb.count)
	oldest := (rb.head - rb.count + rb.cap) % rb.cap
	for i := range rb.count {
		idx := (oldest + i) % rb.cap
		result = append(result, rb.buf[idx])
	}
	return result
}
