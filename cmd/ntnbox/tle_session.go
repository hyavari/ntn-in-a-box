package main

import (
	"fmt"
	"os"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/apihost"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/tle"
)

// tleBootstrapOpts configures shared TLE loading for serve/run.
type tleBootstrapOpts struct {
	Path      string
	Sat       string
	LinkModel string
	StartAt   string
	AltKm     float64
	ElevMin   float64
	Speed     float64
	Passes    int
	Observers []ObserverSpec
}

// tleDeviceEval is one observer's live TLE evaluator.
// Eval is what drivers/recorder use: the primary SequenceEvaluator, or a
// SyncedEval for peers so gap acceleration cannot desync sim clocks.
type tleDeviceEval struct {
	ID   string
	Eval condition.Eval
	Obs  tle.Observer
}

// tleBootstrap holds per-device TLE evaluators and shared session metadata.
type tleBootstrap struct {
	Devices      []tleDeviceEval
	Primary      *tle.SequenceEvaluator
	Model        tle.LinkModel
	SatName      string
	LookaheadSec float64
	InitialState condition.LinkState
	Observers    []apihost.ObserverInfo
}

func bootstrapTLE(opts tleBootstrapOpts) (*tleBootstrap, error) {
	if len(opts.Observers) == 0 {
		return nil, fmt.Errorf("tle: at least one observer is required")
	}

	sats, err := tle.ParseFile(opts.Path)
	if err != nil {
		return nil, err
	}
	sat, err := tle.SelectSatellite(sats, opts.Sat)
	if err != nil {
		return nil, err
	}
	if opts.Sat == "" && len(sats) > 1 {
		fmt.Fprintf(os.Stderr, "ntnbox: using satellite %q (NORAD ID %d) — use --sat to select a different one\n",
			sat.Name, sat.NoradID)
	}
	if age, err := tle.Age(sat, time.Now()); err == nil && age > 14*24*time.Hour {
		fmt.Fprintf(os.Stderr, "ntnbox: warning: TLE is %.0f days old (accuracy degrades after ~14 days)\n", age.Hours()/24)
	}

	var model tle.LinkModel
	if opts.LinkModel != "" {
		m, err := tle.LoadLinkModel(opts.LinkModel)
		if err != nil {
			return nil, err
		}
		model = *m
	} else {
		model = tle.DefaultLinkModel()
	}

	passesCount := opts.Passes
	if passesCount <= 0 {
		passesCount = 10
	}
	speed := opts.Speed
	if speed <= 0 {
		speed = 1
	}
	elevMin := opts.ElevMin
	if elevMin == 0 {
		elevMin = 10
	}

	predictCfg := tle.PredictConfig{
		MinElevDeg: elevMin,
		Count:      passesCount,
		MaxSearch:  48 * time.Hour,
	}

	type predicted struct {
		spec   ObserverSpec
		obs    tle.Observer
		passes []tle.Pass
	}
	predictedList := make([]predicted, 0, len(opts.Observers))
	now := time.Now()

	for _, spec := range opts.Observers {
		obs := tle.Observer{LatDeg: spec.LatDeg, LonDeg: spec.LonDeg, AltKm: opts.AltKm}
		fmt.Fprintf(os.Stderr, "ntnbox: predicting passes for %q from %s (%.4f°, %.4f°)...\n",
			sat.Name, spec.ID, spec.LatDeg, spec.LonDeg)

		passes, err := tle.PredictPasses(sat, obs, now, predictCfg)
		if err != nil {
			return nil, fmt.Errorf("predicting passes for %s: %w", spec.ID, err)
		}
		if len(passes) == 0 {
			return nil, fmt.Errorf("no visible passes for %q from %s (%.4f°, %.4f°) in the next 48h — try lowering --elev-min",
				sat.Name, spec.ID, spec.LatDeg, spec.LonDeg)
		}
		predictedList = append(predictedList, predicted{spec: spec, obs: obs, passes: passes})
		fmt.Fprintf(os.Stderr, "ntnbox: %s: %d passes (next: %s, max elev: %.1f°)\n",
			spec.ID, len(passes), passes[0].Rise.Format(time.RFC3339), passes[0].MaxElevDeg)
	}

	// One shared simulation epoch so all observers see the same satellite
	// timeline (globe track + per-pin coverage beams stay consistent).
	var sharedStart time.Time
	switch opts.StartAt {
	case "", "next-pass":
		earliest := predictedList[0].passes[0].Rise
		for _, p := range predictedList[1:] {
			if p.passes[0].Rise.Before(earliest) {
				earliest = p.passes[0].Rise
			}
		}
		sharedStart = earliest.Add(-30 * time.Second)
	default:
		t, err := time.Parse(time.RFC3339, opts.StartAt)
		if err != nil {
			return nil, fmt.Errorf("invalid --start-at time: %w", err)
		}
		sharedStart = t
	}
	if len(predictedList) > 1 {
		fmt.Fprintf(os.Stderr, "ntnbox: shared TLE sim start %s (all observers)\n", sharedStart.Format(time.RFC3339))
	}

	out := &tleBootstrap{
		Model:     model,
		SatName:   sat.Name,
		Observers: make([]apihost.ObserverInfo, 0, len(predictedList)),
	}

	for _, p := range predictedList {
		seqEval, err := tle.NewSequenceEvaluator(p.passes, model, tle.SequenceConfig{
			Speed:        speed,
			StartAt:      sharedStart,
			LookaheadSec: 30,
			Observer:     p.obs,
			Sat:          sat,
		})
		if err != nil {
			return nil, fmt.Errorf("creating TLE evaluator for %s: %w", p.spec.ID, err)
		}

		out.Devices = append(out.Devices, tleDeviceEval{ID: p.spec.ID, Eval: seqEval, Obs: p.obs})
		out.Observers = append(out.Observers, apihost.ObserverInfo{
			ID:     p.spec.ID,
			LatDeg: p.spec.LatDeg,
			LonDeg: p.spec.LonDeg,
		})
		if out.Primary == nil {
			out.Primary = seqEval
			out.LookaheadSec = seqEval.LookaheadSec()
			delay, jitter, loss, bw := model.Interpolate(model.MinElevDeg)
			out.InitialState = condition.LinkState{
				DelayMs:       delay,
				JitterMs:      jitter,
				LossPct:       loss,
				BandwidthKbps: bw,
			}
		}
	}
	wrapPeerEvals(out.Devices, out.Primary)

	if speed > 1 {
		fmt.Fprintf(os.Stderr, "ntnbox: gap acceleration: %.0fx\n", speed)
	}
	return out, nil
}

// wrapPeerEvals replaces peer SequenceEvaluators with SyncedEval so only
// the primary advances the shared simulation clock.
func wrapPeerEvals(devices []tleDeviceEval, primary *tle.SequenceEvaluator) {
	if primary == nil {
		return
	}
	for i := 1; i < len(devices); i++ {
		peer, ok := devices[i].Eval.(*tle.SequenceEvaluator)
		if !ok {
			continue
		}
		devices[i].Eval = tle.NewSyncedEval(primary, peer)
	}
}

func (b *tleBootstrap) sessionInfo() *apihost.SessionInfo {
	if b == nil || b.Primary == nil {
		return nil
	}
	orbitPoints := tle.ComputeOrbitPoints(b.Primary.SatData(), b.Primary.SimTime(), 200)
	primary := b.Observers[0]
	return &apihost.SessionInfo{
		Mode:           "tle",
		SatelliteName:  b.SatName,
		ObserverLatDeg: primary.LatDeg,
		ObserverLonDeg: primary.LonDeg,
		ObserverAltKm:  b.Devices[0].Obs.AltKm,
		OrbitPoints:    orbitPoints,
		Observers:      b.Observers,
	}
}

func boolPtr(v bool) *bool { return &v }
