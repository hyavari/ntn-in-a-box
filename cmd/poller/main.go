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
	"strings"
	"syscall"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/cli"
)

func main() {
	url := flag.String("url", "http://localhost:8080/echo", "Target URL to poll")
	interval := flag.Duration("interval", 2*time.Second, "Poll interval")
	timeout := flag.Duration("timeout", 5*time.Second, "HTTP request timeout")
	flag.Parse()

	client := &http.Client{Timeout: *timeout}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "%s polling %s every %s (timeout %s)\n\n",
		cli.Styled(cli.Cyan+cli.Bold, "poller"),
		cli.Styled(cli.White, *url),
		*interval, *timeout)
	fmt.Println(cli.Header())

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	// First poll immediately.
	poll(client, *url)

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "\n%s stopped\n", cli.Styled(cli.Dim, "poller"))
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

	ts := start.Format("15:04:05")

	if err != nil {
		reason := shortenError(err.Error())
		fmt.Println(cli.RequestFail(ts, reason))
		return
	}
	resp.Body.Close() //nolint:errcheck

	latency := fmt.Sprintf("%dms", elapsed.Milliseconds())
	fmt.Println(cli.RequestOK(ts, latency, resp.StatusCode))
}

// shortenError trims verbose Go HTTP error messages to something readable.
func shortenError(msg string) string {
	// Remove URL prefix noise.
	if idx := strings.Index(msg, ": "); idx > 0 {
		short := msg[idx+2:]
		// If still long, try one more level.
		if idx2 := strings.Index(short, ": "); idx2 > 0 && len(short) > 40 {
			short = short[idx2+2:]
		}
		msg = short
	}
	// Truncate very long messages.
	if len(msg) > 50 {
		msg = msg[:47] + "..."
	}
	return msg
}
