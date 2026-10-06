package ui

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ssnxd/agentbar/internal/state"
)

// Catppuccin Mocha, matching the user's tmux theme. Lipgloss downsamples to
// the terminal's real capability, so these degrade without RGB.
var (
	cSurface0 = lipgloss.Color("#313244")
	cSurface1 = lipgloss.Color("#45475a")
	cOverlay1 = lipgloss.Color("#7f849c")
	cOverlay2 = lipgloss.Color("#9399b2")
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
	sRule   = lipgloss.NewStyle().Foreground(cSurface1)
	sQuiet  = lipgloss.NewStyle().Bold(true).Foreground(cSubtext0)
	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(cMauve)
	sCount  = lipgloss.NewStyle().Foreground(cSubtext0)
	sHot    = lipgloss.NewStyle().Bold(true).Foreground(cPeach)
	sDone   = lipgloss.NewStyle().Foreground(cGreen)
	sGroup  = lipgloss.NewStyle().Bold(true).Foreground(cSky)
	sKey    = lipgloss.NewStyle().Bold(true).Foreground(cSky)
	sKeyLbl = lipgloss.NewStyle().Foreground(cOverlay1)
	sHere   = lipgloss.NewStyle().Foreground(cMauve)
	sMsg    = lipgloss.NewStyle().Foreground(cPeach)
	sErr    = lipgloss.NewStyle().Foreground(cRed)
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// setAccent recolours what carries the accent, the title and the here
// bar, so the list matches the status line it opens from. Anything but a
// "#rrggbb" colour is ignored. Reports whether it took the colour.
func setAccent(c string) bool {
	c = strings.TrimSpace(c)
	if !hexColor.MatchString(c) {
		return false
	}
	sTitle = sTitle.Foreground(lipgloss.Color(c))
	sHere = sHere.Foreground(lipgloss.Color(c))
	return true
}

type statusLook struct {
	icon  string
	label string
	style lipgloss.Style
}

var statusLooks = map[string]statusLook{
	state.StatusNeedsYou: {"●", "needs you", lipgloss.NewStyle().Bold(true).Foreground(cPeach)},
	state.StatusWorking:  {"◐", "working", lipgloss.NewStyle().Foreground(cBlue)},
	// done is an outcome, like error, so it takes a mark, not a circle: the
	// circles are the live states. It is not bold: it does not ask.
	state.StatusDone: {"✓", "done", sDone},
	// waiting is quiet on purpose: idle sessions must not compete with
	// the ones that need you.
	state.StatusWaiting: {"○", "waiting", lipgloss.NewStyle().Foreground(cSubtext0)},
	state.StatusError:   {"✗", "error", lipgloss.NewStyle().Foreground(cRed)},
	state.StatusUnknown: {"◌", "unknown", lipgloss.NewStyle().Foreground(cOverlay1)},
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
	case state.StatusDone:
		return 2
	case state.StatusWaiting:
		return 3
	case state.StatusWorking:
		return 4
	default:
		return 5
	}
}

// sel applies the selection band to a style when selected.
func sel(st lipgloss.Style, selected bool) lipgloss.Style {
	if selected {
		return st.Background(cSurface0)
	}
	return st
}

// dim is sDim, lifted one step on the selection band so it keeps the
// contrast it has on the plain background.
func dim(selected bool) lipgloss.Style {
	if selected {
		return lipgloss.NewStyle().Foreground(cOverlay2).Background(cSurface0)
	}
	return sDim
}

// quiet reports whether a card sits back: nothing is happening in it and
// nothing is asked of you.
func quiet(status string) bool {
	return status == state.StatusWaiting || status == state.StatusUnknown
}
