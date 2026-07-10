package tle

import (
	_ "embed"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

//go:embed linkmodel_default.yaml
var defaultLinkModelYAML []byte

// LinkModel defines how elevation angle maps to impairment values.
// Each field is a piecewise-linear curve sorted by ElevDeg (ascending).
type LinkModel struct {
	Name          string       `yaml:"name"`
	MinElevDeg    float64      `yaml:"min_elev_deg"`
	DelayMs       []ModelPoint `yaml:"delay_ms"`
	JitterMs      []ModelPoint `yaml:"jitter_ms"`
	LossPct       []ModelPoint `yaml:"loss_pct"`
	BandwidthKbps []ModelPoint `yaml:"bandwidth_kbps"`
}

// ModelPoint maps an elevation angle to an impairment value.
type ModelPoint struct {
	ElevDeg float64 `yaml:"elev_deg"`
	Value   float64 `yaml:"value"`
}

// DefaultLinkModel returns the built-in LEO default link model.
func DefaultLinkModel() LinkModel {
	var m LinkModel
	// Embedded YAML is known-good; panic on error (programming bug).
	if err := yaml.Unmarshal(defaultLinkModelYAML, &m); err != nil {
		panic("tle: embedded default link model is invalid: " + err.Error())
	}
	return m
}

// LoadLinkModel loads a custom link model from a YAML file.
func LoadLinkModel(path string) (*LinkModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tle: reading link model %s: %w", path, err)
	}
	var m LinkModel
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("tle: parsing link model %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("tle: link model %s: %w", path, err)
	}
	return &m, nil
}

// Validate checks that the link model has at least one point per curve
// and that points are sorted by elevation.
func (m *LinkModel) Validate() error {
	curves := []struct {
		name   string
		points []ModelPoint
	}{
		{"delay_ms", m.DelayMs},
		{"jitter_ms", m.JitterMs},
		{"loss_pct", m.LossPct},
		{"bandwidth_kbps", m.BandwidthKbps},
	}
	for _, c := range curves {
		if len(c.points) == 0 {
			return fmt.Errorf("curve %s has no points", c.name)
		}
		if !sort.SliceIsSorted(c.points, func(i, j int) bool {
			return c.points[i].ElevDeg < c.points[j].ElevDeg
		}) {
			return fmt.Errorf("curve %s points not sorted by elev_deg", c.name)
		}
	}
	return nil
}

// Interpolate returns the impairment values for a given elevation angle
// using piecewise-linear interpolation on each curve.
func (m *LinkModel) Interpolate(elevDeg float64) (delay, jitter, loss, bw float64) {
	delay = interpolateCurve(m.DelayMs, elevDeg)
	jitter = interpolateCurve(m.JitterMs, elevDeg)
	loss = interpolateCurve(m.LossPct, elevDeg)
	bw = interpolateCurve(m.BandwidthKbps, elevDeg)
	return
}

// interpolateCurve performs piecewise-linear interpolation on a sorted
// slice of ModelPoints. Clamps to the first/last value for
// elevations outside the defined range.
func interpolateCurve(points []ModelPoint, elevDeg float64) float64 {
	if len(points) == 0 {
		return 0
	}
	// Clamp below minimum.
	if elevDeg <= points[0].ElevDeg {
		return points[0].Value
	}
	// Clamp above maximum.
	if elevDeg >= points[len(points)-1].ElevDeg {
		return points[len(points)-1].Value
	}
	// Find the segment and interpolate.
	for i := 1; i < len(points); i++ {
		if elevDeg <= points[i].ElevDeg {
			lo := points[i-1]
			hi := points[i]
			t := (elevDeg - lo.ElevDeg) / (hi.ElevDeg - lo.ElevDeg)
			return lo.Value + t*(hi.Value-lo.Value)
		}
	}
	return points[len(points)-1].Value
}
