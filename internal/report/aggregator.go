package report

import (
	"sync"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// Sampler reports coverage flags at an instant.
type Sampler interface {
	Sample(now time.Time) (inCoverage, inBlockage bool)
}

// EvalSampler adapts condition.Eval to Sampler.
type EvalSampler struct {
	Eval condition.Eval
}

// Sample implements Sampler.
func (s EvalSampler) Sample(now time.Time) (bool, bool) {
	_, cov := s.Eval.Evaluate(now)
	return cov.InCoverage, cov.InBlockage
}

type covBucket int

const (
	bucketIn covBucket = iota
	bucketBlocked
	bucketOut
)

// Aggregator accumulates coverage and messaging stats for one run.
type Aggregator struct {
	mu sync.Mutex

	profile   string
	deviceID  string // if non-empty, ignore coverage events for other devices
	sampler   Sampler
	startedAt time.Time

	bucket        covBucket
	segmentStart  time.Time
	inSec         float64
	blockedSec    float64
	outSec        float64
	opens         int
	closes        int
	finalized     bool

	msgStatus map[string]string

	unsubCov  func()
	unsubMsg  func()
	stopTick  chan struct{}
	tickDone  chan struct{}
}

// Config wires a new Aggregator.
type Config struct {
	Bus       *eventbus.Bus
	Sampler   Sampler
	Profile   string
	DeviceID  string // primary device filter; empty = accept all
	Start     time.Time
	TickEvery time.Duration // default 1s; 0 → 1s; negative disables ticker (tests)
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
		startedAt:    start,
		segmentStart: start,
		msgStatus:    make(map[string]string),
	}
	if cfg.Sampler != nil {
		a.bucket = classify(cfg.Sampler.Sample(start))
	} else {
		a.bucket = bucketOut
	}

	if cfg.Bus != nil {
		a.unsubCov = cfg.Bus.SubscribeCoverage(a.onCoverage)
		a.unsubMsg = cfg.Bus.SubscribeMessage(a.onMessage)
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
	a.unsubCov = nil
	a.unsubMsg = nil
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
	if next == a.bucket {
		return
	}
	a.accrueLocked(now)
	a.bucket = next
	a.segmentStart = now
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

func classify(inCoverage, inBlockage bool) covBucket {
	if inCoverage {
		return bucketIn
	}
	if inBlockage {
		return bucketBlocked
	}
	return bucketOut
}
