package tle

import (
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

// SyncedEval evaluates a peer SequenceEvaluator at the master's sim time.
// It does not implement Advancer or Positioner — only the master advances
// the clock and publishes satellite position.
type SyncedEval struct {
	Master *SequenceEvaluator
	Peer   *SequenceEvaluator
}

// NewSyncedEval returns an Eval that mirrors Master.SimTime onto Peer
// before each Evaluate call.
func NewSyncedEval(master, peer *SequenceEvaluator) *SyncedEval {
	return &SyncedEval{Master: master, Peer: peer}
}

// Evaluate returns the peer's link/coverage at Master.SimTime without
// mutating Peer (avoids SetSimTime/Evaluate races across HTTP + driver).
func (s *SyncedEval) Evaluate(_ time.Time) (condition.LinkState, condition.CoverageState) {
	return s.Peer.EvaluateAt(s.Master.SimTime())
}

// Lookahead implements condition.LookaheadProvider for peer devices.
func (s *SyncedEval) Lookahead(_ time.Time) condition.LookaheadState {
	return s.Peer.LookaheadAt(s.Master.SimTime())
}
