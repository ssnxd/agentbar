package tmuxctl

import (
	"fmt"
	"strconv"
)

// Per-window sidebar management. One tagged sidebar pane per window; panes
// never move between windows, so nothing else ever resizes on a switch.

const (
	TagOption     = "@agentbar"
	EnabledOption = "@agentbar-enabled"
	// minSpare is how many columns the main pane must keep; narrower
	// windows get no sidebar.
	minSpare = 40
)

// Enabled reports the global on/off flag.
func Enabled(r Runner) bool {
	v, err := r.Run("show-options", "-gqv", EnabledOption)
	return err == nil && v == "1"
}

// SetEnabled sets the global on/off flag.
func SetEnabled(r Runner, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	_, err := r.Run("set-option", "-g", EnabledOption, v)
	return err
}

// Sidebars returns every sidebar pane.
func Sidebars(ps []Pane) []Pane {
	var out []Pane
	for _, p := range ps {
		if p.Sidebar {
			out = append(out, p)
		}
	}
	return out
}

// SidebarIn returns the sidebar pane in window, or nil.
func SidebarIn(ps []Pane, window string) *Pane {
	for i := range ps {
		if ps[i].Sidebar && ps[i].WindowID == window {
			return &ps[i]
		}
	}
	return nil
}

// mainPane returns the active non-sidebar pane of window, or nil.
func mainPane(ps []Pane, window string) *Pane {
	var first *Pane
	for i := range ps {
		p := &ps[i]
		if p.WindowID != window || p.Sidebar {
			continue
		}
		if p.Active {
			return p
		}
		if first == nil {
			first = p
		}
	}
	return first
}

// create splits a sidebar beside target without taking focus, tags it,
// styles it, and pins its width.
func create(r Runner, o Opts, target string) (string, error) {
	args := append([]string{"split-window"}, splitFlags(o)...)
	args = append(args, "-f", "-d", "-t", target, "-P", "-F", "#{pane_id}", o.Cmd)
	id, err := r.Run(args...)
	if err != nil {
		return "", err
	}
	if _, err := r.Run("set-option", "-p", "-t", id, TagOption, "1"); err != nil {
		return id, err
	}
	// Give the sidebar the active-window style so inactive-pane dimming
	// leaves it readable. set-option -p, not select-pane -P: the latter
	// would focus the pane and undo -d.
	if style, err := r.Run("show-options", "-gqv", "window-active-style"); err == nil && style != "" {
		_, _ = r.Run("set-option", "-p", "-t", id, "window-style", style)
	}
	return id, pinWidth(r, o, id)
}

func pinWidth(r Runner, o Opts, pane string) error {
	_, err := r.Run("resize-pane", "-t", pane, "-x", strconv.Itoa(o.Width))
	return err
}

// Ensure adds a sidebar to window if it has none and is wide enough.
func Ensure(r Runner, o Opts, window string) (bool, error) {
	ps, err := ListPanes(r)
	if err != nil {
		return false, err
	}
	return ensure(r, o, ps, window)
}

func ensure(r Runner, o Opts, ps []Pane, window string) (bool, error) {
	if SidebarIn(ps, window) != nil {
		return false, nil
	}
	mp := mainPane(ps, window)
	if mp == nil || mp.WindowWidth < o.Width+minSpare {
		return false, nil
	}
	_, err := create(r, o, mp.PaneID)
	return err == nil, err
}

// OpenAll adds a sidebar to every window of every session that lacks one.
func OpenAll(r Runner, o Opts) (int, error) {
	ps, err := ListPanes(r)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	n := 0
	for _, p := range ps {
		if seen[p.WindowID] {
			continue
		}
		seen[p.WindowID] = true
		ok, err := ensure(r, o, ps, p.WindowID)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// CloseAll kills every sidebar pane everywhere.
func CloseAll(r Runner) (int, error) {
	ps, err := ListPanes(r)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range Sidebars(ps) {
		if _, err := r.Run("kill-pane", "-t", p.PaneID); err == nil {
			n++
		}
	}
	return n, nil
}

// Reflow re-pins the width of every sidebar (window-resized hook).
func Reflow(r Runner, o Opts) error {
	ps, err := ListPanes(r)
	if err != nil {
		return err
	}
	for _, p := range Sidebars(ps) {
		if p.WindowWidth < o.Width+minSpare {
			continue
		}
		_ = pinWidth(r, o, p.PaneID)
	}
	return nil
}

// SweepEmpty closes sidebar panes in every window that has nothing else
// left, so such windows disappear as they would have without agentbar.
// It scans all windows because tmux's pane-exited and after-kill-pane
// hooks do not identify the affected window (hook_window is empty and
// window_id is the client's current window).
func SweepEmpty(r Runner) (int, error) {
	ps, err := ListPanes(r)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range Sidebars(ps) {
		if mainPane(ps, p.WindowID) != nil {
			continue
		}
		if _, err := r.Run("kill-pane", "-t", p.PaneID); err == nil {
			n++
		}
	}
	return n, nil
}

// FocusAction is what Focus decided to do.
type FocusAction int

const (
	FocusNone    FocusAction = iota // no sidebar in this window
	FocusSidebar                    // moved focus onto the sidebar
	FocusBack                       // was in the sidebar; went back to the last pane
)

// Focus toggles focus between the sidebar and the previously active pane.
func Focus(r Runner, curHint string) (FocusAction, error) {
	ps, err := ListPanes(r)
	if err != nil {
		return FocusNone, err
	}
	cur, win, err := Current(r, curHint)
	if err != nil {
		return FocusNone, err
	}
	sb := SidebarIn(ps, win)
	if sb == nil {
		return FocusNone, nil
	}
	if sb.PaneID == cur {
		_, err := r.Run("select-pane", "-l")
		if err != nil {
			// no last pane: fall back to the main pane
			if mp := mainPane(ps, win); mp != nil {
				err = SelectPane(r, mp.PaneID)
			}
		}
		return FocusBack, err
	}
	return FocusSidebar, SelectPane(r, sb.PaneID)
}

// Jump focuses target, switching session and window as needed. Sidebars
// stay where they are; the target window has its own.
func Jump(r Runner, target string) error {
	ps, err := ListPanes(r)
	if err != nil {
		return err
	}
	tp := FindPane(ps, target)
	if tp == nil {
		return fmt.Errorf("pane %s no longer exists", target)
	}
	cur, _, err := Current(r, "")
	if err == nil {
		if cp := FindPane(ps, cur); cp != nil && cp.SessionName != tp.SessionName {
			if _, err := r.Run("switch-client", "-t", tp.SessionName); err != nil {
				return err
			}
		}
	}
	if _, err := r.Run("select-window", "-t", tp.WindowID); err != nil {
		return err
	}
	return SelectPane(r, target)
}
