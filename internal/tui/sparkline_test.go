package tui

import "testing"

func TestRenderSparkline_Empty(t *testing.T) {
	got := renderSparkline(nil)
	if got != "" {
		t.Errorf("renderSparkline(nil) = %q, want empty", got)
	}
}

func TestRenderSparkline_SingleValue(t *testing.T) {
	got := renderSparkline([]float64{50})
	// Single value is flat → mid-height.
	if got != "▄" {
		t.Errorf("renderSparkline([50]) = %q, want %q", got, "▄")
	}
}

func TestRenderSparkline_AllZero(t *testing.T) {
	// All zero is a flat line → mid-height.
	got := renderSparkline([]float64{0, 0, 0, 0, 0})
	want := "▄▄▄▄▄"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderSparkline_Ascending(t *testing.T) {
	// 8 values: min=0, max=7. Normalized via (val-min)/(max-min).
	// 0→▁, 1→▂, 2→▃, 3→▄, 4→▅, 5→▆, 6→▇, 7→█
	values := []float64{0, 1, 2, 3, 4, 5, 6, 7}
	got := renderSparkline(values)
	want := "▁▂▃▄▅▆▇█"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderSparkline_MaxValue(t *testing.T) {
	// All at same value → flat → mid-height.
	values := []float64{100, 100, 100}
	got := renderSparkline(values)
	want := "▄▄▄"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderSparklineFixed(t *testing.T) {
	values := []float64{0, 25, 50, 75, 100}
	got := renderSparklineFixed(values, 100)
	// 0→▁, 25→▂, 50→▄, 75→▅or▆, 100→█
	if len([]rune(got)) != 5 {
		t.Errorf("expected 5 chars, got %d: %q", len([]rune(got)), got)
	}
	runes := []rune(got)
	if runes[0] != '▁' {
		t.Errorf("first char should be ▁, got %c", runes[0])
	}
	if runes[4] != '█' {
		t.Errorf("last char should be █, got %c", runes[4])
	}
}

func TestSparkChar_NegativeMax(t *testing.T) {
	got := sparkChar(5, 0)
	if got != '▁' {
		t.Errorf("sparkChar(5, 0) = %c, want ▁", got)
	}
}
