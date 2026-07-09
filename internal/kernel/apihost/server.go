package apihost

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/device"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// Server is the kernel's HTTP API host. It exposes health, profile,
// device, and condition-state endpoints. It wires together the kernel
// packages into a queryable surface.
type Server struct {
	mux      *http.ServeMux
	profiles map[string]*profile.Profile
	registry *device.Registry
	bus      *eventbus.Bus
	eval     *condition.Evaluator

	// Per-device evaluators, created at device registration time.
	mu         sync.RWMutex
	evaluators map[string]*condition.Evaluator
}

// Config holds what the server needs to start.
type Config struct {
	Profiles  []*profile.Profile
	Registry  *device.Registry
	Bus       *eventbus.Bus        // optional; if nil, /events returns 503
	Evaluator *condition.Evaluator // optional; used by SSE to enrich coverage events
}

// New creates a Server with the given config and returns it ready to
// serve. The server does not start listening — call ListenAndServe or
// use Handler() with httptest.
func New(cfg Config) *Server {
	profiles := make(map[string]*profile.Profile, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		profiles[p.Name] = p
	}

	s := &Server{
		mux:        http.NewServeMux(),
		profiles:   profiles,
		registry:   cfg.Registry,
		bus:        cfg.Bus,
		eval:       cfg.Evaluator,
		evaluators: make(map[string]*condition.Evaluator),
	}
	s.registerRoutes()
	return s
}

// Handler returns the http.Handler for use in tests or custom servers.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Handle registers a handler for a pattern on the server's mux.
// Satisfies the module.RouteRegistrar interface.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

// RegisterEvaluator seeds a device evaluator so that
// GET /devices/{id}/condition works for devices registered outside
// the HTTP API (e.g. by ntnbox run).
func (s *Server) RegisterEvaluator(deviceID string, eval *condition.Evaluator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evaluators[deviceID] = eval
}

// ListenAndServe starts the HTTP server on addr.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return srv.ListenAndServe()
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /echo", s.handleEcho)
	s.mux.HandleFunc("GET /profiles", s.handleListProfiles)
	s.mux.HandleFunc("GET /profiles/{name}", s.handleGetProfile)
	s.mux.HandleFunc("POST /devices", s.handleRegisterDevice)
	s.mux.HandleFunc("GET /devices", s.handleListDevices)
	s.mux.HandleFunc("GET /devices/{id}", s.handleGetDevice)
	s.mux.HandleFunc("GET /devices/{id}/condition", s.handleGetCondition)
	s.mux.HandleFunc("GET /devices/{id}/capabilities", s.handleGetCapabilities)
	s.mux.HandleFunc("GET /events", s.handleSSE)
	s.registerUI()
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleEcho(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"ts": time.Now().Format(time.RFC3339)})
}

func (s *Server) handleListProfiles(w http.ResponseWriter, _ *http.Request) {
	type profileSummary struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Mode        string `json:"mode"`
	}

	summaries := make([]profileSummary, 0, len(s.profiles))
	for _, p := range s.profiles {
		summaries = append(summaries, profileSummary{
			Name:        p.Name,
			Description: p.Description,
			Mode:        string(p.Schedule.Mode),
		})
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, ok := s.profiles[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found: " + name})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type registerDeviceRequest struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	ProfileName string `json:"profile_name"`
}

type deviceResponse struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	ProfileName string `json:"profile_name"`
	CreatedAt   string `json:"created_at"`
}

func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	var req registerDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Verify the profile exists.
	p, ok := s.profiles[req.ProfileName]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown profile: " + req.ProfileName})
		return
	}

	d, err := s.registry.Register(req.ID, device.Type(req.Type), req.ProfileName)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	// Create an evaluator for this device, anchored at registration time.
	eval, err := condition.NewEvaluator(*p, d.CreatedAt)
	if err != nil {
		// This shouldn't happen — profiles are validated at load time.
		// Remove the device we just registered to keep state consistent.
		_ = s.registry.Remove(d.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create evaluator: " + err.Error()})
		return
	}

	s.mu.Lock()
	s.evaluators[d.ID] = eval
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, toDeviceResponse(d))
}

func (s *Server) handleListDevices(w http.ResponseWriter, _ *http.Request) {
	devices := s.registry.List()
	resp := make([]deviceResponse, 0, len(devices))
	for _, d := range devices {
		resp = append(resp, toDeviceResponse(d))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := s.registry.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, toDeviceResponse(d))
}

type conditionResponse struct {
	InCoverage             bool    `json:"in_coverage"`
	ElapsedSec             float64 `json:"elapsed_sec"`
	UntilNextTransitionSec float64 `json:"until_next_transition_sec"`
	DelayMs                float64 `json:"delay_ms,omitempty"`
	JitterMs               float64 `json:"jitter_ms,omitempty"`
	LossPct                float64 `json:"loss_pct,omitempty"`
	BandwidthKbps          float64 `json:"bandwidth_kbps,omitempty"`
}

func (s *Server) handleGetCondition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Check device exists.
	if _, err := s.registry.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	s.mu.RLock()
	eval, ok := s.evaluators[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no evaluator for device: " + id})
		return
	}

	link, cov := eval.Evaluate(time.Now())

	resp := conditionResponse{
		InCoverage:             cov.InCoverage,
		ElapsedSec:             cov.ElapsedSec,
		UntilNextTransitionSec: cov.UntilNextTransitionSec,
	}
	if cov.InCoverage {
		resp.DelayMs = link.DelayMs
		resp.JitterMs = link.JitterMs
		resp.LossPct = link.LossPct
		resp.BandwidthKbps = link.BandwidthKbps
	}

	writeJSON(w, http.StatusOK, resp)
}

func toDeviceResponse(d device.Device) deviceResponse {
	return deviceResponse{
		ID:          d.ID,
		Type:        string(d.Type),
		ProfileName: d.ProfileName,
		CreatedAt:   d.CreatedAt.Format(time.RFC3339),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type capabilitiesResponse struct {
	Messaging       bool    `json:"messaging"`
	StoreAndForward bool    `json:"store_and_forward"`
	SOS             bool    `json:"sos"`
	Voice           bool    `json:"voice"`
	Data            bool    `json:"data"`
	CoverageMode    string  `json:"coverage_mode"`
	MaxBandwidthKbps float64 `json:"max_bandwidth_kbps"`
	SupportsPrediction bool `json:"supports_prediction"`
}

func (s *Server) handleGetCapabilities(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	d, err := s.registry.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	p, ok := s.profiles[d.ProfileName]
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile not found: " + d.ProfileName})
		return
	}

	// Compute max bandwidth from the profile's curves.
	var maxBw float64
	for _, pt := range p.Curves.BandwidthKbps {
		if pt.Value > maxBw {
			maxBw = pt.Value
		}
	}

	resp := capabilitiesResponse{
		Messaging:        false, // not yet implemented
		StoreAndForward:  false, // not yet implemented
		SOS:              false, // not yet implemented
		Voice:            false, // not yet implemented
		Data:             true,  // always available (Dev Sandbox shapes data)
		CoverageMode:     string(p.Schedule.Mode),
		MaxBandwidthKbps: maxBw,
		SupportsPrediction: true, // evaluator can predict future state
	}

	writeJSON(w, http.StatusOK, resp)
}
