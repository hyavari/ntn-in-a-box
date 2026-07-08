package tui

// sparkBlocks are the Unicode block characters used for sparklines,
// ordered from lowest to highest.
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// renderSparkline renders a slice of float64 values as a Unicode
// sparkline string. Values are normalized against the slice's own max.
// An empty slice returns an empty string. When all values are equal
// (flat line), renders at mid-height to convey "steady" rather than
// "maxed out."
func renderSparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}

	minVal := values[0]
	maxVal := values[0]
	for _, v := range values[1:] {
		if v > maxVal {
			maxVal = v
		}
		if v < minVal {
			minVal = v
		}
	}

	runes := make([]rune, len(values))
	if maxVal == minVal {
		// Flat line — show mid-height block.
		for i := range values {
			runes[i] = sparkBlocks[3] // ▄
		}
	} else {
		for i, v := range values {
			runes[i] = sparkCharRange(v, minVal, maxVal)
		}
	}
	return string(runes)
}

// renderSparklineFixed renders with a fixed max (e.g. 100 for loss%).
func renderSparklineFixed(values []float64, fixedMax float64) string {
	if len(values) == 0 {
		return ""
	}

	runes := make([]rune, len(values))
	for i, v := range values {
		runes[i] = sparkChar(v, fixedMax)
	}
	return string(runes)
}

// sparkCharRange maps a value to a spark block given a min and max range.
func sparkCharRange(val, minVal, maxVal float64) rune {
	if maxVal <= minVal {
		return sparkBlocks[3]
	}
	ratio := (val - minVal) / (maxVal - minVal)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	idx := int(ratio * float64(len(sparkBlocks)-1))
	return sparkBlocks[idx]
}

// sparkChar maps a value to a spark block character given a max.
func sparkChar(val, maxVal float64) rune {
	if maxVal <= 0 {
		return sparkBlocks[0]
	}
	ratio := val / maxVal
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	idx := int(ratio * float64(len(sparkBlocks)-1))
	return sparkBlocks[idx]
}
