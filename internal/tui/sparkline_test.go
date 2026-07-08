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

func TestSparkChar_NegativeMax(t *testing.T) {
	got := sparkCharRange(5, 5, 0)
	if got != '▄' {
		t.Errorf("sparkCharRange(5, 5, 0) = %c, want ▄", got)
	}
}
