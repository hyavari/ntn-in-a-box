package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// layoutForWidth returns the appropriate layout mode for the given
// terminal width.
func layoutForWidth(width int) LayoutMode {
	if width >= splitThreshold {
		return LayoutSplit
	}
	return LayoutStacked
}

// renderLayout dispatches to the appropriate layout renderer.
func (m Model) renderLayout() string {
	if m.layoutMode == LayoutStacked || (m.layoutMode == LayoutSplit && m.expandedMode) {
		return m.renderStacked()
	}
	return m.renderSplit()
}

// renderSplit renders the split panel layout (left metrics, right output).
func (m Model) renderSplit() string {
	leftWidth := m.width * 40 / 100
	rightWidth := m.width - leftWidth - 1 // -1 for separator

	left := m.renderLeftPanel(leftWidth)
	right := m.renderRightPanel(rightWidth)

	// Combine line by line with a vertical separator.
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	// Pad to equal height.
	for len(leftLines) < m.height {
		leftLines = append(leftLines, strings.Repeat(" ", leftWidth))
	}
	for len(rightLines) < m.height {
		rightLines = append(rightLines, "")
	}

	var b strings.Builder
	for i := range m.height {
		l := leftLines[i]
		// Pad/truncate left line to fixed visible width.
		visWidth := lipgloss.Width(l)
		if visWidth < leftWidth {
			l += strings.Repeat(" ", leftWidth-visWidth)
		} else if visWidth > leftWidth {
			l = truncateToWidth(l, leftWidth)
		}

		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}

		b.WriteString(l)
		b.WriteString("│")
		b.WriteString(r)
		if i < m.height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// truncateToWidth truncates a styled string to fit within maxWidth
// visible characters. This is a simple approach that uses lipgloss.
func truncateToWidth(s string, maxWidth int) string {
	// Use lipgloss style to truncate.
	style := lipgloss.NewStyle().MaxWidth(maxWidth)
	return style.Render(s)
}

// renderStacked renders the stacked layout (compact header + output).
func (m Model) renderStacked() string {
	header := m.renderStackedHeader()

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	return b.String()
}

// viewportDimensions returns the width and height available for the
// output viewport based on the current layout.
func (m Model) viewportDimensions() (width, height int) {
	if m.layoutMode == LayoutStacked || m.expandedMode {
		// Stacked: 1 header line + 1 separator = 2 chrome lines.
		return m.width, maxInt(1, m.height-2)
	}
	// Split: right panel is 60% width, full height minus header line.
	rightWidth := m.width - m.width*40/100 - 1
	return rightWidth, maxInt(1, m.height-2)
}

// renderOutputContent returns the ring buffer content as a single
// string for the viewport.
func (m Model) renderOutputContent() string {
	lines := m.output.All()
	return strings.Join(lines, "\n")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
