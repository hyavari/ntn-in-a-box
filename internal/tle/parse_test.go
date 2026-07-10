package tle

import (
	"strings"
	"testing"
)

func TestParseFile_ThreeLine(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/iss.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(sats) != 1 {
		t.Fatalf("expected 1 satellite, got %d", len(sats))
	}
	if sats[0].Name != "ISS (ZARYA)" {
		t.Errorf("Name = %q, want %q", sats[0].Name, "ISS (ZARYA)")
	}
	if sats[0].NoradID != 25544 {
		t.Errorf("NoradID = %d, want 25544", sats[0].NoradID)
	}
}

func TestParseFile_TwoLine(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/twoline.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(sats) != 1 {
		t.Fatalf("expected 1 satellite, got %d", len(sats))
	}
	if sats[0].Name != "" {
		t.Errorf("Name = %q, want empty", sats[0].Name)
	}
	if sats[0].NoradID != 25544 {
		t.Errorf("NoradID = %d, want 25544", sats[0].NoradID)
	}
}

func TestParseFile_Multi(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/multi.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(sats) != 3 {
		t.Fatalf("expected 3 satellites, got %d", len(sats))
	}

	expected := []struct {
		name    string
		noradID int
	}{
		{"ISS (ZARYA)", 25544},
		{"STARLINK-1007", 44713},
		{"IRIDIUM 33 DEB", 24946},
	}
	for i, exp := range expected {
		if sats[i].Name != exp.name {
			t.Errorf("sats[%d].Name = %q, want %q", i, sats[i].Name, exp.name)
		}
		if sats[i].NoradID != exp.noradID {
			t.Errorf("sats[%d].NoradID = %d, want %d", i, sats[i].NoradID, exp.noradID)
		}
	}
}

func TestParse_Malformed(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"garbage", "this is not a TLE"},
		{"line1_only", "1 25544U 98067A   24100.54321000  .00016717  00000-0  10270-3 0  9993"},
		{"line1_with_bad_line2", "1 25544U 98067A   24100.54321000  .00016717  00000-0  10270-3 0  9993\nnot a line 2 at all padded to be long enough for the check here now!!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestSelectSatellite_ByName(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/multi.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	sat, err := SelectSatellite(sats, "starlink")
	if err != nil {
		t.Fatalf("SelectSatellite: %v", err)
	}
	if sat.Name != "STARLINK-1007" {
		t.Errorf("Name = %q, want STARLINK-1007", sat.Name)
	}
}

func TestSelectSatellite_ByNoradID(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/multi.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	sat, err := SelectSatellite(sats, "24946")
	if err != nil {
		t.Fatalf("SelectSatellite: %v", err)
	}
	if sat.Name != "IRIDIUM 33 DEB" {
		t.Errorf("Name = %q, want IRIDIUM 33 DEB", sat.Name)
	}
}

func TestSelectSatellite_Empty(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/multi.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Empty selector returns first.
	sat, err := SelectSatellite(sats, "")
	if err != nil {
		t.Fatalf("SelectSatellite: %v", err)
	}
	if sat.NoradID != 25544 {
		t.Errorf("NoradID = %d, want 25544 (first in file)", sat.NoradID)
	}
}

func TestSelectSatellite_NoMatch(t *testing.T) {
	sats, err := ParseFile("../../testdata/tle/multi.tle")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	_, err = SelectSatellite(sats, "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-matching selector")
	}
}
