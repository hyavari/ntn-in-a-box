// NTN-in-a-Box Sample: Message Client (Go)
//
// Demonstrates satellite-aware messaging patterns:
// - Sends messages to a server at regular intervals
// - Detects connectivity loss via timeouts
// - Queues unsent messages locally
// - Flushes queue when connectivity returns
// - Shows per-message latency
//
// Run under ntnbox:
//
//	ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- go run samples/go-messenger/client/main.go
//
// Make sure the server is running first:
//
//	go run samples/go-messenger/server/main.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	defaultURL   = "https://example.com" // reliable, no server needed
	sendInterval = 3 * time.Second
	httpTimeout  = 5 * time.Second
	maxQueue     = 100
)

type message struct {
	ID     int    `json:"id"`
	From   string `json:"from"`
	Text   string `json:"text"`
	SentAt string `json:"sent_at"`
}

// State tracking.
var (
	mu     sync.Mutex
	queue  []message
	msgID  int
	online = true
	stats  = struct{ sent, failed, queued, flushed int }{}
)

// Colors.
const (
	green  = "\033[32m"
	red    = "\033[31m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	dim    = "\033[2m"
	reset  = "\033[0m"
)

func log(color, symbol, msg string) {
	ts := time.Now().Format("15:04:05")
	fmt.Printf("  %s%s%s  %s%s%s  %s\n", dim, ts, reset, color, symbol, reset, msg)
}

// targetURL is set in main() and used by sendMsg.
var targetURL string

func sendMsg(msg message) bool {
	client := &http.Client{Timeout: httpTimeout}

	start := time.Now()
	var resp *http.Response
	var err error

	// POST if targeting a real server, GET for simple connectivity check.
	if targetURL == defaultURL {
		resp, err = client.Get(targetURL)
	} else {
		body, _ := json.Marshal(msg)
		resp, err = client.Post(targetURL, "application/json", bytes.NewReader(body))
	}
	latency := time.Since(start)

	if err != nil {
		if online {
			log(red, "▼", fmt.Sprintf("connection lost: %v", shortenErr(err)))
			online = false
		}
		stats.failed++
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if !online {
			log(green, "▲", "connection restored")
			online = true
		}
		stats.sent++
		log(green, "✓", fmt.Sprintf("msg#%d delivered (%s)", msg.ID, latency.Truncate(time.Millisecond)))
		return true
	}

	stats.failed++
	log(yellow, "✗", fmt.Sprintf("msg#%d HTTP %d", msg.ID, resp.StatusCode))
	return false
}

func enqueue(msg message) {
	mu.Lock()
	defer mu.Unlock()
	if len(queue) >= maxQueue {
		queue = queue[1:] // drop oldest
		log(yellow, "⚠", "queue full, dropped oldest")
	}
	queue = append(queue, msg)
	stats.queued++
	log(red, "◌", fmt.Sprintf("queued msg#%d (queue: %d)", msg.ID, len(queue)))
}

func flushQueue() {
	mu.Lock()
	pending := append([]message(nil), queue...)
	queue = queue[:0]
	mu.Unlock()

	if len(pending) == 0 {
		return
	}

	log(cyan, "⟳", fmt.Sprintf("flushing %d queued messages...", len(pending)))
	for i, msg := range pending {
		if sendMsg(msg) {
			stats.flushed++
		} else {
			// Re-queue only the unsent remainder.
			mu.Lock()
			queue = append(pending[i:], queue...)
			mu.Unlock()
			log(red, "✗", fmt.Sprintf("flush stalled, %d remaining", len(queue)))
			return
		}
	}
	log(green, "✓", "flush complete")
}

func shortenErr(err error) string {
	s := err.Error()
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

func main() {
	// Use SERVER_URL env or default to example.com (no server needed).
	targetURL = defaultURL
	if env := os.Getenv("SERVER_URL"); env != "" {
		targetURL = env
	}

	fmt.Printf("\n  ntn-messenger-client: sending to %s every %s\n", targetURL, sendInterval)
	fmt.Printf("  Demonstrates: offline queue, auto-flush, latency tracking\n\n")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(sendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("\n  stopped. stats: sent=%d failed=%d queued=%d flushed=%d pending=%d\n",
				stats.sent, stats.failed, stats.queued, stats.flushed, len(queue))
			return
		case <-ticker.C:
			msgID++
			msg := message{
				ID:     msgID,
				From:   "ntn-client",
				Text:   fmt.Sprintf("hello from NTN #%d", msgID),
				SentAt: time.Now().Format(time.RFC3339),
			}

			if sendMsg(msg) {
				if len(queue) > 0 {
					flushQueue()
				}
			} else {
				enqueue(msg)
			}

			if msgID%10 == 0 {
				log(dim, "│", fmt.Sprintf("stats: sent=%d failed=%d queued=%d flushed=%d pending=%d",
					stats.sent, stats.failed, stats.queued, stats.flushed, len(queue)))
			}
		}
	}
}
