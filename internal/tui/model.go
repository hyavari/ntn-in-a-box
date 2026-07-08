package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// LayoutMode selects between split and stacked rendering.
type LayoutMode int

const (
	LayoutSplit  LayoutMode = iota // metrics left, output right
	LayoutStacked                  // compact header, output below
)

// splitThreshold is the minimum terminal width for the split layout.
const splitThreshold = 100

// Model is the top-level bubbletea model for the TUI dashboard.
type Model struct {
	// Terminal dimensions.
	width  int
	height int

	// Layout state.
	layoutMode   LayoutMode
	expandedMode bool // Tab toggle: output takes ~90% in split mode

	// Coverage state.
	inCoverage          bool
	coveragePercent     float64
	remainingSec        float64
	inLookahead         bool
	elapsedSec          float64

	// Link metrics.
	linkState condition.LinkState
	hasLink   bool

	// Sparklines (last 20 samples per metric).
	delayHistory     []float64
	jitterHistory    []float64
	lossHistory      []float64
	bandwidthHistory []float64

	// Profile metadata.
	profile profile.Profile

	// Output pane.
	output     *RingBuffer
	viewport   viewport.Model
	followMode bool

	// Child process state.
	cmdExited bool
	exitCode  int

	// API address (for displaying GUI URL).
	addr string

	// Ready indicates that the first WindowSizeMsg has been received
	// and the viewport is initialized.
	ready bool
}

// NewModel creates an initial model with the given profile and ring
// buffer capacity.
func NewModel(p profile.Profile, bufferCapacity int) Model {
	return Model{
		profile:    p,
		output:     NewRingBuffer(bufferCapacity),
		followMode: true,
		inCoverage: true, // optimistic until first event
	}
}

// Init starts the 1-second countdown tick.
func (m Model) Init() tea.Cmd {
	return tickCmd()
}

// Update handles incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutMode = layoutForWidth(msg.Width)

		vpWidth, vpHeight := m.viewportDimensions()
		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.viewport.SetContent(m.renderOutputContent())
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
			m.viewport.SetContent(m.renderOutputContent())
		}

	case CoverageMsg:
		m.inCoverage = msg.InCoverage
		m.elapsedSec = msg.ElapsedSec
		m.remainingSec = msg.UntilNextTransition
		m.coveragePercent = m.computeCoveragePercent()
		m.inLookahead = m.inCoverage && m.remainingSec <= m.profile.Schedule.LookaheadSec

		// Inject coverage separator into output pane on actual transitions.
		switch msg.Kind {
		case "window_opened":
			m.injectCoverageSeparator(true, msg.UntilNextTransition)
			if m.ready && m.followMode {
				m.viewport.SetContent(m.renderOutputContent())
				m.viewport.GotoBottom()
			}
		case "window_closed":
			m.injectCoverageSeparator(false, msg.UntilNextTransition)
			if m.ready && m.followMode {
				m.viewport.SetContent(m.renderOutputContent())
				m.viewport.GotoBottom()
			}
		}

	case LinkStateMsg:
		m.linkState = msg.State
		m.hasLink = true
		m.pushSparkline(msg.State)

	case OutputLineMsg:
		m.output.Write(msg.Line)
		if m.ready && m.followMode {
			m.viewport.SetContent(m.renderOutputContent())
			m.viewport.GotoBottom()
		}

	case CmdExitedMsg:
		m.cmdExited = true
		m.exitCode = msg.Code
		m.output.Write("") // blank separator
		if msg.Err != nil {
			m.output.Write("process exited (code " + itoa(msg.Code) + "): " + msg.Err.Error())
		} else {
			m.output.Write("process exited (code " + itoa(msg.Code) + ")")
		}
		if m.ready && m.followMode {
			m.viewport.SetContent(m.renderOutputContent())
			m.viewport.GotoBottom()
		}

	case TickMsg:
		if m.remainingSec > 0 {
			m.remainingSec -= 1.0
			if m.remainingSec < 0 {
				m.remainingSec = 0
			}
			m.coveragePercent = m.computeCoveragePercent()
			m.inLookahead = m.inCoverage && m.remainingSec <= m.profile.Schedule.LookaheadSec
		}
		return m, tickCmd()
	}

	return m, nil
}

// View renders the full TUI.
func (m Model) View() string {
	if !m.ready {
		return "initializing..."
	}
	return m.renderLayout()
}

// computeCoveragePercent calculates the progress through the current
// phase (in-coverage window or out-of-coverage gap) using remainingSec
// which is kept current by both CoverageMsg and TickMsg.
func (m Model) computeCoveragePercent() float64 {
	if m.profile.Schedule.Mode == profile.ModeContinuous {
		if m.profile.Schedule.PeriodSec == 0 {
			return 0
		}
		elapsed := m.profile.Schedule.PeriodSec - m.remainingSec
		return elapsed / m.profile.Schedule.PeriodSec * 100
	}
	if m.inCoverage {
		if m.profile.Schedule.WindowSec == 0 {
			return 0
		}
		elapsed := m.profile.Schedule.WindowSec - m.remainingSec
		if elapsed < 0 {
			elapsed = 0
		}
		return elapsed / m.profile.Schedule.WindowSec * 100
	}
	gap := m.profile.Schedule.PeriodSec - m.profile.Schedule.WindowSec
	if gap == 0 {
		return 0
	}
	elapsed := gap - m.remainingSec
	if elapsed < 0 {
		elapsed = 0
	}
	return elapsed / gap * 100
}

func (m *Model) pushSparkline(s condition.LinkState) {
	const maxSamples = 20
	m.delayHistory = pushSample(m.delayHistory, s.DelayMs, maxSamples)
	m.jitterHistory = pushSample(m.jitterHistory, s.JitterMs, maxSamples)
	m.lossHistory = pushSample(m.lossHistory, s.LossPct, maxSamples)
	m.bandwidthHistory = pushSample(m.bandwidthHistory, s.BandwidthKbps, maxSamples)
}

func pushSample(history []float64, val float64, max int) []float64 {
	history = append(history, val)
	if len(history) > max {
		history = history[len(history)-max:]
	}
	return history
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}


// tickCmd returns a tea.Cmd that fires a TickMsg after 1 second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg{At: t}
	})
}
