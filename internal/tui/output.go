package tui

import (
	"fmt"
	"strings"
)

// renderRightPanel renders the right output panel: a header line with
// follow-mode indicator, a separator, and the viewport content.
func (m Model) renderRightPanel(width int) string {
	header := m.renderOutputHeader(width)
	separator := styleDim.Render(strings.Repeat("─", width))

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(separator)
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	return b.String()
}

// renderOutputHeader renders the output panel header line with the
// follow/paused indicator.
func (m Model) renderOutputHeader(width int) string {
	label := styleWhite.Render(" OUTPUT")

	var indicator string
	if m.followMode {
		indicator = styleDim.Render("follow [f]")
	} else {
		indicator = styleProgressYellow.Render("paused [f]")
	}

	// Pad between label and indicator.
	labelLen := 8 // " OUTPUT" visible length
	indicatorLen := 10
	padding := width - labelLen - indicatorLen
	if padding < 1 {
		padding = 1
	}

	return fmt.Sprintf("%s%s%s", label, strings.Repeat(" ", padding), indicator)
}

// injectCoverageSeparator writes a styled separator line into the
// output ring buffer when a coverage transition occurs.
func (m *Model) injectCoverageSeparator(opened bool, info float64) {
	var line string
	switch {
	case opened:
		line = fmt.Sprintf("── ▲ coverage opened · %.0fs window ──", info)
	case m.inBlockage:
		line = fmt.Sprintf("── ▼ blocked · clears in %.0fs ──", info)
	default:
		line = fmt.Sprintf("── ▼ coverage lost · next window in %.0fs ──", info)
	}
	m.output.Write(line)
}
