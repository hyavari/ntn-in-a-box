package apihost

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// sseEvent is the JSON payload for an SSE event.
type sseCoverageEvent struct {
	Kind                  string   `json:"kind"`
	InCoverage            bool     `json:"in_coverage"`
	ElapsedSec            float64  `json:"elapsed_sec"`
	UntilNextTransition   float64  `json:"until_next_transition"`
	CyclePosSec           float64  `json:"cycle_pos_sec"`
	InBlockage            bool     `json:"in_blockage,omitempty"`
	At                    string   `json:"at"`
	DeviceID              string   `json:"device_id,omitempty"`
	LookaheadSec          *float64 `json:"lookahead_sec,omitempty"`
	NextOpenAt            *string  `json:"next_open_at,omitempty"`
	NextCloseAt           *string  `json:"next_close_at,omitempty"`
	NextWindowDurationSec *float64 `json:"next_window_duration_sec,omitempty"`
	MaxElevationDeg       *float64 `json:"max_elevation_deg,omitempty"`
}

type sseLinkStateEvent struct {
	DelayMs       float64 `json:"delay_ms"`
	JitterMs      float64 `json:"jitter_ms"`
	LossPct       float64 `json:"loss_pct"`
	BandwidthKbps float64 `json:"bandwidth_kbps"`
	At            string  `json:"at"`
	DeviceID      string  `json:"device_id,omitempty"`
}

// handleSSE streams bus events to the client as Server-Sent Events.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if s.bus == nil {
		http.Error(w, "event bus not available", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher.Flush()

	// Buffered channel to decouple bus callbacks from the write loop.
	ch := make(chan []byte, 64)

	// Subscribe to coverage events.
	unsubCoverage := s.bus.SubscribeCoverage(func(ev eventbus.CoverageEvent) {
		payload := sseCoverageEvent{
			Kind:                string(ev.Kind),
			InCoverage:          ev.InCoverage,
			ElapsedSec:          ev.ElapsedSec,
			UntilNextTransition: ev.UntilNextTransition,
			At:                  ev.At.Format(time.RFC3339),
			DeviceID:            ev.DeviceID,
		}

		eval := s.evaluatorFor(ev.DeviceID)
		// Enrich with evaluator data if available (overrides replay values).
		if eval != nil {
			_, cov := eval.Evaluate(ev.At)
			payload.InCoverage = cov.InCoverage
			payload.ElapsedSec = cov.ElapsedSec
			payload.UntilNextTransition = cov.UntilNextTransitionSec
			payload.CyclePosSec = cov.CyclePosSec
			payload.InBlockage = cov.InBlockage
		} else if payload.ElapsedSec == 0 && payload.UntilNextTransition == 0 {
			// Fallback: derive from event kind.
			payload.InCoverage = ev.Kind == eventbus.KindWindowOpened || ev.Kind == eventbus.KindWindowOpening
		}

		payload.UntilNextTransition = finiteSec(payload.UntilNextTransition)

		if ev.Kind == eventbus.KindWindowOpening || ev.Kind == eventbus.KindWindowClosing {
			enrichLookaheadCoverage(&payload, eval, ev.At)
		}

		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		msg := fmt.Sprintf("event: coverage\ndata: %s\n\n", data)
		select {
		case ch <- []byte(msg):
		default:
		}
	})

	// Subscribe to link-state events.
	unsubLinkState := s.bus.SubscribeLinkState(func(ev eventbus.LinkStateEvent) {
		payload := sseLinkStateEvent{
			DelayMs:       ev.State.DelayMs,
			JitterMs:      ev.State.JitterMs,
			LossPct:       ev.State.LossPct,
			BandwidthKbps: ev.State.BandwidthKbps,
			At:            ev.At.Format(time.RFC3339),
			DeviceID:      ev.DeviceID,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		msg := fmt.Sprintf("event: linkstate\ndata: %s\n\n", data)
		select {
		case ch <- []byte(msg):
		default: // drop if channel full
		}
	})

	// Subscribe to observability events to forward replay lifecycle signals.
	unsubObs := s.bus.SubscribeObservability(func(ev eventbus.ObservabilityEvent) {
		if ev.Name != eventbus.ObsReplayDone {
			return
		}
		msg := fmt.Sprintf("event: lifecycle\ndata: %s\n\n", `{"kind":"replay_done"}`)
		select {
		case ch <- []byte(msg):
		default:
		}
	})

	unsubMessage := s.bus.SubscribeMessage(func(ev eventbus.MessageEvent) {
		payload := map[string]any{
			"id":     ev.ID,
			"from":   ev.From,
			"to":     ev.To,
			"status": ev.Status,
			"at":     ev.At.Format(time.RFC3339),
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		msg := fmt.Sprintf("event: message\ndata: %s\n\n", data)
		select {
		case ch <- []byte(msg):
		default:
		}
	})

	// Subscribe to satellite position events (TLE mode only).
	unsubPosition := s.bus.SubscribeSatellitePosition(func(ev eventbus.SatellitePositionEvent) {
		payload := struct {
			LatDeg       float64 `json:"lat_deg"`
			LonDeg       float64 `json:"lon_deg"`
			AltKm        float64 `json:"alt_km"`
			ElevationDeg float64 `json:"elevation_deg"`
			AzimuthDeg   float64 `json:"azimuth_deg"`
			RangeKm      float64 `json:"range_km"`
		}{
			LatDeg:       ev.LatDeg,
			LonDeg:       ev.LonDeg,
			AltKm:        ev.AltKm,
			ElevationDeg: ev.ElevationDeg,
			AzimuthDeg:   ev.AzimuthDeg,
			RangeKm:      ev.RangeKm,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		msg := fmt.Sprintf("event: satellite_position\ndata: %s\n\n", data)
		select {
		case ch <- []byte(msg):
		default:
		}
	})

	// Write loop: stream events until client disconnects.
	ctx := r.Context()

	// Send session_info as the very first event.
	if s.sessionInfo != nil {
		if data, err := json.Marshal(s.sessionInfo); err == nil {
			msg := fmt.Sprintf("event: session_info\ndata: %s\n\n", data)
			_, _ = w.Write([]byte(msg))
			flusher.Flush()
		}
	}

	// Send initial state immediately so the browser doesn't wait for
	// the next transition event.
	if s.eval != nil {
		now := time.Now()
		_, cov := s.eval.Evaluate(now)
		initial := sseCoverageEvent{
			Kind:                "initial",
			InCoverage:          cov.InCoverage,
			ElapsedSec:          cov.ElapsedSec,
			UntilNextTransition: finiteSec(cov.UntilNextTransitionSec),
			CyclePosSec:         cov.CyclePosSec,
			InBlockage:          cov.InBlockage,
			At:                  now.Format(time.RFC3339),
		}
		if data, err := json.Marshal(initial); err == nil {
			msg := fmt.Sprintf("event: coverage\ndata: %s\n\n", data)
			_, _ = w.Write([]byte(msg))
			flusher.Flush()
		}
	}

	for {
		select {
		case <-ctx.Done():
			unsubCoverage()
			unsubLinkState()
			unsubObs()
			unsubMessage()
			unsubPosition()
			return
		case msg := <-ch:
			_, _ = w.Write(msg)
			flusher.Flush()
		}
	}
}

func enrichLookaheadCoverage(payload *sseCoverageEvent, eval condition.Eval, at time.Time) {
	provider, ok := eval.(condition.LookaheadProvider)
	if !ok {
		return
	}
	st := provider.Lookahead(at)
	payload.LookaheadSec = condition.Float64Ptr(st.ConfiguredLookaheadSec)
	payload.NextWindowDurationSec = st.NextWindowDurationSec
	payload.MaxElevationDeg = st.MaxElevationDeg
	if st.NextOpenAt != nil {
		s := st.NextOpenAt.UTC().Format(time.RFC3339)
		payload.NextOpenAt = &s
	}
	if st.NextCloseAt != nil {
		s := st.NextCloseAt.UTC().Format(time.RFC3339)
		payload.NextCloseAt = &s
	}
}

// finiteSec maps Inf/NaN to a large finite value so encoding/json succeeds.
func finiteSec(v float64) float64 {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 1e18
	}
	return v
}
