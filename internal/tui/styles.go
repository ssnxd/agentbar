package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ssnxd/workflow/internal/db"
)

// Tokyo Night-ish palette. Lipgloss v2 + Bubble Tea v2 downsample to the
// terminal's real capability, so these degrade gracefully without RGB.
var (
	cBg     = lipgloss.Color("#1a1b26")
	cBgAlt  = lipgloss.Color("#24283b")
	cSel    = lipgloss.Color("#2f334d")
	cFg     = lipgloss.Color("#c0caf5")
	cMuted  = lipgloss.Color("#565f89")
	cBlue   = lipgloss.Color("#7aa2f7")
	cCyan   = lipgloss.Color("#7dcfff")
	cGreen  = lipgloss.Color("#9ece6a")
	cYellow = lipgloss.Color("#e0af68")
	cRed    = lipgloss.Color("#f7768e")
	cPurple = lipgloss.Color("#bb9af7")

	sNormal   = lipgloss.NewStyle().Foreground(cFg)
	sDim      = lipgloss.NewStyle().Foreground(cMuted)
	sError    = lipgloss.NewStyle().Foreground(cRed)
	sOk       = lipgloss.NewStyle().Foreground(cGreen)
	sAccent   = lipgloss.NewStyle().Foreground(cBlue)
	sPurple   = lipgloss.NewStyle().Foreground(cPurple)
	sSelected = lipgloss.NewStyle().Bold(true).Foreground(cFg).Background(cSel)

	sAppTitle  = lipgloss.NewStyle().Bold(true).Foreground(cBg).Background(cBlue).Padding(0, 1)
	sCrumb     = lipgloss.NewStyle().Foreground(cCyan).Background(cBgAlt).Padding(0, 1)
	sHeadRow   = lipgloss.NewStyle().Foreground(cMuted).Bold(true)
	sBadgeHot  = lipgloss.NewStyle().Bold(true).Foreground(cBg).Background(cYellow).Padding(0, 1)
	sBadgeExt  = lipgloss.NewStyle().Foreground(cBg).Background(cMuted).Padding(0, 1)
	sBadgeDone = lipgloss.NewStyle().Bold(true).Foreground(cBg).Background(cGreen).Padding(0, 1)

	sKeyChip   = lipgloss.NewStyle().Bold(true).Foreground(cCyan)
	sKeyLabel  = lipgloss.NewStyle().Foreground(cMuted)
	sKeyGroup  = lipgloss.NewStyle().Bold(true).Foreground(cPurple)
	sPanel     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cMuted)
	sPanelHot  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBlue)
	sPanelName = lipgloss.NewStyle().Bold(true).Foreground(cBlue)
)

// spinnerFrames animate "working" agents, driven by the app tick.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type statusLook struct {
	icon  string
	label string
	style lipgloss.Style
}

var statusLooks = map[string]statusLook{
	db.StatusStarting: {"◌", "starting", sDim},
	db.StatusWorking:  {"●", "working", sAccent},
	db.StatusIdle:     {"○", "idle", sOk},
	db.StatusNeedsYou: {"✋", "needs you", lipgloss.NewStyle().Bold(true).Foreground(cYellow)},
	db.StatusError:    {"✗", "error", sError},
	db.StatusDone:     {"✓", "done", sOk},
	db.StatusEnded:    {"▪", "ended", sDim},
	db.StatusDead:     {"☠", "dead", sError},
}

// statusCell renders an icon+label; working agents get an animated spinner.
func statusCell(status string, tick int) string {
	l, ok := statusLooks[status]
	if !ok {
		return status
	}
	icon := l.icon
	if status == db.StatusWorking {
		icon = spinnerFrames[tick%len(spinnerFrames)]
	}
	return l.style.Render(fmt.Sprintf("%s %s", icon, l.label))
}

// headerBar renders the top chrome: app chip, breadcrumb, right-aligned info.
func headerBar(width int, crumb, right string) string {
	left := sAppTitle.Render("workflow") + sCrumb.Render(crumb)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + sDim.Render(right)
}

// panel wraps content in a rounded border with a title line above it.
func panel(title, content string, width int, hot bool) string {
	st := sPanel
	if hot {
		st = sPanelHot
	}
	body := st.Width(width).Render(content)
	if title == "" {
		return body
	}
	return " " + sPanelName.Render(title) + "\n" + body
}

func truncate(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	if n <= 1 {
		return string([]rune(s)[:n])
	}
	r := []rune(s)
	if len(r) > n-1 {
		r = r[:n-1]
	}
	return string(r) + "…"
}
