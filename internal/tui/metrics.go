package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Color palette.
var (
	colorGreen  = lipgloss.Color("#4ade80")
	colorRed    = lipgloss.Color("#f87171")
	colorYellow = lipgloss.Color("#facc15")
	colorDim    = lipgloss.Color("#64748b")
	colorWhite  = lipgloss.Color("#e0e0e0")
)

// Styles.
var (
	styleStatusGreen    = lipgloss.NewStyle().Bold(true).Foreground(colorGreen)
	styleStatusRed      = lipgloss.NewStyle().Bold(true).Foreground(colorRed)
	styleProgressGreen  = lipgloss.NewStyle().Foreground(colorGreen)
	styleProgressRed    = lipgloss.NewStyle().Foreground(colorRed)
	styleProgressYellow = lipgloss.NewStyle().Foreground(colorYellow)
	styleDim            = lipgloss.NewStyle().Foreground(colorDim)
	styleWhite          = lipgloss.NewStyle().Foreground(colorWhite)
)

// renderLeftPanel renders the full left panel: title, coverage status,
// progress bar, metrics, profile info, and help.
func (m Model) renderLeftPanel(width int) string {
	var sections []string

	sections = append(sections, styleStatusGreen.Render(" NTN-in-a-Box"))
	sections = append(sections, "") // blank separator
	sections = append(sections, m.renderCoverageStatus(width))
	sections = append(sections, m.renderProgressBar(width))
	sections = append(sections, "") // blank separator
	sections = append(sections, m.renderLinkMetrics(width))
	sections = append(sections, "") // blank separator
	sections = append(sections, m.renderProfileInfo())
	sections = append(sections, "") // blank separator
	sections = append(sections, m.renderMessages(width))
	sections = append(sections, "") // blank separator
	sections = append(sections, m.renderHelpLine())

	return strings.Join(sections, "\n")
}

// renderCoverageStatus renders the ▲/▼ status indicator.
func (m Model) renderCoverageStatus(width int) string {
	if m.isReplay {
		if m.replayDone {
			if m.replayErr != nil {
				return " " + styleStatusRed.Render("✗ REPLAY FAILED")
			}
			return " " + styleStatusGreen.Render("✓ REPLAY COMPLETE")
		}
		return " " + styleDim.Render("▶ REPLAYING")
	}
	if m.inCoverage {
		if m.focusDeviceID != "" {
			return " " + styleStatusGreen.Render("▲ "+m.focusDeviceID+" IN COVERAGE")
		}
		return " " + styleStatusGreen.Render("▲ IN COVERAGE")
	}
	if m.focusDeviceID != "" {
		return " " + styleStatusRed.Render("▼ "+m.focusDeviceID+" OUT OF COVERAGE")
	}
	return " " + styleStatusRed.Render("▼ OUT OF COVERAGE")
}

// renderProgressBar renders a colored progress bar with percentage and
// countdown.
func (m Model) renderProgressBar(width int) string {
	if m.isReplay {
		return m.renderReplayProgressBar(width)
	}
	// Format: " [████░░░░] 65% · 45s left"
	suffix := fmt.Sprintf(" %.0f%% · %.0fs left", m.coveragePercent, m.remainingSec)
	// barWidth = panel width - 1 leading space - 2 brackets - len(suffix)
	barWidth := width - 1 - 2 - len(suffix)
	if barWidth < 5 {
		barWidth = 5
	}

	filled := int(m.coveragePercent / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	empty := barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	// Pick color based on state.
	var styledBar string
	switch {
	case !m.inCoverage:
		styledBar = styleProgressRed.Render("[" + bar + "]")
	case m.inLookahead:
		styledBar = styleProgressYellow.Render("[" + bar + "]")
	default:
		styledBar = styleProgressGreen.Render("[" + bar + "]")
	}

	return " " + styledBar + styleDim.Render(suffix)
}

// renderReplayProgressBar renders a progress bar for replay mode showing
// elapsed / total duration.
func (m Model) renderReplayProgressBar(width int) string {
	pct := 0.0
	if m.replayTotal > 0 {
		pct = float64(m.replayElapsed) / float64(m.replayTotal) * 100
	}
	if pct > 100 {
		pct = 100
	}

	suffix := fmt.Sprintf(" %.0f%% · %s / %s", pct, formatDuration(m.replayElapsed), formatDuration(m.replayTotal))
	barWidth := width - 1 - 2 - len(suffix)
	if barWidth < 5 {
		barWidth = 5
	}

	filled := int(pct / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	empty := barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	styledBar := styleProgressGreen.Render("[" + bar + "]")

	return " " + styledBar + styleDim.Render(suffix)
}

// formatDuration formats a duration as m:ss or h:mm:ss.
func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// renderLinkMetrics renders the four metric rows with values and
// sparklines. Out of coverage shows placeholders (not stale last-pass values).
func (m Model) renderLinkMetrics(width int) string {
	if !m.inCoverage || !m.hasLink {
		return strings.Join([]string{
			styleDim.Render(" delay   —"),
			styleDim.Render(" jitter  —"),
			styleDim.Render(" loss    —"),
			styleDim.Render(" bw      —"),
		}, "\n")
	}

	var lines []string
	lines = append(lines, m.renderMetricRow("delay ", m.linkState.DelayMs, "ms", m.delayHistory, width))
	lines = append(lines, m.renderMetricRow("jitter", m.linkState.JitterMs, "ms", m.jitterHistory, width))
	lines = append(lines, m.renderMetricRow("loss  ", m.linkState.LossPct, "% ", m.lossHistory, width))
	lines = append(lines, m.renderMetricRow("bw    ", m.linkState.BandwidthKbps, "kb", m.bandwidthHistory, width))
	return strings.Join(lines, "\n")
}

// renderMetricRow renders one metric: label, value, and sparkline.
func (m Model) renderMetricRow(label string, value float64, unit string, history []float64, width int) string {
	valStr := formatMetricValue(value, unit)
	spark := renderSparkline(history)

	if !m.inCoverage {
		// Dim everything when out of coverage.
		return styleDim.Render(fmt.Sprintf(" %s %s %s", label, valStr, spark))
	}
	return fmt.Sprintf(" %s %s %s",
		styleDim.Render(label),
		styleWhite.Render(valStr),
		styleDim.Render(spark))
}

// formatMetricValue formats a numeric value with its unit, right-padded
// to 7 characters for alignment.
func formatMetricValue(val float64, unit string) string {
	var s string
	if val >= 1000 {
		s = fmt.Sprintf("%.0f%s", val, unit)
	} else if val >= 100 {
		s = fmt.Sprintf("%.0f%s", val, unit)
	} else if val >= 10 {
		s = fmt.Sprintf("%.1f%s", val, unit)
	} else {
		s = fmt.Sprintf("%.1f%s", val, unit)
	}
	// Right-align in 7 chars.
	for len(s) < 7 {
		s = " " + s
	}
	return s
}

// renderMessages renders the store-and-forward message list (no body).
func (m Model) renderMessages(width int) string {
	var lines []string
	lines = append(lines, styleDim.Render(" Messages"))
	if len(m.messages) == 0 {
		lines = append(lines, styleDim.Render(" (none yet)"))
		return strings.Join(lines, "\n")
	}

	start := m.messageScroll
	if start < 0 {
		start = 0
	}
	end := start + messageVisibleRows
	if end > len(m.messages) {
		end = len(m.messages)
	}
	for _, row := range m.messages[start:end] {
		id := row.ID
		if len(id) > 10 {
			id = id[:10]
		}
		line := fmt.Sprintf(" %s %s→%s %s", id, row.From, row.To, row.Status)
		if lipgloss.Width(line) > width {
			line = truncateToWidth(line, width)
		}
		lines = append(lines, styleWhite.Render(line))
	}
	if len(m.messages) > messageVisibleRows {
		follow := ""
		if m.messageFollow {
			follow = " ·follow"
		}
		lines = append(lines, styleDim.Render(fmt.Sprintf(" [%d/%d]%s", end, len(m.messages), follow)))
	}
	return strings.Join(lines, "\n")
}

// renderProfileInfo renders static profile metadata.
func (m Model) renderProfileInfo() string {
	mode := string(m.profile.Schedule.Mode)
	var schedLine string
	if m.profile.Schedule.Mode == "periodic" {
		schedLine = fmt.Sprintf(" period: %.0fs / window: %.0fs",
			m.profile.Schedule.PeriodSec, m.profile.Schedule.WindowSec)
	} else {
		schedLine = fmt.Sprintf(" period: %.0fs (continuous)",
			m.profile.Schedule.PeriodSec)
	}
	return strings.Join([]string{
		styleDim.Render(" profile: " + m.profile.Name),
		styleDim.Render(" mode:    " + mode),
		styleDim.Render(schedLine),
	}, "\n")
}

// renderHelpLine renders the keyboard shortcut hints and GUI URL.
func (m Model) renderHelpLine() string {
	var help string
	if m.isReplay && m.replayDone {
		help = styleProgressGreen.Render(" [r]eplay") + styleDim.Render(" [q]uit")
	} else {
		// Keep short — left panel is ~40% width and truncates.
		keys := " [q] [f]ollow [Tab] [J/K]msg"
		if len(m.deviceIDs) > 1 {
			keys += " [d]ev"
		}
		help = styleDim.Render(keys)
	}
	if m.addr != "" {
		help += "\n" + styleDim.Render(" http://localhost:"+addrPort(m.addr)+"/ui")
	}
	return help
}

// addrPort extracts the port from an address like ":8080" or "0.0.0.0:8080".
func addrPort(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i+1:]
		}
	}
	return addr
}

// renderStackedHeader renders the compact 1-line header for the
// stacked/expanded layout.
func (m Model) renderStackedHeader() string {
	var status string
	if m.isReplay {
		if m.replayDone {
			status = styleStatusGreen.Render("✓ DONE")
		} else {
			pct := 0.0
			if m.replayTotal > 0 {
				pct = float64(m.replayElapsed) / float64(m.replayTotal) * 100
			}
			status = styleDim.Render(fmt.Sprintf("▶ %.0f%%", pct))
		}
	} else if m.inCoverage {
		status = styleStatusGreen.Render("▲ IN")
	} else {
		status = styleStatusRed.Render("▼ OUT")
	}

	metrics := ""
	if m.hasLink {
		metrics = fmt.Sprintf("  %s  %s  %s  %s",
			styleDim.Render("d:")+styleWhite.Render(fmt.Sprintf("%.0fms", m.linkState.DelayMs)),
			styleDim.Render("j:")+styleWhite.Render(fmt.Sprintf("%.0fms", m.linkState.JitterMs)),
			styleDim.Render("l:")+styleWhite.Render(fmt.Sprintf("%.1f%%", m.linkState.LossPct)),
			styleDim.Render("bw:")+styleWhite.Render(fmt.Sprintf("%.0fkb", m.linkState.BandwidthKbps)))
	}

	if m.isReplay {
		return fmt.Sprintf(" %s · %s · %s / %s%s",
			status,
			styleDim.Render("replay"),
			formatDuration(m.replayElapsed),
			formatDuration(m.replayTotal),
			metrics)
	}

	return fmt.Sprintf(" %s · %s · %.0f%% · %.0fs%s",
		status,
		styleDim.Render(m.profile.Name),
		m.coveragePercent,
		m.remainingSec,
		metrics)
}
