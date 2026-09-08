package cli

import (
	"fmt"
	"os"

	"github.com/ssnxd/agentbar/internal/tmuxctl"
)

// TmuxInit installs the key binding and the follow hooks into the running
// tmux server. tmux.conf runs it once via `run-shell "<exe> tmux-init"`, so
// the config depends only on the binary location, never on the repo.
//
// Options read from tmux (set -g before the run-shell line):
//
//	@agentbar-key     key after prefix (default a)
//	@agentbar-follow  on|off: move the sidebar on every window switch (default on)
func TmuxInit() {
	r := tmuxctl.Exec{}
	exe := exePath()
	key := "a"
	if v, err := r.Run("show-options", "-gqv", "@agentbar-key"); err == nil && v != "" {
		key = v
	}
	if _, err := r.Run("bind-key", key, "run-shell", exe+" toggle #{pane_id}"); err != nil {
		fatal(err)
	}
	follow := true
	if v, err := r.Run("show-options", "-gqv", "@agentbar-follow"); err == nil && (v == "off" || v == "0" || v == "false") {
		follow = false
	}
	// -b so the hook never blocks tmux; follow is a no-op unless a sidebar
	// exists and is elsewhere.
	cmd := "run-shell -b '" + exe + " follow #{window_id}'"
	for _, hook := range []string{"session-window-changed", "client-session-changed"} {
		if follow {
			if _, err := r.Run("set-hook", "-g", hook, cmd); err != nil {
				fatal(err)
			}
		} else {
			_, _ = r.Run("set-hook", "-gu", hook)
		}
	}
	if os.Getenv("AGENTBAR_VERBOSE") != "" {
		fmt.Printf("bound prefix %s; follow=%v\n", key, follow)
	}
}

// Follow is the hook target: bring the sidebar into the given window.
func Follow(args []string) {
	if len(args) != 1 {
		os.Exit(2)
	}
	r := tmuxctl.Exec{}
	if err := tmuxctl.Follow(r, tmuxctl.LoadOpts(r), args[0]); err != nil {
		// Hook context: nowhere useful to print. Log and exit 0.
		logf("follow %s: %v", args[0], err)
	}
}
