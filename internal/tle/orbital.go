package tle

import (
	"math"
	"time"

	satellite "github.com/joshuaferrara/go-satellite"
)

// SatPosition holds the satellite's geodetic position and look angles
// from an observer at a given instant.
type SatPosition struct {
	LatDeg       float64
	LonDeg       float64
	AltKm        float64
	ElevationDeg float64
	AzimuthDeg   float64
	RangeKm      float64
}

// geodeticAt propagates the SGP4 satellite to time t and returns its
// geodetic position (lat/lon/alt) plus look angles from the observer.
// Returns ok=false on propagation failure.
// Shared by Position(), elevationAt(), and ComputeOrbitPoints().
func geodeticAt(sgp4Sat satellite.Satellite, obsLL satellite.LatLong, obsAltKm float64, t time.Time) (SatPosition, bool) {
	t = t.UTC()
	year, month, day := t.Date()
	hour, min, sec := t.Clock()

	pos, _ := satellite.Propagate(sgp4Sat, year, int(month), day, hour, min, sec)

	// Propagation failure (zero vector).
	if pos.X == 0 && pos.Y == 0 && pos.Z == 0 {
		return SatPosition{ElevationDeg: -90}, false
	}

	jday := satellite.JDay(year, int(month), day, hour, min, sec)
	gmst := satellite.GSTimeFromDate(year, int(month), day, hour, min, sec)

	// Geodetic position.
	alt, _, latLon := satellite.ECIToLLA(pos, gmst)
	latLonDeg := satellite.LatLongDeg(latLon)

	// Look angles from observer.
	lookAngles := satellite.ECIToLookAngles(pos, obsLL, obsAltKm, jday)

	return SatPosition{
		LatDeg:       latLonDeg.Latitude,
		LonDeg:       latLonDeg.Longitude,
		AltKm:        alt,
		ElevationDeg: lookAngles.El * 180.0 / math.Pi,
		AzimuthDeg:   lookAngles.Az * 180.0 / math.Pi,
		RangeKm:      lookAngles.Rg,
	}, true
}

// initSGP4 initializes a go-satellite Satellite from TLE lines.
// Cached by callers to avoid re-parsing on every call.
// Returns a zero-value satellite if lines are empty (safe for
// callers that don't use Position).
func initSGP4(sat Satellite) satellite.Satellite {
	if sat.Line1 == "" || sat.Line2 == "" {
		return satellite.Satellite{}
	}
	return satellite.TLEToSat(sat.Line1, sat.Line2, satellite.GravityWGS84)
}

// observerLatLong converts an Observer to the go-satellite LatLong
// format (radians).
func observerLatLong(obs Observer) satellite.LatLong {
	return satellite.LatLong{
		Latitude:  obs.LatDeg * math.Pi / 180.0,
		Longitude: obs.LonDeg * math.Pi / 180.0,
	}
}
