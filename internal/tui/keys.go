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
			m.viewport.LineUp(1)
			m.followMode = false
		}

	case "down":
		if m.ready {
			m.viewport.LineDown(1)
			if m.viewport.AtBottom() {
				m.followMode = true
			}
		}

	case "pgup":
		if m.ready {
			m.viewport.HalfViewUp()
			m.followMode = false
		}

	case "pgdown":
		if m.ready {
			m.viewport.HalfViewDown()
			if m.viewport.AtBottom() {
				m.followMode = true
			}
		}
	}

	return m, nil
}
