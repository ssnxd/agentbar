// workflow — a tmux-based manager for Claude Code agent fleets.
//
//	workflow                 launch the TUI
//	workflow event           (hook plumbing) ingest a Claude Code hook
//	workflow statusline      (hook plumbing) statusline + telemetry
//	workflow run-agent       (pane plumbing) exec claude for an agent
//	workflow agent ...       (agent-facing) spawn | list | done
//	workflow msg ...         (agent-facing) send | read
//	workflow doctor          environment preflight
package main

import (
	"fmt"
	"os"

	"github.com/ssnxd/workflow/internal/cli"
	"github.com/ssnxd/workflow/internal/tui"
)

// version is stamped by the build (-ldflags "-X main.version=v1.2.3").
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		tui.Run()
		return
	}
	switch args[0] {
	case "event":
		cli.Event(args[1:])
	case "statusline":
		cli.Statusline()
	case "run-agent":
		cli.RunAgent(args[1:])
	case "agent":
		cli.Agent(args[1:])
	case "msg":
		cli.Msg(args[1:])
	case "task":
		cli.Task(args[1:])
	case "new":
		cli.NewTask(args[1:])
	case "doctor":
		cli.Doctor()
	case "version", "-v", "--version":
		fmt.Println("workflow", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "workflow: unknown command %q\n%s", args[0], usage)
		os.Exit(1)
	}
}

const usage = `workflow — manage Claude Code agent fleets on tmux

usage:
  workflow                      launch the TUI
  workflow new --repo ... --title ... --prompt ...
  workflow task quit [--yes] <id>   kill a task's agents, keep its branches
  workflow doctor               check tmux/claude/git prerequisites
  workflow version              print the version
  workflow agent ...            (used by agents) spawn | list | done
  workflow msg ...              (used by agents) send | read
  workflow task done ...        (used by the orchestrator)
`
