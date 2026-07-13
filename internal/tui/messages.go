package tui

import (
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// CoverageMsg wraps a coverage event for the bubbletea update loop.
type CoverageMsg struct {
	Kind                eventbus.CoverageEventKind
	InCoverage          bool
	ElapsedSec          float64
	UntilNextTransition float64
	At                  time.Time
}

// LinkStateMsg wraps a link-state snapshot for the bubbletea update loop.
type LinkStateMsg struct {
	State condition.LinkState
	At    time.Time
}

// OutputLineMsg delivers a single line from the wrapped command.
type OutputLineMsg struct {
	Line string
}

// CmdExitedMsg signals that the wrapped command has exited.
type CmdExitedMsg struct {
	Code int
	Err  error
}

// TickMsg fires every second to update the countdown timer.
type TickMsg struct {
	At time.Time
}

// ReplayProgressMsg reports replay playback progress.
type ReplayProgressMsg struct {
	Elapsed time.Duration
	Total   time.Duration
}

// ReplayDoneMsg signals that the replay has finished (successfully or
// with an error). If Err is non-nil, the replay failed.
type ReplayDoneMsg struct {
	Err error
}

// MessageLifecycleMsg is a store-and-forward status update (no body).
type MessageLifecycleMsg struct {
	ID     string
	From   string
	To     string
	Status string
	At     time.Time
}

// messageRow is one row in the TUI message list.
type messageRow struct {
	ID     string
	From   string
	To     string
	Status string
}
