package tmuxctl

import "fmt"

// Action is what a toggle keypress does given where the sidebar is.
type Action int

const (
	ActionOpen  Action = iota // no sidebar anywhere: open one here
	ActionClose               // sidebar here and focused: close it
	ActionFocus               // sidebar here, not focused: focus it
	ActionMove                // sidebar in another window: bring it here
)

func (a Action) String() string {
	return [...]string{"open", "close", "focus", "move"}[a]
}

// Situation is the input to Decide.
type Situation struct {
	Sidebar       *Pane // nil when none exists
	CurrentPane   string
	CurrentWindow string
}

// Decide implements the toggle table from the spec.
func Decide(s Situation) Action {
	switch {
	case s.Sidebar == nil:
		return ActionOpen
	case s.Sidebar.PaneID == s.CurrentPane:
		return ActionClose
	case s.Sidebar.WindowID == s.CurrentWindow:
		return ActionFocus
	default:
		return ActionMove
	}
}

// Toggle runs the toggle action for the pane the key was pressed in.
func Toggle(r Runner, o Opts, curHint string) error {
	panes, err := ListPanes(r)
	if err != nil {
		return err
	}
	cur, curWin, err := Current(r, curHint)
	if err != nil {
		return err
	}
	sb := FindSidebar(panes)
	switch Decide(Situation{Sidebar: sb, CurrentPane: cur, CurrentWindow: curWin}) {
	case ActionOpen:
		return open(r, o, cur)
	case ActionClose:
		_, err := r.Run("kill-pane", "-t", sb.PaneID)
		return err
	case ActionFocus:
		return SelectPane(r, sb.PaneID)
	default:
		if err := join(r, o, sb.PaneID, cur); err != nil {
			return err
		}
		return SelectPane(r, sb.PaneID)
	}
}

// open splits a new sidebar pane beside cur, tags it, and gives it the
// window-active-style so the user's inactive-pane dimming leaves it alone.
func open(r Runner, o Opts, cur string) error {
	args := append([]string{"split-window"}, splitFlags(o)...)
	args = append(args, "-t", cur, "-P", "-F", "#{pane_id}", o.Cmd)
	id, err := r.Run(args...)
	if err != nil {
		return err
	}
	if _, err := r.Run("set-option", "-p", "-t", id, "@agentbar", "1"); err != nil {
		return err
	}
	if style, err := r.Run("show-options", "-gqv", "window-active-style"); err == nil && style != "" {
		_, _ = r.Run("select-pane", "-t", id, "-P", style)
	}
	return nil
}

// join moves the sidebar pane next to target.
func join(r Runner, o Opts, sidebar, target string) error {
	args := append([]string{"join-pane"}, splitFlags(o)...)
	args = append(args, "-s", sidebar, "-t", target)
	_, err := r.Run(args...)
	return err
}

// Follow brings the sidebar into window without stealing focus. It runs
// from tmux's session-window-changed hook, so a plain window switch keeps
// the sidebar in view. No sidebar, or already here: no-op.
func Follow(r Runner, o Opts, window string) error {
	panes, err := ListPanes(r)
	if err != nil {
		return err
	}
	sb := FindSidebar(panes)
	if sb == nil || sb.WindowID == window {
		return nil
	}
	var target string
	for _, p := range panes {
		if p.WindowID == window && p.Active {
			target = p.PaneID
			break
		}
	}
	if target == "" {
		return nil // window vanished between the hook and now
	}
	args := append([]string{"join-pane", "-d"}, splitFlags(o)...)
	args = append(args, "-s", sb.PaneID, "-t", target)
	_, err = r.Run(args...)
	return err
}

// Jump focuses target and brings the sidebar along into its window. When the
// target is in another tmux session the client is switched there first.
func Jump(r Runner, o Opts, target string) error {
	panes, err := ListPanes(r)
	if err != nil {
		return err
	}
	tp := FindPane(panes, target)
	if tp == nil {
		return fmt.Errorf("pane %s no longer exists", target)
	}
	sb := FindSidebar(panes)
	if sb != nil && sb.WindowID != tp.WindowID {
		if err := join(r, o, sb.PaneID, target); err != nil {
			return err
		}
	}
	if sb != nil && sb.SessionName != tp.SessionName {
		if _, err := r.Run("switch-client", "-t", tp.SessionName); err != nil {
			return err
		}
	}
	if _, err := r.Run("select-window", "-t", tp.WindowID); err != nil {
		return err
	}
	return SelectPane(r, target)
}
