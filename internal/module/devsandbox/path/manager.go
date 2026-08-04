// Package path selects the active egress bearer for dual-path sandboxes.
package path

import "sync"

// Bearer names exposed on the API and in handover events.
const (
	BearerSatellite   = "satellite"
	BearerTerrestrial = "terrestrial"
)

// Handover reasons (closed set).
const (
	ReasonCoverageGained = "satellite_coverage_gained"
	ReasonCoverageLost   = "satellite_coverage_lost"
	ReasonBlocked        = "satellite_blocked"
	ReasonUnblocked      = "satellite_unblocked"
)

// Transition is emitted when the selected bearer changes after Init.
type Transition struct {
	From   string
	To     string
	Reason string
}

// Manager tracks the active bearer for one device with terrestrial fallback.
type Manager struct {
	mu                sync.Mutex
	enabled           bool
	current           string
	initialized       bool
	lastLeaveBlockage bool
}

// New returns a Manager. When enabled is false, methods are no-ops.
func New(enabled bool) *Manager {
	return &Manager{enabled: enabled}
}

// Enabled reports whether dual-path selection is active.
func (m *Manager) Enabled() bool {
	return m != nil && m.enabled
}

// Current returns the selected bearer after Init/TransitionTo, or "".
func (m *Manager) Current() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// Desired returns the bearer that should be active for the coverage sample.
func Desired(inCoverage bool) string {
	if inCoverage {
		return BearerSatellite
	}
	return BearerTerrestrial
}

// Init sets the initial bearer from satellite coverage without emitting a
// transition. Call once before the child process starts.
func (m *Manager) Init(inCoverage bool) string {
	if m == nil || !m.enabled {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = Desired(inCoverage)
	m.initialized = true
	m.lastLeaveBlockage = false
	return m.current
}

// TransitionTo commits want as the active bearer. Returns a Transition when
// the bearer changed. Call only after the default route has been switched
// successfully (or for Init-equivalent cases where route already matches).
func (m *Manager) TransitionTo(want string, inBlockage bool) *Transition {
	if m == nil || !m.enabled {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		m.current = want
		m.initialized = true
		m.lastLeaveBlockage = want == BearerTerrestrial && inBlockage
		return nil
	}
	if want == m.current {
		if want == BearerTerrestrial && inBlockage {
			m.lastLeaveBlockage = true
		}
		return nil
	}
	from := m.current
	reason := transitionReason(from, want, inBlockage, m.lastLeaveBlockage)
	if want == BearerTerrestrial {
		m.lastLeaveBlockage = inBlockage
	} else {
		m.lastLeaveBlockage = false
	}
	m.current = want
	return &Transition{From: from, To: want, Reason: reason}
}

func transitionReason(from, to string, inBlockage, lastLeaveBlockage bool) string {
	if to == BearerTerrestrial {
		if inBlockage {
			return ReasonBlocked
		}
		return ReasonCoverageLost
	}
	if from == BearerTerrestrial && lastLeaveBlockage {
		return ReasonUnblocked
	}
	return ReasonCoverageGained
}

// GatewayFor returns the host-side gateway IP for the active bearer.
func GatewayFor(bearer, satGateway, terrGateway string) string {
	if bearer == BearerTerrestrial {
		return terrGateway
	}
	return satGateway
}
