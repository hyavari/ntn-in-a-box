package main

import (
	"math"
	"testing"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/tle"
)

func testPasses() []tle.Pass {
	rise := time.Date(2024, 4, 9, 12, 1, 0, 0, time.UTC)
	set := time.Date(2024, 4, 9, 12, 6, 0, 0, time.UTC)
	var samples []tle.ElevSample
	dur := set.Sub(rise)
	for offset := time.Duration(0); offset <= dur; offset += 5 * time.Second {
		t := rise.Add(offset)
		frac := float64(offset) / float64(dur)
		elev := 10 + 50*math.Sin(frac*math.Pi)
		samples = append(samples, tle.ElevSample{T: t, ElevDeg: elev, RangeKm: 1000})
	}
	return []tle.Pass{{
		Satellite: "TEST", NoradID: 1,
		Rise: rise, Set: set, Duration: dur,
		MaxElevDeg: 60, MaxElevTime: rise.Add(dur / 2),
		Samples: samples,
	}}
}

func TestWrapPeerEvals_SyncedNotAdvancer(t *testing.T) {
	passes := testPasses()
	model := tle.DefaultLinkModel()
	start := passes[0].Rise.Add(-time.Minute)

	primary, err := tle.NewSequenceEvaluator(passes, model, tle.SequenceConfig{Speed: 1, StartAt: start})
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	peerSeq, err := tle.NewSequenceEvaluator(passes, model, tle.SequenceConfig{Speed: 60, StartAt: start})
	if err != nil {
		t.Fatalf("peer: %v", err)
	}

	devices := []tleDeviceEval{
		{ID: "sf", Eval: primary},
		{ID: "nyc", Eval: peerSeq},
	}
	wrapPeerEvals(devices, primary)

	if _, ok := devices[0].Eval.(condition.Advancer); !ok {
		t.Fatal("primary must remain Advancer")
	}
	if _, ok := devices[1].Eval.(condition.Advancer); ok {
		t.Fatal("peer SyncedEval must not implement Advancer")
	}
	if _, ok := devices[1].Eval.(condition.LookaheadProvider); !ok {
		t.Fatal("peer must implement LookaheadProvider")
	}
	if !primary.SimTime().Equal(peerSeq.SimTime()) {
		t.Fatalf("shared start: primary %v peer %v", primary.SimTime(), peerSeq.SimTime())
	}
}
