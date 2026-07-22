// Package tle provides TLE (Two-Line Element) parsing, satellite pass
// prediction, link model interpolation, profile generation, and a
// SequenceEvaluator for live TLE-driven simulations.
//
// SGP4 propagation is provided by github.com/joshuaferrara/go-satellite
// (v0.0.0-20220611, last updated 2022). The library is stable and
// functionally complete for SGP4/SDP4, but pulls dated transitive deps
// (github.com/pkg/errors, old ginkgo/gomega test deps). If a
// maintained alternative emerges, consider swapping.
package tle

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Satellite represents a parsed TLE entry.
type Satellite struct {
	Name    string // Line 0 (optional name line in 3-line format)
	Line1   string // TLE line 1
	Line2   string // TLE line 2
	NoradID int    // Extracted from line 1 (columns 3-7)
}

// ParseFile reads a TLE file and returns all satellites found.
// Supports both 2-line and 3-line TLE formats.
func ParseFile(path string) ([]Satellite, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("tle: opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return Parse(f)
}

// Parse reads TLE entries from r. It handles both 2-line format
// (lines starting with "1 " and "2 ") and 3-line format (name line
// followed by line 1 and line 2).
func Parse(r io.Reader) ([]Satellite, error) {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\n ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("tle: reading input: %w", err)
	}

	var sats []Satellite
	i := 0
	for i < len(lines) {
		// Try to identify the start of a TLE entry.
		if isTLELine1(lines[i]) {
			// 2-line format: line1 + line2
			if i+1 >= len(lines) {
				return nil, fmt.Errorf("tle: line %d: line 1 without matching line 2", i+1)
			}
			if !isTLELine2(lines[i+1]) {
				return nil, fmt.Errorf("tle: line %d: expected line 2, got: %q", i+2, lines[i+1])
			}
			sat, err := parseSatellite("", lines[i], lines[i+1])
			if err != nil {
				return nil, fmt.Errorf("tle: line %d: %w", i+1, err)
			}
			sats = append(sats, sat)
			i += 2
		} else if i+2 < len(lines) && isTLELine1(lines[i+1]) && isTLELine2(lines[i+2]) {
			// 3-line format: name + line1 + line2
			sat, err := parseSatellite(lines[i], lines[i+1], lines[i+2])
			if err != nil {
				return nil, fmt.Errorf("tle: line %d: %w", i+1, err)
			}
			sats = append(sats, sat)
			i += 3
		} else {
			return nil, fmt.Errorf("tle: line %d: unrecognized format: %q", i+1, lines[i])
		}
	}

	if len(sats) == 0 {
		return nil, fmt.Errorf("tle: no valid TLE entries found")
	}

	return sats, nil
}

// SelectSatellite finds a satellite by name (case-insensitive substring)
// or NORAD ID (exact integer match). Returns an error if no match or
// ambiguous.
func SelectSatellite(sats []Satellite, selector string) (Satellite, error) {
	if selector == "" {
		if len(sats) == 0 {
			return Satellite{}, fmt.Errorf("tle: no satellites available")
		}
		return sats[0], nil
	}

	// Try NORAD ID first.
	if id, err := strconv.Atoi(selector); err == nil {
		for _, s := range sats {
			if s.NoradID == id {
				return s, nil
			}
		}
		return Satellite{}, fmt.Errorf("tle: no satellite with NORAD ID %d", id)
	}

	// Name search (case-insensitive substring).
	lower := strings.ToLower(selector)
	var matches []Satellite
	for _, s := range sats {
		if strings.Contains(strings.ToLower(s.Name), lower) {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return Satellite{}, fmt.Errorf("tle: no satellite matching %q", selector)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return Satellite{}, fmt.Errorf("tle: ambiguous selector %q matches %d satellites: %s",
			selector, len(matches), strings.Join(names, ", "))
	}
}

// isTLELine1 checks if a line looks like TLE line 1.
func isTLELine1(line string) bool {
	return len(line) >= 69 && line[0] == '1' && line[1] == ' '
}

// isTLELine2 checks if a line looks like TLE line 2.
func isTLELine2(line string) bool {
	return len(line) >= 69 && line[0] == '2' && line[1] == ' '
}

// parseSatellite constructs a Satellite from its component lines.
func parseSatellite(name, line1, line2 string) (Satellite, error) {
	// Extract NORAD ID from line 1, columns 3-7 (1-indexed: positions 2-6 in 0-indexed).
	idStr := strings.TrimSpace(line1[2:7])
	noradID, err := strconv.Atoi(idStr)
	if err != nil {
		return Satellite{}, fmt.Errorf("invalid NORAD ID in line 1: %q", idStr)
	}

	return Satellite{
		Name:    strings.TrimSpace(name),
		Line1:   line1,
		Line2:   line2,
		NoradID: noradID,
	}, nil
}
