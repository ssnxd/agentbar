package ui

import (
	"charm.land/lipgloss/v2"
	"github.com/ssnxd/agentbar/internal/state"
)

// Catppuccin Mocha, matching the user's tmux theme. Lipgloss downsamples to
// the terminal's real capability, so these degrade without RGB.
var (
	cSurface0 = lipgloss.Color("#313244")
	cOverlay1 = lipgloss.Color("#7f849c")
	cSubtext0 = lipgloss.Color("#a6adc8")
	cText     = lipgloss.Color("#cdd6f4")
	cBlue     = lipgloss.Color("#89b4fa")
	cSky      = lipgloss.Color("#89dceb")
	cGreen    = lipgloss.Color("#a6e3a1")
	cPeach    = lipgloss.Color("#fab387")
	cRed      = lipgloss.Color("#f38ba8")
	cMauve    = lipgloss.Color("#cba6f7")

	sText   = lipgloss.NewStyle().Foreground(cText)
	sDim    = lipgloss.NewStyle().Foreground(cOverlay1)
	sSub    = lipgloss.NewStyle().Foreground(cSubtext0)
	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(cMauve)
	sCount  = lipgloss.NewStyle().Foreground(cSubtext0)
	sHot    = lipgloss.NewStyle().Bold(true).Foreground(cPeach)
	sGroup  = lipgloss.NewStyle().Bold(true).Foreground(cSky)
	sKey    = lipgloss.NewStyle().Bold(true).Foreground(cSky)
	sKeyLbl = lipgloss.NewStyle().Foreground(cOverlay1)
	sHere   = lipgloss.NewStyle().Foreground(cMauve)
	sMsg    = lipgloss.NewStyle().Foreground(cPeach)
	sErr    = lipgloss.NewStyle().Foreground(cRed)
)

type statusLook struct {
	icon  string
	label string
	style lipgloss.Style
}

var statusLooks = map[string]statusLook{
	state.StatusNeedsYou: {"●", "needs you", lipgloss.NewStyle().Bold(true).Foreground(cPeach)},
	state.StatusWorking:  {"◐", "working", lipgloss.NewStyle().Foreground(cBlue)},
	state.StatusWaiting:  {"○", "waiting", lipgloss.NewStyle().Foreground(cGreen)},
	state.StatusError:    {"✗", "error", lipgloss.NewStyle().Foreground(cRed)},
	state.StatusUnknown:  {"◌", "unknown", lipgloss.NewStyle().Foreground(cOverlay1)},
}

// spinnerFrames animate working rows, driven by the poll tick.
var spinnerFrames = []string{"◐", "◓", "◑", "◒"}

func look(status string) statusLook {
	if l, ok := statusLooks[status]; ok {
		return l
	}
	return statusLooks[state.StatusUnknown]
}

// rank orders statuses by urgency: lower sorts first.
func rank(status string) int {
	switch status {
	case state.StatusNeedsYou:
		return 0
	case state.StatusError:
		return 1
	case state.StatusWaiting:
		return 2
	case state.StatusWorking:
		return 3
	default:
		return 4
	}
}

// sel applies the selection band to a style when selected.
func sel(st lipgloss.Style, selected bool) lipgloss.Style {
	if selected {
		return st.Background(cSurface0)
	}
	return st
}
