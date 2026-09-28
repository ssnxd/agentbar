package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ssnxd/agentbar/internal/daemon"
	"github.com/ssnxd/agentbar/internal/tmuxctl"
	"github.com/ssnxd/agentbar/internal/ui"
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
	// the status line may still show what the daemon publishes
	if !daemon.StatusReads(r) {
		_ = daemon.Stop()
	}
}

// popupChrome is the rows and columns the popup's border takes.
const popupChrome = 2

// popupWidth is the width of the list inside the popup: wider than a
// sidebar, since it takes no room from the panes.
const popupWidth = 64

// Popup (prefix a, or a click on the indicator) floats the session list
// in the centre of the client, as a picker sized to its content. args: the client to show
// it on and the pane the user is in, both expanded by tmux in the binding.
func Popup(args []string) {
	requireTmux()
	r := tmuxctl.Exec{}
	var client, here string
	if len(args) > 0 {
		client = args[0]
	}
	if len(args) > 1 {
		here = args[1]
	}
	if client == "" {
		c, err := r.Run("display-message", "-p", "#{client_name}")
		if err != nil || c == "" {
			fatal(fmt.Errorf("popup: no tmux client"))
		}
		client = c
	}
	if err := daemon.EnsureRunning(exePath()); err != nil {
		// run-shell would put a failure in the user's pane: say it in
		// the status line instead
		_, _ = r.Run("display-message", "-c", client, "agentbar: "+err.Error())
		return
	}
	w := popupWidth
	cw, ch := 0, 0
	if v, err := r.Run("display-message", "-p", "-c", client, "#{client_width} #{client_height}"); err == nil {
		_, _ = fmt.Sscan(v, &cw, &ch)
	}
	// keep a margin of the pane in sight on every side
	if cw > 0 && w > cw-popupChrome-4 {
		w = cw - popupChrome - 4
	}
	h := 12
	if s, err := daemon.Once(daemon.SocketPath(), time.Second); err == nil {
		h = ui.PopupHeight(s.Sessions, w)
	}
	h += popupChrome
	if ch > 0 && h > ch-4 {
		h = ch - 4
	}
	cmd := fmt.Sprintf("%s view --popup --client %s --here %s", shellQuote(exePath()), shellQuote(client), shellQuote(here))
	// display-popup returns when the popup closes; an error here is a
	// popup that is already open
	_, _ = r.Run("display-popup", "-E", "-c", client, "-x", "C", "-y", "C",
		"-w", strconv.Itoa(w+popupChrome), "-h", strconv.Itoa(h),
		"-b", "rounded", "-S", "fg=#45475a", cmd)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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

// statusClick is tmux's own binding for a click on the status line.
const statusClick = "switch-client -t ="

// bindStatusClick makes a click on the indicator open the popup and leaves
// every other click on the status line as tmux has it. A binding the user
// wrote themselves is not replaced. Reports whether the click is bound.
func bindStatusClick(r tmuxctl.Runner, popup string) bool {
	out, _ := r.Run("list-keys", "-T", "root", "MouseDown1Status")
	if f := strings.Fields(out); len(f) > 4 {
		if cur := strings.Join(f[4:], " "); cur != statusClick && !strings.Contains(cur, " popup ") {
			return false
		}
	}
	_, err := r.Run("bind-key", "-T", "root", "MouseDown1Status", "if-shell", "-F",
		"#{==:#{mouse_status_range},"+daemon.PillRange+"}", popup, statusClick)
	return err == nil
}

// TmuxInit installs key bindings and hooks into the running tmux server.
// tmux.conf runs it once via `run-shell "<exe> tmux-init"`.
//
//	@agentbar-key          popup key after prefix (default a)
//	@agentbar-focus-key    sidebar focus key after prefix (default A)
//	@agentbar-sidebar-key  sidebar toggle key after prefix (default none)
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
	popup := "run-shell -b '" + exe + " popup #{client_name} #{pane_id}'"
	if _, err := r.Run("bind-key", key, "run-shell", "-b", exe+" popup #{client_name} #{pane_id}"); err != nil {
		fatal(err)
	}
	if v, err := r.Run("show-options", "-gqv", "@agentbar-sidebar-key"); err == nil && v != "" {
		if _, err := r.Run("bind-key", v, "run-shell", exe+" toggle #{pane_id}"); err != nil {
			fatal(err)
		}
	}
	if !bindStatusClick(r, popup) {
		logf("tmux-init: MouseDown1Status is bound by you; the indicator is not clickable")
	}
	// the status line shows the indicator: it needs the daemon from the start
	if daemon.StatusReads(r) {
		if err := daemon.EnsureRunning(exe); err != nil {
			logf("tmux-init: %v", err)
		}
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
