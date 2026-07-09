package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

func newReplayModel() Model {
	m := NewModel(profile.Profile{Name: "replay"}, 100)
	m.isReplay = true
	m.ready = true
	m.width = 120
	m.height = 40
	return m
}

func TestReplayProgressMsg_UpdatesModel(t *testing.T) {
	m := newReplayModel()

	result, _ := m.Update(ReplayProgressMsg{
		Elapsed: 30 * time.Second,
		Total:   60 * time.Second,
	})

	got := result.(Model)
	if got.replayElapsed != 30*time.Second {
		t.Errorf("replayElapsed = %v, want 30s", got.replayElapsed)
	}
	if got.replayTotal != 60*time.Second {
		t.Errorf("replayTotal = %v, want 60s", got.replayTotal)
	}
}

func TestReplayDoneMsg_SetsReplayDone(t *testing.T) {
	m := newReplayModel()

	result, _ := m.Update(ReplayDoneMsg{})

	got := result.(Model)
	if !got.replayDone {
		t.Error("expected replayDone=true after ReplayDoneMsg")
	}
	if got.replayErr != nil {
		t.Errorf("expected nil replayErr, got %v", got.replayErr)
	}
	// Should inject separator into output.
	lines := got.output.All()
	found := false
	for _, l := range lines {
		if l == "── replay complete ──" {
			found = true
		}
	}
	if !found {
		t.Error("expected '── replay complete ──' in output buffer")
	}
}

func TestReplayDoneMsg_WithError(t *testing.T) {
	m := newReplayModel()

	result, _ := m.Update(ReplayDoneMsg{Err: errors.New("file not found")})

	got := result.(Model)
	if !got.replayDone {
		t.Error("expected replayDone=true after ReplayDoneMsg with error")
	}
	if got.replayErr == nil {
		t.Error("expected non-nil replayErr")
	}
	lines := got.output.All()
	found := false
	for _, l := range lines {
		if l == "── replay failed: file not found ──" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error message in output buffer, got: %v", lines)
	}
}

func TestReplayKey_R_IgnoredBeforeDone(t *testing.T) {
	m := newReplayModel()
	// Replay NOT done yet.
	m.replayDone = false

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	got := result.(Model)
	if got.replayAgain {
		t.Error("r key should be ignored when replayDone=false")
	}
	if cmd != nil {
		t.Error("r key should return nil cmd when replayDone=false")
	}
}

func TestReplayKey_R_TriggersQuitWhenDone(t *testing.T) {
	m := newReplayModel()
	m.replayDone = true

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	got := result.(Model)
	if !got.replayAgain {
		t.Error("r key should set replayAgain=true when replayDone=true")
	}
	if cmd == nil {
		t.Error("r key should return a Quit cmd when replayDone=true")
	}
}

func TestReplayKey_R_IgnoredWhenNotReplayMode(t *testing.T) {
	m := NewModel(profile.Profile{Name: "test"}, 100)
	m.isReplay = false
	m.replayDone = true // shouldn't matter
	m.ready = true
	m.width = 120
	m.height = 40

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	got := result.(Model)
	if got.replayAgain {
		t.Error("r key should be ignored when not in replay mode")
	}
	if cmd != nil {
		t.Error("expected nil cmd when not in replay mode")
	}
}

func TestOutputSuppressedAfterReplayDone(t *testing.T) {
	m := newReplayModel()
	m.replayDone = true

	result, _ := m.Update(OutputLineMsg{Line: "should be dropped"})

	got := result.(Model)
	lines := got.output.All()
	for _, l := range lines {
		if l == "should be dropped" {
			t.Error("output should be suppressed after replayDone")
		}
	}
}

func TestOutputAcceptedBeforeReplayDone(t *testing.T) {
	m := newReplayModel()
	m.replayDone = false

	result, _ := m.Update(OutputLineMsg{Line: "hello"})

	got := result.(Model)
	lines := got.output.All()
	found := false
	for _, l := range lines {
		if l == "hello" {
			found = true
		}
	}
	if !found {
		t.Error("output should be accepted before replayDone")
	}
}
