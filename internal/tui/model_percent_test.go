package tui

import (
	"testing"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// TestComputeCoveragePercent_BlockageKeepsCycleProgress verifies that a
// continuous profile's progress bar tracks the schedule cycle through a
// blockage (no reset to 0%, no spike to ~100%) and resumes cleanly after.
func TestComputeCoveragePercent_BlockageKeepsCycleProgress(t *testing.T) {
	p := profile.Profile{
		Name:      "t",
		Schedule:  profile.Schedule{Mode: profile.ModeContinuous, PeriodSec: 300},
		Blockages: []profile.Blockage{{OffsetSec: 60, DurationSec: 20}},
	}
	m := NewModel(p, 10)

	// In coverage 60s into the 300s cycle → ~20%.
	upd, _ := m.Update(CoverageMsg{
		InCoverage: true, ElapsedSec: 60, UntilNextTransition: 240, CyclePosSec: 60,
	})
	m = upd.(Model)
	if got := m.coveragePercent; got < 19 || got > 21 {
		t.Fatalf("in-coverage percent = %.1f, want ~20", got)
	}

	// Blockage starts at the same cycle position — bar must stay ~20%,
	// not reset to 0% or jump toward 100%.
	upd, _ = m.Update(CoverageMsg{
		InCoverage: false, ElapsedSec: 0, UntilNextTransition: 20, CyclePosSec: 60,
	})
	m = upd.(Model)
	if got := m.coveragePercent; got < 19 || got > 21 {
		t.Fatalf("blockage-start percent = %.1f, want ~20 (must keep cycle progress)", got)
	}

	// After 10 ticks the cycle has advanced; bar ~23%, still out of coverage.
	for i := 0; i < 10; i++ {
		upd, _ = m.Update(TickMsg{})
		m = upd.(Model)
	}
	if m.inCoverage {
		t.Fatal("expected still out of coverage during blockage")
	}
	if got := m.coveragePercent; got < 22 || got > 25 {
		t.Fatalf("mid-blockage percent = %.1f, want ~23", got)
	}

	// Recovery: sync to cycle pos 80 → ~27%.
	upd, _ = m.Update(CoverageMsg{
		InCoverage: true, ElapsedSec: 80, UntilNextTransition: 220, CyclePosSec: 80,
	})
	m = upd.(Model)
	if !m.inCoverage {
		t.Fatal("expected back in coverage")
	}
	if got := m.coveragePercent; got < 26 || got > 28 {
		t.Fatalf("post-blockage percent = %.1f, want ~27", got)
	}
}

// TestTick_ContinuousCountdownFollowsCycle ensures "Xs left" stays aligned
// with the cycle bar across a wrap — it must not stick at 0s while the
// percent keeps climbing.
func TestTick_ContinuousCountdownFollowsCycle(t *testing.T) {
	p := profile.Profile{
		Name:     "t",
		Schedule: profile.Schedule{Mode: profile.ModeContinuous, PeriodSec: 30},
	}
	m := NewModel(p, 10)
	upd, _ := m.Update(CoverageMsg{
		InCoverage: true, ElapsedSec: 28, UntilNextTransition: 2, CyclePosSec: 28,
	})
	m = upd.(Model)

	upd, _ = m.Update(TickMsg{}) // 29 → 1s left
	m = upd.(Model)
	if m.remainingSec < 0.5 || m.remainingSec > 1.5 {
		t.Fatalf("remaining at pos 29 = %.1f, want ~1", m.remainingSec)
	}

	upd, _ = m.Update(TickMsg{}) // wraps to 0 → 30s left
	m = upd.(Model)
	if m.cyclePosSec > 0.5 {
		t.Fatalf("cyclePos after wrap = %.1f, want ~0", m.cyclePosSec)
	}
	if m.remainingSec < 29.5 || m.remainingSec > 30.5 {
		t.Fatalf("remaining after wrap = %.1f, want ~30 (must not stick at 0)", m.remainingSec)
	}
	if got := m.coveragePercent; got > 2 {
		t.Fatalf("percent after wrap = %.1f, want ~0", got)
	}
}
