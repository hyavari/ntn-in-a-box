package tui

import tea "github.com/charmbracelet/bubbletea"

// handleKey processes keyboard input and returns the updated model and
// any command.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "r":
		if m.isReplay && m.replayDone {
			m.replayAgain = true
			return m, tea.Quit
		}

	case "f":
		m.followMode = !m.followMode
		if m.followMode && m.ready {
			m.viewport.SetContent(m.renderOutputContent())
			m.viewport.GotoBottom()
		}

	case "tab":
		m.expandedMode = !m.expandedMode
		if m.ready {
			vpWidth, vpHeight := m.viewportDimensions()
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
			m.viewport.SetContent(m.renderOutputContent())
			if m.followMode {
				m.viewport.GotoBottom()
			}
		}

	case "up":
		if m.ready {
			m.viewport.ScrollUp(1)
			m.followMode = false
		}

	case "down":
		if m.ready {
			m.viewport.ScrollDown(1)
			if m.viewport.AtBottom() {
				m.followMode = true
			}
		}

	case "pgup":
		if m.ready {
			m.viewport.HalfPageUp()
			m.followMode = false
		}

	case "pgdown":
		if m.ready {
			m.viewport.HalfPageDown()
			if m.viewport.AtBottom() {
				m.followMode = true
			}
		}

	case "J":
		maxScroll := len(m.messages) - messageVisibleRows
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.messageScroll < maxScroll {
			m.messageScroll++
		}
		if m.messageScroll >= maxScroll {
			m.messageFollow = true
		}

	case "K":
		if m.messageScroll > 0 {
			m.messageScroll--
			m.messageFollow = false
		}

	case "d":
		if next, ok := m.cycleFocus(); ok {
			if m.onFocusChange != nil {
				m.onFocusChange(next)
			}
		}
	}

	return m, nil
}
