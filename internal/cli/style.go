// Package cli provides terminal styling helpers (ANSI colors, symbols)
// for NTN-in-a-Box CLI output.
package cli

import (
	"fmt"
	"os"
)

// ANSI color codes.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	BgRed   = "\033[41m"
	BgGreen = "\033[42m"
)

// Symbols for coverage state.
const (
	SymUp      = "▲"
	SymDown    = "▼"
	SymWarn    = "⚠"
	SymBlock   = "■"
	SymDot     = "●"
	SymArrowUp = "↑"
)

// ColorEnabled returns true if stderr supports color output.
func ColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Styled wraps text in an ANSI style code, with reset appended.
// If color is disabled, returns text unchanged.
func Styled(style, text string) string {
	if !colorOn {
		return text
	}
	return style + text + Reset
}

// colorOn is set at init based on terminal detection.
var colorOn = ColorEnabled()

// SetColor allows overriding color detection (for testing).
func SetColor(enabled bool) {
	colorOn = enabled
}

// CoverageOpened formats a "coverage opened" message.
func CoverageOpened(profile string, remainingSec float64) string {
	return fmt.Sprintf("%s %s · %s · %.0fs remaining",
		Styled(Green+Bold, SymUp),
		Styled(Green+Bold, "coverage opened"),
		Styled(Cyan, profile),
		remainingSec)
}

// CoverageClosed formats a "coverage closed" message.
func CoverageClosed(nextInSec float64) string {
	return fmt.Sprintf("%s %s · next window in %.0fs",
		Styled(Red+Bold, SymDown),
		Styled(Red+Bold, "coverage lost"),
		nextInSec)
}

// CoverageClosing formats a "closing soon" warning.
func CoverageClosing(inSec float64) string {
	return fmt.Sprintf("%s %s in %.0fs",
		Styled(Yellow, SymWarn),
		Styled(Yellow, "window closing"),
		inSec)
}

// CoverageOpening formats a "opening soon" notice.
func CoverageOpening(inSec float64) string {
	return fmt.Sprintf("%s %s in %.0fs",
		Styled(Blue, SymUp),
		Styled(Blue, "window opening"),
		inSec)
}

// RequestOK formats a successful request line.
func RequestOK(ts, latency string, status int) string {
	// Pad raw string before styling to avoid ANSI bytes affecting width.
	for len(latency) < 7 {
		latency = " " + latency
	}
	return "  " + Styled(Dim, ts) + "  " +
		Styled(Green, fmt.Sprintf("%d", status)) + "  " +
		Styled(White, latency) + "  " +
		Styled(Green, "ok")
}

// RequestFail formats a failed request line.
func RequestFail(ts, reason string) string {
	return "  " + Styled(Dim, ts) + "  " +
		Styled(Red, "---") + "  " +
		Styled(Dim, "      —") + "  " +
		Styled(Red, reason)
}

// Header formats a column header line.
func Header() string {
	return Styled(Dim, "  TIME      STS   LATENCY  RESULT")
}
