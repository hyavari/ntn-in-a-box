package main

import (
	"fmt"
	"strconv"
	"strings"
)

// stringList is a repeatable flag.Value (e.g. --observer a=1,2 --observer b=3,4).
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// ObserverSpec is a named ground observer for TLE multi-device sessions.
type ObserverSpec struct {
	ID     string
	LatDeg float64
	LonDeg float64
}

// parseObserverFlag parses "id=lat,lon" (e.g. sandbox-0=37.7749,-122.4194).
func parseObserverFlag(s string) (ObserverSpec, error) {
	eq := strings.IndexByte(s, '=')
	if eq <= 0 || eq == len(s)-1 {
		return ObserverSpec{}, fmt.Errorf("invalid --observer %q (want id=lat,lon)", s)
	}
	id := strings.TrimSpace(s[:eq])
	coords := strings.TrimSpace(s[eq+1:])
	parts := strings.Split(coords, ",")
	if len(parts) != 2 {
		return ObserverSpec{}, fmt.Errorf("invalid --observer %q (want id=lat,lon)", s)
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return ObserverSpec{}, fmt.Errorf("invalid --observer latitude in %q: %w", s, err)
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return ObserverSpec{}, fmt.Errorf("invalid --observer longitude in %q: %w", s, err)
	}
	if id == "" {
		return ObserverSpec{}, fmt.Errorf("invalid --observer %q: empty id", s)
	}
	return ObserverSpec{ID: id, LatDeg: lat, LonDeg: lon}, nil
}

// parseObserverFlags parses a list of --observer values; IDs must be unique.
func parseObserverFlags(raw []string) ([]ObserverSpec, error) {
	out := make([]ObserverSpec, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, r := range raw {
		spec, err := parseObserverFlag(r)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[spec.ID]; ok {
			return nil, fmt.Errorf("duplicate --observer id %q", spec.ID)
		}
		seen[spec.ID] = struct{}{}
		out = append(out, spec)
	}
	return out, nil
}

// resolveTLEObservers returns observers for TLE mode.
// Prefer explicit --observer list; else sandbox-0 from --lat/--lon when those were set.
func resolveTLEObservers(observers []ObserverSpec, lat, lon float64, latLonSet bool) ([]ObserverSpec, error) {
	if len(observers) > 0 {
		return observers, nil
	}
	if !latLonSet {
		return nil, fmt.Errorf("--lat and --lon are required when using --tle (or pass --observer id=lat,lon)")
	}
	return []ObserverSpec{{ID: "sandbox-0", LatDeg: lat, LonDeg: lon}}, nil
}

// rejectObserverDeviceMix errors if geographic observers are combined with phase-offset flags.
func rejectObserverDeviceMix(observers []ObserverSpec, numDevices int, phaseSec float64) error {
	if len(observers) == 0 {
		return nil
	}
	if numDevices != 1 || phaseSec != 0 {
		return fmt.Errorf("--observer cannot be combined with --devices / --phase-sec (use one multi-device mode)")
	}
	return nil
}
