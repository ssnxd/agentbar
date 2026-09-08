package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ssnxd/agentbar/internal/daemon"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

// exePath is the absolute, symlink-resolved path of this binary. Hooks and
// pane commands must not depend on PATH.
func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "agentbar"
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe
}

func requireTmux() {
	if os.Getenv("TMUX") == "" && os.Getenv("TMUX_PANE") == "" {
		fmt.Fprintln(os.Stderr, "agentbar: run this from inside tmux")
		os.Exit(1)
	}
}

func opts(r tmuxctl.Runner) tmuxctl.Opts {
	o := tmuxctl.LoadOpts(r)
	o.Cmd = exePath() + " view"
	return o
}

func hint(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return os.Getenv("TMUX_PANE")
}

func openAll(r tmuxctl.Runner) {
	if err := tmuxctl.SetEnabled(r, true); err != nil {
		fatal(err)
	}
	if err := daemon.EnsureRunning(exePath()); err != nil {
		fatal(err)
	}
	if _, err := tmuxctl.OpenAll(r, opts(r)); err != nil {
		fatal(err)
	}
}

func closeAll(r tmuxctl.Runner) {
	_ = tmuxctl.SetEnabled(r, false)
	if _, err := tmuxctl.CloseAll(r); err != nil {
		fatal(err)
	}
	_ = daemon.Stop()
}

// Toggle (prefix a): sidebars everywhere, or none.
func Toggle(args []string) {
	requireTmux()
	r := tmuxctl.Exec{}
	if tmuxctl.Enabled(r) {
		closeAll(r)
		return
	}
	openAll(r)
}

// Close removes every sidebar (also what q in the sidebar runs).
func Close() {
	closeAll(tmuxctl.Exec{})
}

// Focus (prefix A): into the sidebar, or back to where you were. With no
// sidebar in this window it opens them everywhere and focuses this one.
func Focus(args []string) {
	requireTmux()
	r := tmuxctl.Exec{}
	h := hint(args)
	a, err := tmuxctl.Focus(r, h)
	if err != nil {
		fatal(err)
	}
	if a == tmuxctl.FocusNone {
		openAll(r)
		if _, err := tmuxctl.Focus(r, h); err != nil {
			fatal(err)
		}
	}
}

// Ensure is the after-new-window / after-new-session hook target.
func Ensure(args []string) {
	if len(args) != 1 {
		os.Exit(2)
	}
	r := tmuxctl.Exec{}
	if !tmuxctl.Enabled(r) {
		return
	}
	if _, err := tmuxctl.Ensure(r, opts(r), args[0]); err != nil {
		logf("ensure %s: %v", args[0], err)
	}
}

// Reflow is the window-resized hook target.
func Reflow() {
	r := tmuxctl.Exec{}
	if err := tmuxctl.Reflow(r, opts(r)); err != nil {
		logf("reflow: %v", err)
	}
}

// Sweep is the pane-exited / after-kill-pane hook target: close sidebars
// left alone in a window.
func Sweep() {
	if _, err := tmuxctl.SweepEmpty(tmuxctl.Exec{}); err != nil {
		logf("sweep: %v", err)
	}
}

// Nudge asks the daemon for a fresh snapshot (window/session change hooks).
func Nudge() {
	_ = daemon.Nudge()
}

// Jump moves focus to a pane (used by the sidebar; also handy from scripts).
func Jump(args []string) {
	requireTmux()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: agentbar jump <pane-id>")
		os.Exit(2)
	}
	if err := tmuxctl.Jump(tmuxctl.Exec{}, args[0]); err != nil {
		fatal(err)
	}
}

// hooks agentbar installs, all indexed at 97 so user hooks are untouched.
// Observed on tmux 3.7: in after-new-window, #{window_id} is the new
// window and #{hook_window} is empty; in after-kill-pane neither names the
// affected window, so sweep scans everything.
var tmuxHooks = [][2]string{
	{"after-new-window[97]", "ensure #{window_id}"},
	{"after-new-session[97]", "ensure #{window_id}"},
	{"window-resized[97]", "reflow"},
	{"after-kill-pane[97]", "sweep"},
	{"pane-exited[97]", "sweep"},
	{"after-select-window[97]", "nudge"},
	{"client-session-changed[97]", "nudge"},
}

// staleHooks were set by earlier versions (single moving pane).
var staleHooks = []string{"session-window-changed", "client-session-changed"}

// TmuxInit installs key bindings and hooks into the running tmux server.
// tmux.conf runs it once via `run-shell "<exe> tmux-init"`.
//
//	@agentbar-key        toggle key after prefix (default a)
//	@agentbar-focus-key  focus key after prefix (default A)
func TmuxInit() {
	r := tmuxctl.Exec{}
	exe := exePath()
	key, focusKey := "a", "A"
	if v, err := r.Run("show-options", "-gqv", "@agentbar-key"); err == nil && v != "" {
		key = v
	}
	if v, err := r.Run("show-options", "-gqv", "@agentbar-focus-key"); err == nil && v != "" {
		focusKey = v
	}
	if _, err := r.Run("bind-key", key, "run-shell", exe+" toggle #{pane_id}"); err != nil {
		fatal(err)
	}
	if _, err := r.Run("bind-key", focusKey, "run-shell", exe+" focus #{pane_id}"); err != nil {
		fatal(err)
	}
	for _, h := range staleHooks {
		_, _ = r.Run("set-hook", "-gu", h)
	}
	for _, h := range tmuxHooks {
		if _, err := r.Run("set-hook", "-g", h[0], "run-shell -b '"+exe+" "+h[1]+"'"); err != nil {
			fatal(err)
		}
	}
	if os.Getenv("AGENTBAR_VERBOSE") != "" {
		fmt.Printf("bound prefix %s (toggle), prefix %s (focus); %d hooks\n", key, focusKey, len(tmuxHooks))
	}
}
