package tle

import (
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
)

func TestSyncedEval_FollowsMasterUnderSpeed(t *testing.T) {
	passes := syntheticPasses()
	model := DefaultLinkModel()
	start := passes[0].Rise.Add(-2 * time.Minute) // start in a gap

	master, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:   60,
		StartAt: start,
	})
	if err != nil {
		t.Fatalf("master: %v", err)
	}
	peer, err := NewSequenceEvaluator(passes, model, SequenceConfig{
		Speed:   60,
		StartAt: start,
	})
	if err != nil {
		t.Fatalf("peer: %v", err)
	}

	synced := NewSyncedEval(master, peer)
	if _, ok := any(synced).(condition.Advancer); ok {
		t.Fatal("SyncedEval must not implement Advancer")
	}
	if _, ok := any(synced).(condition.Positioner); ok {
		t.Fatal("SyncedEval must not implement Positioner")
	}
	if _, ok := any(synced).(condition.LookaheadProvider); !ok {
		t.Fatal("SyncedEval must implement LookaheadProvider")
	}

	wall := time.Now()
	master.Advance(wall)
	// 2s wall at 60x gap speed → +120s sim (still before rise, or near it).
	master.Advance(wall.Add(2 * time.Second))

	_, cov := synced.Evaluate(time.Time{})
	_, wantCov := master.Evaluate(time.Time{})
	if cov.InCoverage != wantCov.InCoverage {
		t.Fatalf("peer InCoverage=%v, master=%v", cov.InCoverage, wantCov.InCoverage)
	}
	// EvaluateAt must not mutate peer's own clock.
	if !peer.SimTime().Equal(start) {
		t.Fatalf("peer SimTime mutated: got %v want %v", peer.SimTime(), start)
	}

	master.Advance(wall.Add(4 * time.Second))
	la := synced.Lookahead(time.Time{})
	laMaster := master.Lookahead(time.Time{})
	if la.InCoverage != laMaster.InCoverage {
		t.Fatalf("lookahead InCoverage peer=%v master=%v", la.InCoverage, laMaster.InCoverage)
	}
}
