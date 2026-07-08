package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func TestLayoutForWidth(t *testing.T) {
	tests := []struct {
		width int
		want  LayoutMode
	}{
		{120, LayoutSplit},
		{100, LayoutSplit},
		{99, LayoutStacked},
		{80, LayoutStacked},
		{40, LayoutStacked},
	}
	for _, tt := range tests {
		got := layoutForWidth(tt.width)
		if got != tt.want {
			t.Errorf("layoutForWidth(%d) = %d, want %d", tt.width, got, tt.want)
		}
	}
}

func TestModel_WindowSizeMsg_SetsLayout(t *testing.T) {
	m := NewModel(profile.Profile{
		Name: "test",
		Schedule: profile.Schedule{
			Mode:      profile.ModePeriodic,
			PeriodSec: 60,
			WindowSec: 30,
		},
	}, 100)

	// Simulate wide terminal.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	if m.layoutMode != LayoutSplit {
		t.Errorf("expected LayoutSplit for width=120, got %d", m.layoutMode)
	}
	if !m.ready {
		t.Error("expected ready=true after WindowSizeMsg")
	}

	// Simulate narrow terminal.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	if m.layoutMode != LayoutStacked {
		t.Errorf("expected LayoutStacked for width=80, got %d", m.layoutMode)
	}
}

func TestModel_WindowSizeMsg_ResizeBackToSplit(t *testing.T) {
	m := NewModel(profile.Profile{
		Name: "test",
		Schedule: profile.Schedule{
			Mode:      profile.ModePeriodic,
			PeriodSec: 60,
			WindowSec: 30,
		},
	}, 100)

	// Start narrow.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	if m.layoutMode != LayoutStacked {
		t.Fatalf("expected LayoutStacked, got %d", m.layoutMode)
	}

	// Resize to wide.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	if m.layoutMode != LayoutSplit {
		t.Errorf("expected LayoutSplit after resize, got %d", m.layoutMode)
	}
}
