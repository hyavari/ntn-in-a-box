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
	LayoutSplit   LayoutMode = iota // metrics left, output right
	LayoutStacked                   // compact header, output below
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
	inCoverage      bool
	coveragePercent float64
	remainingSec    float64
	inLookahead     bool
	elapsedSec      float64

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

	// Replay mode state.
	isReplay      bool
	replayElapsed time.Duration
	replayTotal   time.Duration
	replayDone    bool
	replayErr     error // non-nil if replay failed
	replayAgain   bool  // set when user chooses to replay again

	// Store-and-forward message list (newest at end; upsert by id).
	messages       []messageRow
	messageScroll  int
	messageCap     int
	messageFollow  bool // auto-scroll to newest on append

	// Multi-device focus (coverage/link filter); empty = show all / primary.
	focusDeviceID string
	deviceIDs     []string
	onFocusChange func(id string) // set by Run; swaps adapter focus/eval

	// API address (for displaying GUI URL).
	addr string

	// Ready indicates that the first WindowSizeMsg has been received
	// and the viewport is initialized.
	ready bool
}

const (
	messageListCap     = 50
	messageVisibleRows = 8
)

// NewModel creates an initial model with the given profile and ring
// buffer capacity.
func NewModel(p profile.Profile, bufferCapacity int) Model {
	return Model{
		profile:       p,
		output:        NewRingBuffer(bufferCapacity),
		followMode:    true,
		messageFollow: true,
		inCoverage:    true, // optimistic until first event
		messageCap:    messageListCap,
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

	case MessageLifecycleMsg:
		m.upsertMessage(messageRow{
			ID:     msg.ID,
			From:   msg.From,
			To:     msg.To,
			Status: msg.Status,
		})

	case OutputLineMsg:
		// Stop showing output once replay is complete (child will be killed on exit).
		if m.isReplay && m.replayDone {
			break
		}
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

	case ReplayProgressMsg:
		m.replayElapsed = msg.Elapsed
		m.replayTotal = msg.Total

	case ReplayDoneMsg:
		m.replayDone = true
		m.replayErr = msg.Err
		m.output.Write("")
		if msg.Err != nil {
			m.output.Write("── replay failed: " + msg.Err.Error() + " ──")
		} else {
			m.output.Write("── replay complete ──")
		}
		if m.ready && m.followMode {
			m.viewport.SetContent(m.renderOutputContent())
			m.viewport.GotoBottom()
		}
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

func (m *Model) upsertMessage(row messageRow) {
	for i := range m.messages {
		if m.messages[i].ID == row.ID {
			m.messages[i] = row
			return
		}
	}
	m.messages = append(m.messages, row)
	cap := m.messageCap
	if cap <= 0 {
		cap = messageListCap
	}
	if len(m.messages) > cap {
		m.messages = m.messages[len(m.messages)-cap:]
	}
	maxScroll := len(m.messages) - messageVisibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.messageFollow {
		m.messageScroll = maxScroll
	} else if m.messageScroll > maxScroll {
		m.messageScroll = maxScroll
	}
}

func (m *Model) clearMetricsForFocusChange() {
	m.hasLink = false
	m.delayHistory = nil
	m.jitterHistory = nil
	m.lossHistory = nil
	m.bandwidthHistory = nil
	m.inLookahead = false
	// Coverage fields are refreshed via FocusRefresh from Run's onFocusChange
	// (Evaluate on the newly focused device). Until that arrives, avoid
	// labeling the new device with the previous device's coverage.
	m.inCoverage = false
	m.remainingSec = 0
	m.elapsedSec = 0
	m.coveragePercent = 0
}

// cycleFocus advances to the next device id. Returns the new focus id
// and whether it changed.
func (m *Model) cycleFocus() (string, bool) {
	if len(m.deviceIDs) < 2 {
		return m.focusDeviceID, false
	}
	idx := 0
	for i, id := range m.deviceIDs {
		if id == m.focusDeviceID {
			idx = (i + 1) % len(m.deviceIDs)
			break
		}
	}
	next := m.deviceIDs[idx]
	if next == m.focusDeviceID {
		return m.focusDeviceID, false
	}
	m.focusDeviceID = next
	m.clearMetricsForFocusChange()
	return next, true
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
