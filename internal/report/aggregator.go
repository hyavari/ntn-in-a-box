package report

import (
	"math"
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/voice"
)

// Sampler reports coverage flags at an instant.
type Sampler interface {
	Sample(now time.Time) (inCoverage, inBlockage bool)
}

// LinkSampler optionally provides link impairments for voice estimates.
type LinkSampler interface {
	SampleLink(now time.Time) (link condition.LinkState, inCoverage, inBlockage bool)
}

// EvalSampler adapts condition.Eval to Sampler and LinkSampler.
type EvalSampler struct {
	Eval condition.Eval
}

// Sample implements Sampler.
func (s EvalSampler) Sample(now time.Time) (bool, bool) {
	_, cov := s.Eval.Evaluate(now)
	return cov.InCoverage, cov.InBlockage
}

// SampleLink implements LinkSampler.
func (s EvalSampler) SampleLink(now time.Time) (condition.LinkState, bool, bool) {
	link, cov := s.Eval.Evaluate(now)
	return link, cov.InCoverage, cov.InBlockage
}

type covBucket int

const (
	bucketIn covBucket = iota
	bucketBlocked
	bucketOut
)

// Aggregator accumulates coverage, messaging, and voice stats for one run.
type Aggregator struct {
	mu sync.Mutex

	profile      string
	deviceID     string // if non-empty, ignore coverage events for other devices
	sampler      Sampler
	linkSampler  LinkSampler
	voiceCapable bool
	startedAt    time.Time

	bucket       covBucket
	segmentStart time.Time
	inSec        float64
	blockedSec   float64
	outSec       float64
	opens        int
	closes       int
	finalized    bool

	msgStatus  map[string]string
	callStatus map[string]string

	// Voice estimates: exact running means over all samples; percentiles from
	// a fixed ring (last voiceRingCap in-coverage samples).
	voiceCount  int
	voiceSumMOS float64
	voiceSumJB  float64
	voiceSumPLC float64
	voiceRing   []voice.Sample
	voiceRingI  int // next write index
	voiceRingN  int // how many slots filled (≤ cap)

	unsubCov  func()
	unsubMsg  func()
	unsubCall func()
	stopTick  chan struct{}
	tickDone  chan struct{}
}

// voiceRingCap bounds percentile sample memory (~1h at 1 Hz).
const voiceRingCap = 3600

// Config wires a new Aggregator.
type Config struct {
	Bus          *eventbus.Bus
	Sampler      Sampler
	Profile      string
	DeviceID     string // primary device filter; empty = accept all
	Start        time.Time
	TickEvery    time.Duration // default 1s; 0 → 1s; negative disables ticker (tests)
	VoiceCapable bool
}

// New starts subscriptions and an optional ticker. Call Close then Finalize.
func New(cfg Config) *Aggregator {
	start := cfg.Start
	if start.IsZero() {
		start = time.Now().UTC()
	}
	tickEvery := cfg.TickEvery
	if tickEvery == 0 {
		tickEvery = time.Second
	}

	a := &Aggregator{
		profile:      cfg.Profile,
		deviceID:     cfg.DeviceID,
		sampler:      cfg.Sampler,
		voiceCapable: cfg.VoiceCapable,
		startedAt:    start,
		segmentStart: start,
		msgStatus:    make(map[string]string),
		callStatus:   make(map[string]string),
	}
	if ls, ok := cfg.Sampler.(LinkSampler); ok {
		a.linkSampler = ls
	}
	if cfg.Sampler != nil {
		a.bucket = classify(cfg.Sampler.Sample(start))
	} else {
		a.bucket = bucketOut
	}

	if cfg.Bus != nil {
		a.unsubCov = cfg.Bus.SubscribeCoverage(a.onCoverage)
		a.unsubMsg = cfg.Bus.SubscribeMessage(a.onMessage)
		a.unsubCall = cfg.Bus.SubscribeCall(a.onCall)
	}

	if tickEvery > 0 && cfg.Sampler != nil {
		a.stopTick = make(chan struct{})
		a.tickDone = make(chan struct{})
		go a.tickLoop(tickEvery)
	}
	return a
}

// Close stops the ticker and unsubscribes from the bus. Safe to call twice.
func (a *Aggregator) Close() {
	a.mu.Lock()
	stop := a.stopTick
	a.stopTick = nil
	unsubCov := a.unsubCov
	unsubMsg := a.unsubMsg
	unsubCall := a.unsubCall
	a.unsubCov = nil
	a.unsubMsg = nil
	a.unsubCall = nil
	a.mu.Unlock()

	if stop != nil {
		close(stop)
		<-a.tickDone
	}
	if unsubCov != nil {
		unsubCov()
	}
	if unsubMsg != nil {
		unsubMsg()
	}
	if unsubCall != nil {
		unsubCall()
	}
}

// Finalize closes the last coverage segment and returns the report.
// Subsequent Finalize calls return the same snapshot (idempotent).
func (a *Aggregator) Finalize(end time.Time) Report {
	a.Close()

	a.mu.Lock()
	defer a.mu.Unlock()

	if end.IsZero() {
		end = time.Now().UTC()
	}
	if !a.finalized {
		a.rollLocked(end)
		a.finalized = true
	}

	dur := end.Sub(a.startedAt).Seconds()
	if dur < 0 {
		dur = 0
	}
	r := Report{
		StartedAt:   a.startedAt,
		EndedAt:     end,
		DurationSec: dur,
		Profile:     a.profile,
		Coverage: CoverageStats{
			InSec:      a.inSec,
			BlockedSec: a.blockedSec,
			OutSec:     a.outSec,
			Opens:      a.opens,
			Closes:     a.closes,
		},
		Messaging: a.messagingLocked(),
		Voice:     a.voiceLocked(),
	}
	if dur > 0 {
		r.Coverage.InPct = 100 * a.inSec / dur
		r.Coverage.BlockedPct = 100 * a.blockedSec / dur
		r.Coverage.OutPct = 100 * a.outSec / dur
	}
	return r
}

func (a *Aggregator) tickLoop(every time.Duration) {
	defer close(a.tickDone)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-a.stopTick:
			return
		case now := <-t.C:
			a.sampleNow(now.UTC())
		}
	}
}

func (a *Aggregator) onCoverage(ev eventbus.CoverageEvent) {
	if a.deviceID != "" && ev.DeviceID != "" && ev.DeviceID != a.deviceID {
		return
	}
	switch ev.Kind {
	case eventbus.KindWindowOpened, eventbus.KindWindowClosed:
		// Driver emits window_* on any InCoverage flip, including blockage
		// enter/exit. Count opens/closes only for scheduled in↔out (not
		// in↔blocked). Always re-sample so time buckets stay accurate.
		a.noteTransition(ev.Kind, ev.At)
	default:
		// Lookahead notices do not change wall-clock buckets.
	}
}

func (a *Aggregator) noteTransition(kind eventbus.CoverageEventKind, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finalized {
		return
	}
	prev := a.bucket
	if a.sampler != nil {
		if at.Before(a.segmentStart) {
			at = a.segmentStart
		}
		next := classify(a.sampler.Sample(at))
		if next != prev {
			a.accrueLocked(at)
			a.bucket = next
			a.segmentStart = at
		}
	}
	switch kind {
	case eventbus.KindWindowOpened:
		// Scheduled open (incl. initial in-coverage announce). Skip
		// blockage clear (blocked → in).
		if a.bucket == bucketIn && prev != bucketBlocked {
			a.opens++
		}
	case eventbus.KindWindowClosed:
		// Scheduled close: in → out. Skip in → blocked.
		if prev == bucketIn && a.bucket == bucketOut {
			a.closes++
		}
	}
}

func (a *Aggregator) onMessage(ev eventbus.MessageEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finalized {
		return
	}
	if ev.ID == "" {
		return
	}
	a.msgStatus[ev.ID] = ev.Status
}

func (a *Aggregator) onCall(ev eventbus.CallEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finalized {
		return
	}
	if a.deviceID != "" && ev.DeviceID != "" && ev.DeviceID != a.deviceID {
		return
	}
	if ev.ID == "" {
		return
	}
	a.callStatus[ev.ID] = ev.Status
}

func (a *Aggregator) sampleNow(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finalized || a.sampler == nil {
		return
	}
	if now.Before(a.segmentStart) {
		now = a.segmentStart
	}
	next := classify(a.sampler.Sample(now))
	if next != a.bucket {
		a.accrueLocked(now)
		a.bucket = next
		a.segmentStart = now
	}
	a.maybeVoiceSampleLocked(now)
}

func (a *Aggregator) maybeVoiceSampleLocked(now time.Time) {
	if !a.voiceCapable || a.linkSampler == nil {
		return
	}
	link, inCov, _ := a.linkSampler.SampleLink(now)
	if !inCov {
		return
	}
	if math.IsNaN(link.DelayMs) || math.IsNaN(link.JitterMs) || math.IsNaN(link.LossPct) ||
		math.IsInf(link.DelayMs, 0) || math.IsInf(link.JitterMs, 0) || math.IsInf(link.LossPct, 0) {
		return
	}
	s := voice.Estimate(link.DelayMs, link.JitterMs, link.LossPct)
	a.voiceCount++
	a.voiceSumMOS += s.MOS
	a.voiceSumJB += s.JitterBufferStress
	a.voiceSumPLC += s.PLCPressure
	if a.voiceRing == nil {
		a.voiceRing = make([]voice.Sample, voiceRingCap)
	}
	a.voiceRing[a.voiceRingI] = s
	a.voiceRingI = (a.voiceRingI + 1) % voiceRingCap
	if a.voiceRingN < voiceRingCap {
		a.voiceRingN++
	}
}

func (a *Aggregator) rollLocked(now time.Time) {
	if now.Before(a.segmentStart) {
		now = a.segmentStart
	}
	a.accrueLocked(now)
	a.segmentStart = now
}

func (a *Aggregator) accrueLocked(now time.Time) {
	sec := now.Sub(a.segmentStart).Seconds()
	if sec <= 0 {
		return
	}
	switch a.bucket {
	case bucketIn:
		a.inSec += sec
	case bucketBlocked:
		a.blockedSec += sec
	case bucketOut:
		a.outSec += sec
	}
}

func (a *Aggregator) messagingLocked() MessagingStats {
	if len(a.msgStatus) == 0 {
		return MessagingStats{Present: false}
	}
	var delivered, failed, open int
	for _, st := range a.msgStatus {
		switch st {
		case "delivered":
			delivered++
		case "failed":
			failed++
		default:
			open++
		}
	}
	unique := len(a.msgStatus)
	return MessagingStats{
		Present:      true,
		Unique:       unique,
		Delivered:    delivered,
		Failed:       failed,
		Open:         open,
		DeliveryRate: float64(delivered) / float64(unique),
	}
}

func (a *Aggregator) voiceLocked() VoiceStats {
	vs := VoiceStats{
		Capable: a.voiceCapable,
		Calls:   a.callsLocked(),
	}
	if !a.voiceCapable || a.voiceCount == 0 {
		return vs
	}
	n := float64(a.voiceCount)
	pct := voice.Aggregate(a.voiceRingSamples())
	vs.Estimates = VoiceEstimates{
		MouthToEarMsP50:       roundFloat(pct.MouthToEarMsP50, 1),
		MouthToEarMsP95:       roundFloat(pct.MouthToEarMsP95, 1),
		MOSAvg:                roundFloat(a.voiceSumMOS/n, 2),
		JitterBufferStressAvg: roundFloat(a.voiceSumJB/n, 3),
		PLCPressureAvg:        roundFloat(a.voiceSumPLC/n, 3),
		InCoverageSampleCount: a.voiceCount,
	}
	return vs
}

func (a *Aggregator) voiceRingSamples() []voice.Sample {
	if a.voiceRingN == 0 {
		return nil
	}
	out := make([]voice.Sample, a.voiceRingN)
	if a.voiceRingN < voiceRingCap {
		copy(out, a.voiceRing[:a.voiceRingN])
		return out
	}
	// Ring is full: voiceRingI is the oldest slot.
	n := copy(out, a.voiceRing[a.voiceRingI:])
	copy(out[n:], a.voiceRing[:a.voiceRingI])
	return out
}

func (a *Aggregator) callsLocked() CallStats {
	if len(a.callStatus) == 0 {
		return CallStats{Present: false}
	}
	var completed, dropped, open int
	for _, st := range a.callStatus {
		switch st {
		case "completed":
			completed++
		case "dropped":
			dropped++
		default:
			open++ // started or unknown in-flight
		}
	}
	attempted := len(a.callStatus)
	return CallStats{
		Present:         true,
		Attempted:       attempted,
		Completed:       completed,
		Dropped:         dropped,
		Open:            open,
		CompletionRate:  roundFloat(float64(completed)/float64(attempted), 3),
		DropOnCloseRate: roundFloat(float64(dropped)/float64(attempted), 3),
	}
}

func roundFloat(x float64, places int) float64 {
	if places < 0 {
		return x
	}
	pow := math.Pow(10, float64(places))
	return math.Round(x*pow) / pow
}

func classify(inCoverage, inBlockage bool) covBucket {
	if inCoverage {
		return bucketIn
	}
	if inBlockage {
		return bucketBlocked
	}
	return bucketOut
}
