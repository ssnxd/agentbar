// agentbar — a tmux sidebar for running Claude Code sessions.
package main

import (
	"fmt"
	"os"

	"github.com/ssnxd/agentbar/internal/cli"
	"github.com/ssnxd/agentbar/internal/ui"
)

// version is stamped by the build (-ldflags "-X main.version=v1.2.3").
var version = "dev"

func main() {
	args := os.Args[1:]
	cmd := "help"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	switch cmd {
	// user-facing
	case "toggle":
		cli.Toggle(args)
	case "focus":
		cli.Focus(args)
	case "close":
		cli.Close()
	case "install":
		cli.Install(args)
	case "doctor":
		cli.Doctor()
	case "tmux-init":
		cli.TmuxInit()
	case "jump":
		cli.Jump(args)
	// plumbing
	case "view", "ui":
		ui.Run()
	case "daemon":
		cli.Daemon()
	case "event":
		cli.Event(args)
	case "ensure":
		cli.Ensure(args)
	case "reflow":
		cli.Reflow()
	case "sweep":
		cli.Sweep()
	case "nudge":
		cli.Nudge()
	case "version", "-v", "--version":
		fmt.Println("agentbar", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "agentbar: unknown command %q\n%s", cmd, usage)
		os.Exit(1)
	}
}

const usage = `agentbar — a tmux sidebar for running Claude Code sessions

usage:
  agentbar toggle               sidebars in every window, or none (bind to prefix a)
  agentbar focus                into the sidebar / back to your pane (bind to prefix A)
  agentbar close                remove every sidebar
  agentbar install [--uninstall]   add (or remove) agentbar hooks in ~/.claude/settings.json
  agentbar tmux-init            bind keys and install tmux hooks (run-shell this from tmux.conf)
  agentbar doctor               check tmux, hooks, and Claude's session registry
  agentbar jump <pane-id>       focus a pane (switching session/window as needed)
  agentbar version

plumbing (run by tmux hooks and the sidebar itself):
  view · daemon · event · ensure <window> · reflow · sweep · nudge
`
