package tle

import (
	"fmt"

	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// GenerateOpts controls profile generation from a pass.
type GenerateOpts struct {
	LookaheadSec float64 // Lookahead for coverage transition notice (default: 30)
	GapSec       float64 // Gap after this pass in seconds (for PeriodSec)
	Index        int     // Pass number (for naming)
	SatName      string  // Satellite name (for naming)
}

// GenerateProfile converts a Pass + LinkModel into a profile.Profile.
// Each ElevSample in the pass is mapped through the link model to
// produce the impairment curves.
func GenerateProfile(pass Pass, model LinkModel, opts GenerateOpts) (*profile.Profile, error) {
	if len(pass.Samples) < 2 {
		return nil, fmt.Errorf("tle: pass has too few samples (%d), need at least 2", len(pass.Samples))
	}

	lookahead := opts.LookaheadSec
	if lookahead == 0 {
		lookahead = 30
	}

	windowSec := pass.Duration.Seconds()
	periodSec := windowSec + opts.GapSec
	if opts.GapSec == 0 {
		// Single pass with no gap: period = window, no lookahead possible.
		periodSec = windowSec
		lookahead = 0
	}

	// Build curves from elevation samples.
	delayPts := make([]profile.Point, 0, len(pass.Samples))
	jitterPts := make([]profile.Point, 0, len(pass.Samples))
	lossPts := make([]profile.Point, 0, len(pass.Samples))
	bwPts := make([]profile.Point, 0, len(pass.Samples))

	for _, s := range pass.Samples {
		offsetSec := s.T.Sub(pass.Rise).Seconds()
		delay, jitter, loss, bw := model.Interpolate(s.ElevDeg)

		delayPts = append(delayPts, profile.Point{OffsetSec: offsetSec, Value: delay})
		jitterPts = append(jitterPts, profile.Point{OffsetSec: offsetSec, Value: jitter})
		lossPts = append(lossPts, profile.Point{OffsetSec: offsetSec, Value: loss})
		bwPts = append(bwPts, profile.Point{OffsetSec: offsetSec, Value: bw})
	}

	name := fmt.Sprintf("tle_%s_pass%d", sanitizeName(opts.SatName), opts.Index+1)

	p := &profile.Profile{
		Name: name,
		Description: fmt.Sprintf(
			"TLE-generated profile for %s pass %d (max elevation %.1f°, duration %s)",
			opts.SatName, opts.Index+1, pass.MaxElevDeg, pass.Duration.Round(1e9), // round to seconds
		),
		Schedule: profile.Schedule{
			Mode:         profile.ModePeriodic,
			PeriodSec:    periodSec,
			WindowSec:    windowSec,
			LookaheadSec: lookahead,
		},
		Curves: profile.Curves{
			DelayMs:       delayPts,
			JitterMs:      jitterPts,
			LossPct:       lossPts,
			BandwidthKbps: bwPts,
		},
	}

	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("tle: generated profile failed validation: %w", err)
	}

	return p, nil
}

// sanitizeName converts a satellite name to a filesystem/profile-safe
// string (lowercase, spaces to underscores, strip special chars).
func sanitizeName(name string) string {
	if name == "" {
		return "unknown"
	}
	var out []byte
	for _, c := range []byte(name) {
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32) // tolower
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c == ' ' || c == '-' || c == '_':
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}
