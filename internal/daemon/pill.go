package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/ssnxd/agentbar/internal/session"
	"github.com/ssnxd/agentbar/internal/state"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

// The indicator is one pill for the status line, published as a ready
// tmux format in @agentbar_pill. Place it with #{E:@agentbar_pill}.
//
//	● 1 needs you 12m   filled: a session needs you, with the age of the
//	                    oldest request; "● 1" on clients under 90 columns
//	◐ 2 ○ 1             soft: sessions run or wait, nothing is asked of you
//	(empty)             no sessions
//
// The whole pill is a click range named "agentbar". Two options the user
// sets give it the caps of their own status line:
//
//	@agentbar-cap-left, @agentbar-cap-right   e.g. "" and ""
//
// Without caps the pill is a plain block padded by one space.
const (
	PillOption = "@agentbar_pill"
	PillRange  = "agentbar"
	// below this client width the filled pill drops its label
	pillWide = 90
)

// Catppuccin Mocha, as in the sidebar.
const (
	pCrust    = "#11111b"
	pSurface0 = "#313244"
	pOverlay2 = "#9399b2"
	pPeach    = "#fab387"
	pBlue     = "#89b4fa"
	pRed      = "#f38ba8"
)

// Pill renders the indicator for a snapshot; empty with no sessions.
func Pill(s Snapshot) string {
	var hot, working, waiting, failed int
	var oldest time.Time
	for _, ss := range s.Sessions {
		switch ss.Status {
		case state.StatusNeedsYou:
			hot++
			if !ss.LastActivity.IsZero() && (oldest.IsZero() || ss.LastActivity.Before(oldest)) {
				oldest = ss.LastActivity
			}
		case state.StatusWorking:
			working++
		case state.StatusWaiting:
			waiting++
		case state.StatusError:
			failed++
		}
	}
	switch {
	case hot > 0:
		short := fmt.Sprintf("● %d", hot)
		long := short + " needs you"
		if hot > 1 {
			long = short + " need you"
		}
		if a := session.Age(s.At, oldest); a != "" {
			long += " " + a
		}
		body := fmt.Sprintf("#{?#{e|>=:#{client_width},%d},%s,%s}", pillWide, long, short)
		return pill(pPeach, "fg="+pCrust+" bold", body)
	case working+waiting+failed > 0:
		var segs []string
		seg := func(n int, fg, icon string) {
			if n > 0 {
				segs = append(segs, fmt.Sprintf("#[fg=%s]%s %d", fg, icon, n))
			}
		}
		seg(failed, pRed, "✗")
		seg(working, pBlue, "◐")
		seg(waiting, pOverlay2, "○")
		return pill(pSurface0, "fg="+pOverlay2, strings.Join(segs, " "))
	}
	return ""
}

// pill wraps body in a block of colour bg with the user's caps, as one
// click range, followed by the space that parts it from what comes next.
func pill(bg, style, body string) string {
	edge := "#[fg=" + bg + " bg=default nobold]"
	fill := "#[" + style + " bg=" + bg + "]"
	return "#[range=user|" + PillRange + "]" +
		edge + "#{@agentbar-cap-left}" +
		fill + "#{?@agentbar-cap-left,, }" + body + fill + "#{?@agentbar-cap-right,, }" +
		edge + "#{@agentbar-cap-right}" +
		"#[norange default] "
}

// StatusReads reports whether the user's status line reads any option the
// daemon publishes. While it does, the daemon must outlive the viewers.
func StatusReads(r tmuxctl.Runner) bool {
	for _, o := range []string{"status-right", "status-left", "status-format"} {
		if v, err := r.Run("show-options", "-gv", o); err == nil && strings.Contains(v, "@agentbar_") {
			return true
		}
	}
	return false
}

// refreshStatus redraws the status line of every client now, so a change
// does not wait for status-interval.
func refreshStatus(r tmuxctl.Runner) {
	out, err := r.Run("list-clients", "-F", "#{client_name}")
	if err != nil {
		return
	}
	for _, c := range strings.Fields(out) {
		_, _ = r.Run("refresh-client", "-S", "-t", c)
	}
}

// Once reads one snapshot from a running daemon.
func Once(sock string, wait time.Duration) (Snapshot, error) {
	var s Snapshot
	c, err := dial(sock, wait)
	if err != nil {
		return s, err
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(wait))
	err = decodeLine(c, &s)
	return s, err
}
