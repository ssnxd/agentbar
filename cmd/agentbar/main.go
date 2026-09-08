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
	cmd := "ui"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	switch cmd {
	case "ui":
		ui.Run()
	case "event":
		cli.Event(args)
	case "toggle":
		cli.Toggle(args)
	case "jump":
		cli.Jump(args)
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
  agentbar                      run the sidebar UI (normally launched by toggle)
  agentbar toggle               open / focus / move / close the sidebar (bind to prefix a)
  agentbar jump <pane-id>       move the sidebar next to a pane and focus the pane
  agentbar install [--uninstall]   add (or remove) agentbar hooks in ~/.claude/settings.json
  agentbar doctor               check tmux, hooks, and Claude's session registry
  agentbar event                (hook plumbing) receive a Claude Code hook on stdin
  agentbar version
`
