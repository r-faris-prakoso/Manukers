// Package theme provides shared color and style helpers used by both the ui
// and ui/views packages, avoiding import cycles.
package theme

import "github.com/gdamore/tcell/v2"

// ─── Color palette ────────────────────────────────────────────────────────────

var (
	ColorTitle       = tcell.ColorDarkCyan
	ColorHeader      = tcell.ColorYellow
	ColorHealthy     = tcell.ColorGreen
	ColorWarning     = tcell.ColorYellow
	ColorUnhealthy   = tcell.ColorRed
	ColorDim         = tcell.ColorDarkGray
	ColorNormal      = tcell.ColorWhite
	ColorAccent      = tcell.ColorAqua
	ColorNavActive   = tcell.ColorAqua
	ColorNavInactive = tcell.ColorDarkGray
)

// StateColor returns a tcell color for an AWS resource state string.
func StateColor(state string) tcell.Color {
	switch state {
	case "running", "active", "available", "healthy", "ACTIVE", "AVAILABLE":
		return ColorHealthy
	case "stopped", "inactive", "failed", "unhealthy", "FAILED":
		return ColorUnhealthy
	case "pending", "provisioning", "initializing", "initial", "draining",
		"CREATING", "UPDATING", "DEGRADED":
		return ColorWarning
	default:
		return ColorNormal
	}
}

// StateIcon returns a small icon character for a state.
func StateIcon(state string) string {
	switch state {
	case "running", "active", "available", "healthy", "ACTIVE", "AVAILABLE":
		return "●"
	case "stopped", "failed", "unhealthy", "FAILED":
		return "✖"
	case "pending", "provisioning", "initializing", "CREATING", "UPDATING":
		return "◌"
	case "draining":
		return "⊘"
	default:
		return "○"
	}
}

// StateColorName returns a tview dynamic-color name for a state.
func StateColorName(state string) string {
	switch state {
	case "running", "active", "available", "healthy", "ACTIVE", "AVAILABLE":
		return "green"
	case "stopped", "failed", "unhealthy", "FAILED":
		return "red"
	default:
		return "yellow"
	}
}

// HealthBar returns a simple visual progress bar like "████░░" for health ratios.
func HealthBar(healthy, total int) string {
	if total == 0 {
		return "─"
	}
	const width = 6
	filled := (healthy * width) / total
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return bar
}
