package tle

import (
	"fmt"
	"math"
	"time"

	satellite "github.com/joshuaferrara/go-satellite"
)

// Observer is a ground location for pass prediction.
type Observer struct {
	LatDeg float64 // Latitude in degrees, north positive
	LonDeg float64 // Longitude in degrees, east positive
	AltKm  float64 // Altitude above sea level in km (default 0)
}

// Pass represents one satellite visibility window above the observer.
type Pass struct {
	Satellite   string        // Satellite name
	NoradID     int           // NORAD catalog ID
	Rise        time.Time     // When elevation crosses above minimum
	Set         time.Time     // When elevation crosses below minimum
	Duration    time.Duration // Set - Rise
	MaxElevDeg  float64       // Peak elevation angle during pass
	MaxElevTime time.Time     // Time of peak elevation
	Samples     []ElevSample  // Elevation at regular intervals across the pass
}

// ElevSample is one elevation measurement within a pass.
type ElevSample struct {
	T       time.Time
	ElevDeg float64
	RangeKm float64 // Slant range to satellite
}

// PredictConfig controls pass prediction behavior.
type PredictConfig struct {
	MinElevDeg     float64       // Minimum elevation to consider visible (default: 10)
	Count          int           // Number of passes to find (default: 10)
	MaxSearch      time.Duration // Maximum time window to search (default: 48h)
	SampleInterval time.Duration // Elevation sample interval within a pass (default: 5s)
}

func (c *PredictConfig) withDefaults() PredictConfig {
	out := *c
	if out.MinElevDeg < 0 {
		out.MinElevDeg = 0
	}
	if out.Count == 0 {
		out.Count = 10
	}
	if out.MaxSearch == 0 {
		out.MaxSearch = 48 * time.Hour
	}
	if out.SampleInterval == 0 {
		out.SampleInterval = 5 * time.Second
	}
	return out
}

// PredictPasses finds the next N passes of sat visible from obs,
// starting at startTime. It steps through time using SGP4 propagation
// to find when the satellite rises above and sets below the minimum
// elevation angle.
func PredictPasses(sat Sat, obs Observer, startTime time.Time, cfg PredictConfig) ([]Pass, error) {
	cfg = cfg.withDefaults()

	// Initialize the SGP4 propagator.
	sgp4Sat := satellite.TLEToSat(sat.Line1, sat.Line2, satellite.GravityWGS84)

	// Observer coordinates in radians for the go-satellite library.
	obsLL := satellite.LatLong{
		Latitude:  obs.LatDeg * math.Pi / 180.0,
		Longitude: obs.LonDeg * math.Pi / 180.0,
	}

	endTime := startTime.Add(cfg.MaxSearch)
	coarseStep := 30 * time.Second // Coarse scanning step
	fineStep := 1 * time.Second    // Binary search precision

	var passes []Pass
	t := startTime

	// Check if satellite is already visible at start time.
	initialElev, _ := elevationAt(sgp4Sat, obsLL, obs.AltKm, t)
	if initialElev >= cfg.MinElevDeg {
		// Already above minimum elevation — find the set time from here.
		setTime, found := findSet(sgp4Sat, obsLL, obs.AltKm, cfg.MinElevDeg, t, endTime, coarseStep, fineStep)
		if found {
			duration := setTime.Sub(t)
			if duration >= 10*time.Second {
				samples, maxElev, maxElevTime := samplePass(sgp4Sat, obsLL, obs.AltKm, t, setTime, cfg.SampleInterval)
				passes = append(passes, Pass{
					Satellite:   sat.Name,
					NoradID:     sat.NoradID,
					Rise:        t,
					Set:         setTime,
					Duration:    duration,
					MaxElevDeg:  maxElev,
					MaxElevTime: maxElevTime,
					Samples:     samples,
				})
			}
			t = setTime.Add(coarseStep)
		}
	}

	for t.Before(endTime) && len(passes) < cfg.Count {
		// Scan for a rise event (elevation going above minimum).
		riseTime, found := findRise(sgp4Sat, obsLL, obs.AltKm, cfg.MinElevDeg, t, endTime, coarseStep, fineStep)
		if !found {
			break
		}

		// Scan for the set event (elevation going below minimum).
		setTime, found := findSet(sgp4Sat, obsLL, obs.AltKm, cfg.MinElevDeg, riseTime, endTime, coarseStep, fineStep)
		if !found {
			break
		}

		duration := setTime.Sub(riseTime)

		// Skip very short passes (< 10 seconds).
		if duration < 10*time.Second {
			t = setTime.Add(coarseStep)
			continue
		}

		// Sample the pass.
		samples, maxElev, maxElevTime := samplePass(sgp4Sat, obsLL, obs.AltKm, riseTime, setTime, cfg.SampleInterval)

		passes = append(passes, Pass{
			Satellite:   sat.Name,
			NoradID:     sat.NoradID,
			Rise:        riseTime,
			Set:         setTime,
			Duration:    duration,
			MaxElevDeg:  maxElev,
			MaxElevTime: maxElevTime,
			Samples:     samples,
		})

		// Move past this pass.
		t = setTime.Add(coarseStep)
	}

	return passes, nil
}

// Sat is a type alias used in PredictPasses to avoid confusion with
// the go-satellite library's Satellite type.
type Sat = Satellite

// elevationAt computes the elevation angle (in degrees) of the
// satellite as seen from the observer at time t.
func elevationAt(sgp4Sat satellite.Satellite, obsLL satellite.LatLong, obsAltKm float64, t time.Time) (elevDeg, rangeKm float64) {
	t = t.UTC()
	year, month, day := t.Date()
	hour, min, sec := t.Clock()

	pos, _ := satellite.Propagate(sgp4Sat, year, int(month), day, hour, min, sec)

	// Check for propagation failure (zero vector).
	if pos.X == 0 && pos.Y == 0 && pos.Z == 0 {
		return -90, 0
	}

	jday := satellite.JDay(year, int(month), day, hour, min, sec)
	lookAngles := satellite.ECIToLookAngles(pos, obsLL, obsAltKm, jday)

	elevDeg = lookAngles.El * 180.0 / math.Pi
	rangeKm = lookAngles.Rg
	return elevDeg, rangeKm
}

// findRise scans forward from start to find when the satellite first
// rises above minElev. Returns the refined rise time.
func findRise(sgp4Sat satellite.Satellite, obsLL satellite.LatLong, obsAltKm, minElev float64, start, end time.Time, coarseStep, fineStep time.Duration) (time.Time, bool) {
	prev := start
	prevElev, _ := elevationAt(sgp4Sat, obsLL, obsAltKm, prev)

	t := start.Add(coarseStep)
	for t.Before(end) || t.Equal(end) {
		elev, _ := elevationAt(sgp4Sat, obsLL, obsAltKm, t)

		if prevElev < minElev && elev >= minElev {
			// Refine with binary search.
			lo, hi := prev, t
			for hi.Sub(lo) > fineStep {
				mid := lo.Add(hi.Sub(lo) / 2)
				midElev, _ := elevationAt(sgp4Sat, obsLL, obsAltKm, mid)
				if midElev < minElev {
					lo = mid
				} else {
					hi = mid
				}
			}
			return hi, true
		}

		prev = t
		prevElev = elev
		t = t.Add(coarseStep)
	}

	return time.Time{}, false
}

// findSet scans forward from riseTime to find when the satellite sets
// below minElev. Returns the refined set time.
func findSet(sgp4Sat satellite.Satellite, obsLL satellite.LatLong, obsAltKm, minElev float64, riseTime, end time.Time, coarseStep, fineStep time.Duration) (time.Time, bool) {
	prev := riseTime
	prevElev, _ := elevationAt(sgp4Sat, obsLL, obsAltKm, prev)

	t := riseTime.Add(coarseStep)
	for t.Before(end) || t.Equal(end) {
		elev, _ := elevationAt(sgp4Sat, obsLL, obsAltKm, t)

		if prevElev >= minElev && elev < minElev {
			// Refine with binary search.
			lo, hi := prev, t
			for hi.Sub(lo) > fineStep {
				mid := lo.Add(hi.Sub(lo) / 2)
				midElev, _ := elevationAt(sgp4Sat, obsLL, obsAltKm, mid)
				if midElev >= minElev {
					lo = mid
				} else {
					hi = mid
				}
			}
			return lo, true
		}

		prev = t
		prevElev = elev
		t = t.Add(coarseStep)
	}

	// If we never found the set within the window, use end.
	// This handles the edge case where a pass extends beyond MaxSearch.
	if prevElev >= minElev {
		return end, true
	}

	return time.Time{}, false
}

// samplePass samples elevation at regular intervals across the pass,
// tracking the maximum elevation point.
func samplePass(sgp4Sat satellite.Satellite, obsLL satellite.LatLong, obsAltKm float64, rise, set time.Time, interval time.Duration) ([]ElevSample, float64, time.Time) {
	var samples []ElevSample
	var maxElev float64
	var maxElevTime time.Time

	t := rise
	for t.Before(set) || t.Equal(set) {
		elev, rng := elevationAt(sgp4Sat, obsLL, obsAltKm, t)
		samples = append(samples, ElevSample{
			T:       t,
			ElevDeg: elev,
			RangeKm: rng,
		})
		if elev > maxElev {
			maxElev = elev
			maxElevTime = t
		}
		t = t.Add(interval)
	}

	// Always include the final set point if not already included.
	if len(samples) > 0 && samples[len(samples)-1].T.Before(set) {
		elev, rng := elevationAt(sgp4Sat, obsLL, obsAltKm, set)
		samples = append(samples, ElevSample{
			T:       set,
			ElevDeg: elev,
			RangeKm: rng,
		})
	}

	return samples, maxElev, maxElevTime
}

// TLEAge returns how old the TLE is relative to a reference time.
// Useful for warning users about stale TLE data.
func TLEAge(sat Satellite, refTime time.Time) (time.Duration, error) {
	tleTime, err := tleEpoch(sat.Line1)
	if err != nil {
		return 0, err
	}
	return refTime.Sub(tleTime), nil
}

// tleEpoch extracts the epoch time from TLE line 1.
// Line 1 columns 19-20: 2-digit year, columns 21-32: day of year (fractional).
func tleEpoch(line1 string) (time.Time, error) {
	if len(line1) < 32 {
		return time.Time{}, fmt.Errorf("tle: line 1 too short to extract epoch")
	}

	epochStr := line1[18:32]
	// First two chars are the year.
	yearStr := epochStr[0:2]
	dayStr := epochStr[2:]

	var year int
	if _, err := fmt.Sscanf(yearStr, "%d", &year); err != nil {
		return time.Time{}, fmt.Errorf("tle: invalid epoch year: %q", yearStr)
	}

	// Two-digit year: 57-99 → 1957-1999, 00-56 → 2000-2056.
	if year >= 57 {
		year += 1900
	} else {
		year += 2000
	}

	var dayOfYear float64
	if _, err := fmt.Sscanf(dayStr, "%f", &dayOfYear); err != nil {
		return time.Time{}, fmt.Errorf("tle: invalid epoch day: %q", dayStr)
	}

	// Convert year + fractional day to time.Time.
	jan1 := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	epochTime := jan1.Add(time.Duration((dayOfYear - 1) * 24 * float64(time.Hour)))
	return epochTime, nil
}
