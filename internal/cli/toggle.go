package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

// exePath is the absolute, symlink-resolved path of this binary. Hooks and
// the sidebar pane command must not depend on PATH.
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

// Toggle is bound to prefix a. An optional pane id argument names the pane
// the key was pressed in (run-shell expands #{pane_id}); otherwise
// TMUX_PANE is used.
func Toggle(args []string) {
	requireTmux()
	hint := ""
	if len(args) > 0 {
		hint = args[0]
	}
	if hint == "" {
		hint = os.Getenv("TMUX_PANE")
	}
	r := tmuxctl.Exec{}
	o := tmuxctl.LoadOpts(r)
	o.Cmd = exePath() + " ui"
	if err := tmuxctl.Toggle(r, o, hint); err != nil {
		fmt.Fprintln(os.Stderr, "agentbar:", err)
		os.Exit(1)
	}
}

// Jump moves the sidebar next to a pane and focuses that pane.
func Jump(args []string) {
	requireTmux()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: agentbar jump <pane-id>")
		os.Exit(2)
	}
	r := tmuxctl.Exec{}
	if err := tmuxctl.Jump(r, tmuxctl.LoadOpts(r), args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "agentbar:", err)
		os.Exit(1)
	}
}
