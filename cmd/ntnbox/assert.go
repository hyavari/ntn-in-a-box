package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func runAssert(args []string) error {
	fs := flag.NewFlagSet("assert", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profilePath := fs.String("profile", "testdata/profiles/sos_burst.yaml", "profile YAML for serve")
	addr := fs.String("addr", "127.0.0.1:18091", "serve listen address")
	timeoutSec := fs.Int("timeout", 30, "seconds to wait for delivered")
	device := fs.String("device", "sandbox-0", "sender device id")
	to := fs.String("to", "cloud", "message destination")
	body := fs.String("body", "assert-demo", "message body")
	if err := fs.Parse(args); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable: %w", err)
	}
	if _, err := profile.ResolveLoad(*profilePath); err != nil {
		return fmt.Errorf("profile %s: %w", *profilePath, err)
	}

	logPath := filepath.Join(os.TempDir(), "ntnbox-assert-serve.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("serve log: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(exe, "serve", "--profile", *profilePath, "--addr", *addr)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting serve: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	base := "http://" + httpBaseHost(*addr)
	if err := waitReady(base+"/devices/"+*device+"/condition", 5*time.Second); err != nil {
		return fmt.Errorf("%w (see %s)", err, logPath)
	}

	postBody, _ := json.Marshal(map[string]string{"to": *to, "body": *body})
	resp, err := http.Post(base+"/devices/"+*device+"/messages", "application/json", bytes.NewReader(postBody))
	if err != nil {
		return fmt.Errorf("POST message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var accepted struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		return fmt.Errorf("decode accept: %w", err)
	}
	if accepted.ID == "" {
		return errors.New("no message id in response")
	}

	deadline := time.Now().Add(time.Duration(*timeoutSec) * time.Second)
	var lastStatus string
	for time.Now().Before(deadline) {
		st, err := fetchMessageStatus(base, accepted.ID)
		if err == nil {
			lastStatus = st
			switch st {
			case "delivered":
				fmt.Printf("assert: OK  %s → delivered\n", accepted.ID)
				return nil
			case "failed":
				return fmt.Errorf("message failed: %s", accepted.ID)
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastStatus == "" {
		lastStatus = "unknown"
	}
	return fmt.Errorf("timeout after %ds (last status=%s) mid=%s", *timeoutSec, lastStatus, accepted.ID)
}

func waitReady(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("serve did not become ready")
}

func fetchMessageStatus(base, id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/messages/"+id, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Status, nil
}

// httpBaseHost maps a serve --addr to a host:port clients can dial.
// Mirrors normalizeListenAddr: empty host / :port → 127.0.0.1; 0.0.0.0 → 127.0.0.1.
func httpBaseHost(addr string) string {
	normalized := normalizeListenAddr(addr)
	host, port, err := splitHostPortLoose(normalized)
	if err != nil {
		return normalized
	}
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return host + ":" + port
}

func splitHostPortLoose(addr string) (host, port string, err error) {
	return net.SplitHostPort(addr)
}
