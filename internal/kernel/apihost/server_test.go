package apihost

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

var testStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func newTestBus() *eventbus.Bus {
	return eventbus.New(eventbus.LinkStateThrottle{Interval: 0, DeltaThreshold: 0})
}

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

func TestGetLookahead(t *testing.T) {
	_, ts := testServer(t)

	body := `{"id":"ue-1","type":"virtual_ue","profile_name":"leo_pass_90s"}`
	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	resp.Body.Close() //nolint:errcheck

	resp2, err := http.Get(ts.URL + "/devices/ue-1/lookahead?lead_sec=60")
	if err != nil {
		t.Fatalf("GET /lookahead: %v", err)
	}
	defer resp2.Body.Close() //nolint:errcheck

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}

	var la lookaheadResponse
	if err := json.NewDecoder(resp2.Body).Decode(&la); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !la.InCoverage {
		t.Error("expected in_coverage=true")
	}
	if la.NextOpenAt == nil || la.NextCloseAt == nil {
		t.Error("expected next_open_at and next_close_at")
	}
	if la.NextWindowDurationSec == nil || *la.NextWindowDurationSec <= 0 {
		t.Error("expected next_window_duration_sec")
	}
	if la.EffectiveLookaheadSec != 60 {
		t.Errorf("effective_lookahead_sec = %v, want 60", la.EffectiveLookaheadSec)
	}
}

func TestGetLookaheadDeviceNotFound(t *testing.T) {
	_, ts := testServer(t)
	resp, err := http.Get(ts.URL + "/devices/nonexistent/lookahead")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

type evalOnlyStub struct{}

func (evalOnlyStub) Evaluate(now time.Time) (condition.LinkState, condition.CoverageState) {
	return condition.LinkState{}, condition.CoverageState{InCoverage: true, UntilNextTransitionSec: 10}
}

func TestGetLookaheadNotImplemented(t *testing.T) {
	srv, ts := testServer(t)

	body := `{"id":"ue-1","type":"virtual_ue","profile_name":"leo_pass_90s"}`
	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST /devices: %v", err)
	}
	resp.Body.Close() //nolint:errcheck

	srv.RegisterEvaluator("ue-1", evalOnlyStub{})

	resp2, err := http.Get(ts.URL + "/devices/ue-1/lookahead")
	if err != nil {
		t.Fatalf("GET /lookahead: %v", err)
	}
	defer resp2.Body.Close() //nolint:errcheck
	if resp2.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp2.StatusCode)
	}
}

func TestEnrichLookaheadCoverage_Opening(t *testing.T) {
	p := &profile.Profile{
		Name: "t",
		Schedule: profile.Schedule{
			Mode: profile.ModePeriodic, PeriodSec: 100, WindowSec: 20, LookaheadSec: 10,
		},
		Curves: profile.Curves{
			DelayMs:       []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: 20, Value: 1}},
			JitterMs:      []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: 20, Value: 1}},
			LossPct:       []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: 20, Value: 1}},
			BandwidthKbps: []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: 20, Value: 1}},
		},
	}
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ev, err := condition.NewEvaluator(*p, epoch)
	if err != nil {
		t.Fatal(err)
	}

	payload := sseCoverageEvent{Kind: string(eventbus.KindWindowOpening)}
	enrichLookaheadCoverage(&payload, ev, epoch.Add(50*time.Second)) // out of coverage
	if payload.LookaheadSec == nil || *payload.LookaheadSec != 10 {
		t.Fatalf("LookaheadSec = %v, want 10", payload.LookaheadSec)
	}
	if payload.NextOpenAt == nil || payload.NextCloseAt == nil {
		t.Fatal("expected next_open_at and next_close_at")
	}
	if payload.NextWindowDurationSec == nil || *payload.NextWindowDurationSec != 20 {
		t.Fatalf("duration = %v, want 20", payload.NextWindowDurationSec)
	}

	opened := sseCoverageEvent{Kind: string(eventbus.KindWindowOpened)}
	enrichLookaheadCoverage(&opened, ev, epoch)
	// enrichment only called for opening/closing in handleSSE; helper itself still fills —
	// assert helper works; opened path simply isn't invoked from handleSSE.
}

func TestSSE_LookaheadEnrichmentOnOpening(t *testing.T) {
	p := &profile.Profile{
		Name: "t",
		Schedule: profile.Schedule{
			Mode: profile.ModePeriodic, PeriodSec: 100, WindowSec: 20, LookaheadSec: 10,
		},
		Curves: profile.Curves{
			DelayMs:       []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: 20, Value: 1}},
			JitterMs:      []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: 20, Value: 1}},
			LossPct:       []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: 20, Value: 1}},
			BandwidthKbps: []profile.Point{{OffsetSec: 0, Value: 1}, {OffsetSec: 20, Value: 1}},
		},
	}
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ev, err := condition.NewEvaluator(*p, epoch)
	if err != nil {
		t.Fatal(err)
	}

	bus := newTestBus()
	srv := New(Config{
		Profiles:  []*profile.Profile{p},
		Registry:  device.NewRegistry(),
		Bus:       bus,
		Evaluator: ev,
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Drain initial coverage event.
	buf := make([]byte, 8192)
	_, _ = resp.Body.Read(buf)

	bus.PublishCoverageEvent(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowOpening,
		At:   epoch.Add(50 * time.Second),
	})

	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(buf[:n])
	if !bytes.Contains([]byte(body), []byte("window_opening")) {
		t.Fatalf("expected window_opening, got:\n%s", body)
	}
	if !bytes.Contains([]byte(body), []byte("lookahead_sec")) {
		t.Fatalf("expected lookahead_sec enrichment, got:\n%s", body)
	}
	if !bytes.Contains([]byte(body), []byte("next_open_at")) {
		t.Fatalf("expected next_open_at enrichment, got:\n%s", body)
	}

	bus.PublishCoverageEvent(eventbus.CoverageEvent{
		Kind: eventbus.KindWindowOpened,
		At:   epoch,
	})
	n, err = resp.Body.Read(buf)
	if err != nil {
		t.Fatalf("read opened: %v", err)
	}
	openedBody := string(buf[:n])
	if !bytes.Contains([]byte(openedBody), []byte("window_opened")) {
		t.Fatalf("expected window_opened, got:\n%s", openedBody)
	}
	if bytes.Contains([]byte(openedBody), []byte("lookahead_sec")) {
		t.Fatalf("window_opened should not include lookahead_sec, got:\n%s", openedBody)
	}
}

func TestFiniteSec(t *testing.T) {
	if got := finiteSec(math.Inf(1)); got != 1e18 {
		t.Errorf("Inf = %v, want 1e18", got)
	}
	if got := finiteSec(12.5); got != 12.5 {
		t.Errorf("finite = %v, want 12.5", got)
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

func TestSSE_NoBus_Returns503(t *testing.T) {
	srv := New(Config{
		Profiles: []*profile.Profile{},
		Registry: device.NewRegistry(),
		Bus:      nil,
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestSSE_StreamsEvents(t *testing.T) {
	bus := newTestBus()
	srv := New(Config{
		Profiles: []*profile.Profile{},
		Registry: device.NewRegistry(),
		Bus:      bus,
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Connect to SSE endpoint.
	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Publish a link-state event to the bus.
	bus.PublishLinkState(condition.LinkState{
		DelayMs:       42,
		JitterMs:      5,
		LossPct:       0.2,
		BandwidthKbps: 20000,
	}, testStart)

	// Read from the response body.
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatalf("reading SSE stream: %v", err)
	}
	body := string(buf[:n])

	if !bytes.Contains([]byte(body), []byte("event: linkstate")) {
		t.Errorf("expected 'event: linkstate' in body, got:\n%s", body)
	}
	if !bytes.Contains([]byte(body), []byte(`"delay_ms":42`)) {
		t.Errorf("expected delay_ms:42 in body, got:\n%s", body)
	}
}

func TestUI_ServesIndexHTML(t *testing.T) {
	srv := New(Config{
		Profiles: []*profile.Profile{},
		Registry: device.NewRegistry(),
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ui/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/ status = %d, want 200", resp.StatusCode)
	}

	var body bytes.Buffer
	body.ReadFrom(resp.Body)
	if !bytes.Contains(body.Bytes(), []byte("NTN-in-a-Box")) {
		t.Error("expected 'NTN-in-a-Box' in response body")
	}
}

func TestUI_RedirectsFromUI(t *testing.T) {
	srv := New(Config{
		Profiles: []*profile.Profile{},
		Registry: device.NewRegistry(),
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Get(ts.URL + "/ui")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("GET /ui status = %d, want 301", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/ui/" {
		t.Errorf("Location = %q, want /ui/", loc)
	}
}

func TestCapabilities_ReturnsDeviceCapabilities(t *testing.T) {
	_, ts := testServer(t)
	defer ts.Close()

	// Register a device first.
	body := `{"id":"cap-test","type":"virtual_ue","profile_name":"leo_pass_90s"}`
	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register device: status %d", resp.StatusCode)
	}

	// Get capabilities.
	resp, err = http.Get(ts.URL + "/devices/cap-test/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var caps struct {
		Data               bool    `json:"data"`
		CoverageMode       string  `json:"coverage_mode"`
		MaxBandwidthKbps   float64 `json:"max_bandwidth_kbps"`
		SupportsPrediction bool    `json:"supports_prediction"`
		Messaging          bool    `json:"messaging"`
		SOS                bool    `json:"sos"`
		StoreAndForward    bool    `json:"store_and_forward"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&caps); err != nil {
		t.Fatal(err)
	}

	if !caps.Data {
		t.Error("data should be true")
	}
	if caps.CoverageMode != "periodic" {
		t.Errorf("coverage_mode = %q, want periodic", caps.CoverageMode)
	}
	if caps.MaxBandwidthKbps != 2000 {
		t.Errorf("max_bandwidth_kbps = %f, want 2000", caps.MaxBandwidthKbps)
	}
	if !caps.SupportsPrediction {
		t.Error("supports_prediction should be true")
	}
	if caps.Messaging || caps.SOS || caps.StoreAndForward {
		t.Error("leo_pass_90s should not advertise messaging/sos/store_and_forward")
	}
}

func TestCapabilities_SOSProfile(t *testing.T) {
	p := &profile.Profile{
		Name: "sos_burst",
		Schedule: profile.Schedule{
			Mode: profile.ModePeriodic, PeriodSec: 480, WindowSec: 15, LookaheadSec: 20,
		},
		Curves: profile.Curves{
			DelayMs:       []profile.Point{{OffsetSec: 0, Value: 350}, {OffsetSec: 15, Value: 400}},
			JitterMs:      []profile.Point{{OffsetSec: 0, Value: 80}, {OffsetSec: 15, Value: 80}},
			LossPct:       []profile.Point{{OffsetSec: 0, Value: 25}, {OffsetSec: 15, Value: 30}},
			BandwidthKbps: []profile.Point{{OffsetSec: 0, Value: 8}, {OffsetSec: 15, Value: 16}},
		},
	}
	srv := New(Config{
		Profiles: []*profile.Profile{p},
		Registry: device.NewRegistry(),
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"id":"sos-1","type":"virtual_ue","profile_name":"sos_burst"}`
	resp, err := http.Post(ts.URL+"/devices", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/devices/sos-1/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var caps struct {
		Messaging       bool `json:"messaging"`
		SOS             bool `json:"sos"`
		StoreAndForward bool `json:"store_and_forward"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&caps); err != nil {
		t.Fatal(err)
	}
	if !caps.Messaging || !caps.SOS {
		t.Errorf("sos_burst caps = %+v, want messaging+sos true", caps)
	}
	if caps.StoreAndForward {
		t.Error("store_and_forward still unimplemented")
	}
}

func TestCapabilities_NotFound(t *testing.T) {
	_, ts := testServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/devices/nonexistent/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSSE_ForwardsReplayDone(t *testing.T) {
	bus := newTestBus()
	srv := New(Config{
		Profiles: []*profile.Profile{},
		Registry: device.NewRegistry(),
		Bus:      bus,
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Connect to SSE endpoint.
	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Publish a replay_done observability event.
	bus.Emit(eventbus.ObservabilityEvent{
		Name: eventbus.ObsReplayDone,
		At:   testStart,
	})

	// Read from the response body.
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatalf("reading SSE stream: %v", err)
	}
	body := string(buf[:n])

	if !bytes.Contains([]byte(body), []byte("event: lifecycle")) {
		t.Errorf("expected 'event: lifecycle' in body, got:\n%s", body)
	}
	if !bytes.Contains([]byte(body), []byte(`"kind":"replay_done"`)) {
		t.Errorf("expected kind:replay_done in body, got:\n%s", body)
	}
}
