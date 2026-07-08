package tui

import (
	"fmt"
	"strings"

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
	sections = append(sections, m.renderHelpLine())

	return strings.Join(sections, "\n")
}

// renderCoverageStatus renders the ▲/▼ status indicator.
func (m Model) renderCoverageStatus(width int) string {
	if m.inCoverage {
		return " " + styleStatusGreen.Render("▲ IN COVERAGE")
	}
	return " " + styleStatusRed.Render("▼ OUT OF COVERAGE")
}

// renderProgressBar renders a colored progress bar with percentage and
// countdown.
func (m Model) renderProgressBar(width int) string {
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

// renderLinkMetrics renders the four metric rows with values and
// sparklines.
func (m Model) renderLinkMetrics(width int) string {
	if !m.hasLink && !m.inCoverage {
		return styleDim.Render(" no link")
	}

	var lines []string

	if m.hasLink {
		lines = append(lines, m.renderMetricRow("delay ", m.linkState.DelayMs, "ms", m.delayHistory, width))
		lines = append(lines, m.renderMetricRow("jitter", m.linkState.JitterMs, "ms", m.jitterHistory, width))
		lines = append(lines, m.renderMetricRow("loss  ", m.linkState.LossPct, "% ", m.lossHistory, width))
		lines = append(lines, m.renderMetricRow("bw    ", m.linkState.BandwidthKbps, "kb", m.bandwidthHistory, width))
	} else {
		lines = append(lines, styleDim.Render(" delay   —"))
		lines = append(lines, styleDim.Render(" jitter  —"))
		lines = append(lines, styleDim.Render(" loss    —"))
		lines = append(lines, styleDim.Render(" bw      —"))
	}

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
	help := styleDim.Render(" [q]uit [f]ollow [Tab]expand [↑↓]scroll")
	if m.addr != "" {
		help += "\n" + styleDim.Render(" GUI: http://localhost:"+addrPort(m.addr)+"/ui")
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
	if m.inCoverage {
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

	return fmt.Sprintf(" %s · %s · %.0f%% · %.0fs%s",
		status,
		styleDim.Render(m.profile.Name),
		m.coveragePercent,
		m.remainingSec,
		metrics)
}
