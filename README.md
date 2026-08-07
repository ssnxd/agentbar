# workflow

A terminal UI that manages fleets of Claude Code agents on top of tmux.

You give it a **task**: a git repository plus a prompt. The task starts an
**orchestrator** — a real, interactive Claude Code session — which plans the
work, picks models, and spawns **worker** agents. Every agent runs in its own
tmux window on a dedicated tmux server. You attach to any agent with one key,
interact, press `Ctrl-Q`, and land back in the dashboard.

## Requirements

- tmux >= 3.2
- Claude Code CLI (`claude`) in PATH
- git

Check with:

```
workflow doctor
```

## Install

From a release: grab a binary from the GitHub releases page, or:

```
go install github.com/ssnxd/workflow/cmd/workflow@latest
```

From source:

```
make install     # builds with version info into $GOPATH/bin
```

Put the binary anywhere. Agents find it automatically: the spawner prepends
its own location to each agent's PATH. Run `workflow doctor` to verify the
environment.

## Use

```
workflow            # the TUI
workflow new --repo ~/code/myrepo --title "fix auth" --prompt "..."   # scripted
```

Starting a task is a two-step wizard. Step one picks the repository: if
**fzf** is installed, your own fzf takes over the terminal — with a git-log
preview pane per repo — otherwise a built-in fuzzy picker runs. Candidates
are known projects first, then git repos discovered under `repo_roots` from
config.json (branch and recency read without exec'ing git). Step two is
title + prompt with a live summary card of exactly what will be created.

The dashboard also lists **external Claude sessions** — `claude` processes you
started yourself in other terminals. Each shows its conversation title, model,
permission mode, tmux location, uptime, context size, and a derived state:
`working` (transcript streaming), `waiting` (turn closed — it wants your
input), or `blocked?` (turn open but silent — often a permission prompt).
`Enter` jumps to the session in your own tmux. Anything running under
workflow's worktrees is excluded, so managed agents never appear here.

You never need to memorize keys: the footer always shows exactly the keys
that work right now (which-key style, context-aware — e.g. `r recover` only
appears on a dead agent), and `?` expands the full grouped keymap. The core
set: `enter` open/attach/jump · `Ctrl-Q` detach · `n` new task · `r` recover ·
`x` archive · `esc` back · `q` quit.

## How it works

- **tmux owns every agent process.** One dedicated server (`tmux -L workflow`),
  one tmux session per task, one window per agent, never panes. Agents survive
  TUI restarts and crashes. After a reboot, dead agents recover with
  `claude --resume`.
- **Status comes from Claude Code hooks, not pane scraping.** Each agent is
  launched with settings that pipe `SessionStart`, `Stop`, `PermissionRequest`,
  `Notification`, and friends into `workflow event`, which writes to SQLite.
  The TUI reads the DB. Statuses: starting, working, idle, needs-you, error,
  done, ended, dead.
- **Each agent gets a git worktree and a branch.** Workers branch off the task
  branch (`wf/<id>-<slug>--<agent>`); the orchestrator's worktree is checked out
  on the task branch and merges reviewed worker branches into it. You review
  the task branch.
- **Agents communicate through file inboxes**, not typed keystrokes. `workflow
  msg send/read` moves the payload; the only send-keys in the app is a short
  "you have mail" nudge to an *idle* session.
- **Two skills teach the agents the system**: `workflow-comms` (protocol,
  every agent) and `workflow-orch` (playbook, orchestrator), injected via
  `--add-dir`. Identity (name, task, role) rides in `--append-system-prompt`,
  which survives context compaction.

## Layout on disk

```
~/.local/share/workflow/
  workflow.db      state and history (SQLite)
  config.json      models, worktree copy globs, extra permission rules
  tmux.conf        managed server config (regenerated at startup)
  claude/          skills injected into agents
  worktrees/<task>/<agent>/
  tasks/<task>/    inboxes and per-agent launch material
```

The dedicated tmux server is invisible to your own tmux. To poke at it
directly: `tmux -L workflow ls`.
