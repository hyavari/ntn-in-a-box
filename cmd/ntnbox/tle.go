package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hyavari/ntn-in-a-box/internal/tle"
	"gopkg.in/yaml.v3"
)

func runTLE(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ntnbox tle <subcommand>\n\nSubcommands:\n  generate   Generate profile YAML from TLE data")
	}

	switch args[0] {
	case "generate":
		return runTLEGenerate(args[1:])
	default:
		return fmt.Errorf("unknown tle subcommand: %s\n\nSubcommands:\n  generate   Generate profile YAML from TLE data", args[0])
	}
}

func runTLEGenerate(args []string) error {
	fs := flag.NewFlagSet("tle generate", flag.ContinueOnError)
	filePath := fs.String("file", "", "Path to TLE file (required)")
	lat := fs.Float64("lat", 0, "Observer latitude in degrees, north positive (required)")
	lon := fs.Float64("lon", 0, "Observer longitude in degrees, east positive (required)")
	alt := fs.Float64("alt", 0, "Observer altitude above sea level in km")
	output := fs.String("output", "", "Output file path or directory (required)")
	passes := fs.Int("passes", 1, "Number of passes to generate")
	linkModelPath := fs.String("link-model", "", "Path to custom link model YAML (default: built-in)")
	elevMin := fs.Float64("elev-min", 10, "Minimum elevation angle in degrees")
	satSelector := fs.String("sat", "", "Select satellite by name or NORAD ID")
	startStr := fs.String("start", "", "Start time for prediction (RFC3339, default: now)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate required flags.
	if *filePath == "" {
		return errors.New("--file is required\n\nUsage: ntnbox tle generate --file <path> --lat <deg> --lon <deg> --output <path>")
	}
	latSet, lonSet := false, false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "lat" {
			latSet = true
		}
		if f.Name == "lon" {
			lonSet = true
		}
	})
	if !latSet || !lonSet {
		return errors.New("--lat and --lon are required\n\nUsage: ntnbox tle generate --file <path> --lat <deg> --lon <deg> --output <path>")
	}
	if *output == "" {
		return errors.New("--output is required\n\nUsage: ntnbox tle generate --file <path> --lat <deg> --lon <deg> --output <path>")
	}

	// Parse TLE file.
	sats, err := tle.ParseFile(*filePath)
	if err != nil {
		return err
	}

	// Select satellite.
	sat, err := tle.SelectSatellite(sats, *satSelector)
	if err != nil {
		return err
	}

	if *satSelector == "" && len(sats) > 1 {
		fmt.Fprintf(os.Stderr, "ntnbox: using satellite %q (NORAD ID %d) — use --sat to select a different one\n",
			sat.Name, sat.NoradID)
	}

	// Check TLE age.
	age, err := tle.Age(sat, time.Now())
	if err == nil && age > 14*24*time.Hour {
		fmt.Fprintf(os.Stderr, "ntnbox: warning: TLE is %.0f days old (accuracy degrades after ~14 days)\n", age.Hours()/24)
	}

	// Load link model.
	var model tle.LinkModel
	if *linkModelPath != "" {
		m, err := tle.LoadLinkModel(*linkModelPath)
		if err != nil {
			return err
		}
		model = *m
	} else {
		model = tle.DefaultLinkModel()
	}

	// Determine start time.
	startTime := time.Now()
	if *startStr != "" {
		t, err := time.Parse(time.RFC3339, *startStr)
		if err != nil {
			return fmt.Errorf("invalid --start time: %w", err)
		}
		startTime = t
	}

	// Predict passes.
	obs := tle.Observer{LatDeg: *lat, LonDeg: *lon, AltKm: *alt}
	cfg := tle.PredictConfig{
		MinElevDeg: *elevMin,
		Count:      *passes,
		MaxSearch:  48 * time.Hour,
	}

	fmt.Fprintf(os.Stderr, "ntnbox: predicting passes for %q from (%.4f°, %.4f°)...\n", sat.Name, *lat, *lon)

	predicted, err := tle.PredictPasses(sat, obs, startTime, cfg)
	if err != nil {
		return fmt.Errorf("predicting passes: %w", err)
	}
	if len(predicted) == 0 {
		return fmt.Errorf("no visible passes found for %q from (%.4f°, %.4f°) in the next 48h — try lowering --elev-min",
			sat.Name, *lat, *lon)
	}

	// Generate profiles and write output.
	if *passes == 1 {
		// Single pass: write to output file.
		p, err := tle.GenerateProfile(predicted[0], model, tle.GenerateOpts{
			LookaheadSec: 30,
			GapSec:       0,
			Index:        0,
			SatName:      sat.Name,
		})
		if err != nil {
			return fmt.Errorf("generating profile: %w", err)
		}
		if err := writeProfileYAML(*output, p); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ntnbox: wrote %s (pass: %s, duration: %s, max elev: %.1f°)\n",
			*output, predicted[0].Rise.Format(time.RFC3339), predicted[0].Duration.Round(time.Second), predicted[0].MaxElevDeg)
	} else {
		// Multiple passes: write to directory.
		if err := os.MkdirAll(*output, 0o755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
		for i, pass := range predicted {
			var gapSec float64
			if i+1 < len(predicted) {
				gapSec = predicted[i+1].Rise.Sub(pass.Set).Seconds()
			}
			p, err := tle.GenerateProfile(pass, model, tle.GenerateOpts{
				LookaheadSec: 30,
				GapSec:       gapSec,
				Index:        i,
				SatName:      sat.Name,
			})
			if err != nil {
				return fmt.Errorf("generating profile for pass %d: %w", i+1, err)
			}
			filename := filepath.Join(*output, fmt.Sprintf("pass_%03d.yaml", i+1))
			if err := writeProfileYAML(filename, p); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "  pass %d: %s (duration: %s, max elev: %.1f°) → %s\n",
				i+1, pass.Rise.Format(time.RFC3339), pass.Duration.Round(time.Second), pass.MaxElevDeg, filename)
		}
		fmt.Fprintf(os.Stderr, "ntnbox: wrote %d profiles to %s\n", len(predicted), *output)
	}

	return nil
}

func writeProfileYAML(path string, p interface{}) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshaling YAML: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
