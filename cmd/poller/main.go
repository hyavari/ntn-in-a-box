// Command poller is a minimal HTTP polling client that prints
// per-request latency and status. It exists as a reference app for
// demonstrating NTN-in-a-Box's Dev Sandbox: run it under ntnbox run
// and watch latency/errors change as coverage windows open and close.
//
// Usage:
//
//	poller [--url <target>] [--interval <duration>] [--timeout <duration>]
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8080/echo", "Target URL to poll")
	interval := flag.Duration("interval", 2*time.Second, "Poll interval")
	timeout := flag.Duration("timeout", 5*time.Second, "HTTP request timeout")
	flag.Parse()

	client := &http.Client{Timeout: *timeout}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "poller: polling %s every %s (timeout %s)\n", *url, *interval, *timeout)
	fmt.Println("timestamp | status | latency | result")

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	// First poll immediately.
	poll(client, *url)

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\npoller: stopped")
			return
		case <-ticker.C:
			poll(client, *url)
		}
	}
}

func poll(client *http.Client, url string) {
	start := time.Now()
	resp, err := client.Get(url) //nolint:noctx
	elapsed := time.Since(start)

	ts := start.Format(time.RFC3339)

	if err != nil {
		fmt.Printf("%s | %3d | %8s | %s\n", ts, 0, "—", err)
		return
	}
	resp.Body.Close() //nolint:errcheck

	fmt.Printf("%s | %3d | %7dms | ok\n", ts, resp.StatusCode, elapsed.Milliseconds())
}
