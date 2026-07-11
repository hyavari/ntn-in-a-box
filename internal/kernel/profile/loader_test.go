package profile

import (
	"path/filepath"
	"testing"
)

func TestLoadFile_SampleProfiles(t *testing.T) {
	tests := []struct {
		file         string
		wantName     string
		wantMode     Mode
		wantWindow   float64
		wantCurveLen int // len(DelayMs)
	}{
		{
			file:         "leo_pass_90s.yaml",
			wantName:     "leo_pass_90s",
			wantMode:     ModePeriodic,
			wantWindow:   90,
			wantCurveLen: 4,
		},
		{
			file:         "geo_steady.yaml",
			wantName:     "geo_steady",
			wantMode:     ModeContinuous,
			wantWindow:   0,
			wantCurveLen: 1,
		},
		{
			file:         "d2c_burst.yaml",
			wantName:     "d2c_burst",
			wantMode:     ModePeriodic,
			wantWindow:   20,
			wantCurveLen: 1,
		},
		{
			file:         "sos_burst.yaml",
			wantName:     "sos_burst",
			wantMode:     ModePeriodic,
			wantWindow:   15,
			wantCurveLen: 3,
		},
		{
			file:         "sos_hostile.yaml",
			wantName:     "sos_hostile",
			wantMode:     ModePeriodic,
			wantWindow:   10,
			wantCurveLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "testdata", "profiles", tt.file)
			p, err := LoadFile(path)
			if err != nil {
				t.Fatalf("LoadFile(%s) returned error: %v", path, err)
			}
			if p.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", p.Name, tt.wantName)
			}
			if p.Schedule.Mode != tt.wantMode {
				t.Errorf("Schedule.Mode = %q, want %q", p.Schedule.Mode, tt.wantMode)
			}
			if p.Schedule.WindowSec != tt.wantWindow {
				t.Errorf("Schedule.WindowSec = %v, want %v", p.Schedule.WindowSec, tt.wantWindow)
			}
			if len(p.Curves.DelayMs) != tt.wantCurveLen {
				t.Errorf("len(Curves.DelayMs) = %d, want %d", len(p.Curves.DelayMs), tt.wantCurveLen)
			}
		})
	}
}

func TestLoadFile_MissingFile(t *testing.T) {
	_, err := LoadFile(filepath.Join("testdata", "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("LoadFile on a missing file: expected an error, got nil")
	}
}

func TestLoadBytes_MalformedYAML(t *testing.T) {
	_, err := LoadBytes([]byte("name: [this is not valid: yaml"))
	if err == nil {
		t.Fatal("LoadBytes on malformed YAML: expected an error, got nil")
	}
}

func TestLoadBytes_InvalidProfileIsRejected(t *testing.T) {
	// Well-formed YAML, but fails Validate (missing schedule.mode).
	_, err := LoadBytes([]byte(`
name: broken
curves:
  delay_ms: [{offset_sec: 0, value: 1}]
  jitter_ms: [{offset_sec: 0, value: 1}]
  loss_pct: [{offset_sec: 0, value: 1}]
  bandwidth_kbps: [{offset_sec: 0, value: 1}]
`))
	if err == nil {
		t.Fatal("LoadBytes on an invalid profile: expected an error, got nil")
	}
}
