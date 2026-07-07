package apihost

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	p := &profile.Profile{
		Name:        "leo_pass_90s",
		Description: "test LEO pass",
		Schedule: profile.Schedule{
			Mode:         profile.ModePeriodic,
			PeriodSec:    600,
			WindowSec:    90,
			LookaheadSec: 30,
		},
		Curves: profile.Curves{
			DelayMs:       []profile.Point{{OffsetSec: 0, Value: 150}, {OffsetSec: 90, Value: 100}},
			JitterMs:      []profile.Point{{OffsetSec: 0, Value: 40}, {OffsetSec: 90, Value: 40}},
			LossPct:       []profile.Point{{OffsetSec: 0, Value: 10}, {OffsetSec: 90, Value: 10}},
			BandwidthKbps: []profile.Point{{OffsetSec: 0, Value: 2000}, {OffsetSec: 90, Value: 2000}},
		},
	}

	srv := New(Config{
		Profiles: []*profile.Profile{p},
		Registry: device.NewRegistry(),
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestHealthEndpoint(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v, want status=ok", body)
	}
}

func TestEchoEndpoint(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/echo")
	if err != nil {
		t.Fatalf("GET /echo: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ts"] == "" {
		t.Error("ts field is empty")
	}
}

func TestListProfiles(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/profiles")
	if err != nil {
		t.Fatalf("GET /profiles: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var profiles []map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&profiles); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("got %d profiles, want 1", len(profiles))
	}
	if profiles[0]["name"] != "leo_pass_90s" {
		t.Errorf("name = %q, want %q", profiles[0]["name"], "leo_pass_90s")
	}
}

func TestGetProfile(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/profiles/leo_pass_90s")
	if err != nil {
		t.Fatalf("GET /profiles/leo_pass_90s: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var p profile.Profile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Name != "leo_pass_90s" {
		t.Errorf("name = %q, want %q", p.Name, "leo_pass_90s")
	}
}

func TestGetProfileNotFound(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/profiles/nonexistent")
	if err != nil {
		t.Fatalf("GET /profiles/nonexistent: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRegisterAndGetDevice(t *testing.T) {
	_, ts := testServer(t)

	// Register a device.
	body := `{"id":"ue-1","type":"virtual_ue","profile_name":"leo_pass_90s"}`
	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated {
		var errBody map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		t.Fatalf("status = %d, want 201; body = %v", resp.StatusCode, errBody)
	}

	var d deviceResponse
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.ID != "ue-1" || d.Type != "virtual_ue" || d.ProfileName != "leo_pass_90s" {
		t.Errorf("device = %+v, want id=ue-1 type=virtual_ue profile=leo_pass_90s", d)
	}
	if d.CreatedAt == "" {
		t.Error("created_at is empty")
	}

	// Get the device back.
	resp2, err := http.Get(ts.URL + "/devices/ue-1")
	if err != nil {
		t.Fatalf("GET /devices/ue-1: %v", err)
	}
	defer resp2.Body.Close() //nolint:errcheck

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}

	var d2 deviceResponse
	if err := json.NewDecoder(resp2.Body).Decode(&d2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d2.ID != "ue-1" {
		t.Errorf("GET device ID = %q, want %q", d2.ID, "ue-1")
	}
}

func TestListDevices(t *testing.T) {
	_, ts := testServer(t)

	// Register two devices.
	for _, id := range []string{"ue-1", "ue-2"} {
		body := `{"id":"` + id + `","type":"virtual_ue","profile_name":"leo_pass_90s"}`
		resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("POST /devices: %v", err)
		}
		resp.Body.Close() //nolint:errcheck
	}

	resp, err := http.Get(ts.URL + "/devices")
	if err != nil {
		t.Fatalf("GET /devices: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var devices []deviceResponse
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(devices) != 2 {
		t.Errorf("got %d devices, want 2", len(devices))
	}
}

func TestRegisterDeviceDuplicate(t *testing.T) {
	_, ts := testServer(t)

	body := `{"id":"ue-1","type":"virtual_ue","profile_name":"leo_pass_90s"}`
	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	resp.Body.Close() //nolint:errcheck

	// Second registration with same ID should fail.
	resp2, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST /devices (dup): %v", err)
	}
	defer resp2.Body.Close() //nolint:errcheck

	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp2.StatusCode)
	}
}

func TestRegisterDeviceUnknownProfile(t *testing.T) {
	_, ts := testServer(t)

	body := `{"id":"ue-1","type":"virtual_ue","profile_name":"nonexistent"}`
	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRegisterDeviceBadJSON(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString("not json"))
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetDeviceNotFound(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/devices/nonexistent")
	if err != nil {
		t.Fatalf("GET /devices/nonexistent: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetCondition(t *testing.T) {
	_, ts := testServer(t)

	// Register a device first.
	body := `{"id":"ue-1","type":"virtual_ue","profile_name":"leo_pass_90s"}`
	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	resp.Body.Close() //nolint:errcheck

	// Query condition state.
	resp2, err := http.Get(ts.URL + "/devices/ue-1/condition")
	if err != nil {
		t.Fatalf("GET /devices/ue-1/condition: %v", err)
	}
	defer resp2.Body.Close() //nolint:errcheck

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}

	var cond conditionResponse
	if err := json.NewDecoder(resp2.Body).Decode(&cond); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The device was just registered, so it should be in coverage
	// (periodic mode, we're within the first 90s window).
	if !cond.InCoverage {
		t.Error("expected in_coverage=true right after registration")
	}
	if cond.DelayMs == 0 && cond.BandwidthKbps == 0 {
		t.Error("expected non-zero link state values while in coverage")
	}
}

func TestGetConditionDeviceNotFound(t *testing.T) {
	_, ts := testServer(t)

	resp, err := http.Get(ts.URL + "/devices/nonexistent/condition")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
