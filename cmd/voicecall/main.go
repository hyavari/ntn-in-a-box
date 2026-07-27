// Command voicecall is a synthetic voice-call client for NTN-in-a-Box demos.
// It polls device condition, starts/completes/drops calls, and posts
// call-event telemetry so ntnbox run --report can tally session stats.
//
// Usage (API must be up via ntnbox run --addr; from inside the sandbox use the
// veth gateway, not 127.0.0.1):
//
//	ntnbox run --addr 127.0.0.1:8080 --report out.json --profile lband_geo -- ./voicecall
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/cli"
)

func defaultAPIBase() string {
	if v := os.Getenv("NTNBOX_API_BASE"); v != "" {
		return v
	}
	// Under ntnbox run, the API is on the host side of the veth (control-exempt).
	return "http://10.200.0.1:8080"
}

func main() {
	api := flag.String("api", defaultAPIBase(), "ntnbox API base URL (default: NTNBOX_API_BASE or http://10.200.0.1:8080)")
	device := flag.String("device", "sandbox-0", "Device ID")
	talk := flag.Duration("talk", 15*time.Second, "Talk time before completing a call")
	gap := flag.Duration("gap", 5*time.Second, "Idle gap between calls")
	poll := flag.Duration("poll", time.Second, "Condition poll interval")
	flag.Parse()

	client := &http.Client{Timeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "%s device %s via %s (talk %s, gap %s)\n\n",
		cli.Styled(cli.Cyan+cli.Bold, "voicecall"),
		cli.Styled(cli.White, *device),
		*api, *talk, *gap)
	fmt.Println("timestamp | event     | call_id")

	var (
		activeID  string
		startedAt time.Time
		lastEnd   time.Time
		callSeq   int
	)
	lastEnd = time.Now().Add(-*gap) // allow immediate first call

	ticker := time.NewTicker(*poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if activeID != "" {
				_ = postCall(client, *api, *device, activeID, "dropped")
				printRow("dropped", activeID)
			}
			fmt.Fprintf(os.Stderr, "\n%s stopped\n", cli.Styled(cli.Dim, "voicecall"))
			return
		case now := <-ticker.C:
			inCov, err := getInCoverage(client, *api, *device)
			if err != nil {
				fmt.Fprintf(os.Stderr, "voicecall: condition: %v\n", err)
				continue
			}
			if activeID != "" {
				if !inCov {
					_ = postCall(client, *api, *device, activeID, "dropped")
					printRow("dropped", activeID)
					activeID = ""
					lastEnd = now
					continue
				}
				if now.Sub(startedAt) >= *talk {
					_ = postCall(client, *api, *device, activeID, "completed")
					printRow("completed", activeID)
					activeID = ""
					lastEnd = now
				}
				continue
			}
			if inCov && now.Sub(lastEnd) >= *gap {
				callSeq++
				activeID = fmt.Sprintf("call-%d", callSeq)
				if err := postCall(client, *api, *device, activeID, "started"); err != nil {
					fmt.Fprintf(os.Stderr, "voicecall: start: %v\n", err)
					activeID = ""
					continue
				}
				startedAt = now
				printRow("started", activeID)
			}
		}
	}
}

func printRow(event, id string) {
	fmt.Printf("%s | %-9s | %s\n", time.Now().UTC().Format(time.RFC3339), event, id)
}

type conditionResp struct {
	InCoverage bool `json:"in_coverage"`
	InBlockage bool `json:"in_blockage"`
}

func getInCoverage(client *http.Client, api, device string) (bool, error) {
	resp, err := client.Get(api + "/devices/" + device + "/condition")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var c conditionResp
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return false, err
	}
	return c.InCoverage, nil
}

func postCall(client *http.Client, api, device, callID, status string) error {
	body, _ := json.Marshal(map[string]string{"id": callID, "status": status})
	resp, err := client.Post(api+"/devices/"+device+"/call-events", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, bytes.TrimSpace(b))
	}
	return nil
}
