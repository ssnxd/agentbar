# Changelog

## 0.1.0 (unreleased)

The project is now agentbar: a tmux sidebar for Claude Code sessions you
run yourself. The previous `workflow` orchestrator (tasks, worktrees,
agent-to-agent messaging, dashboard) is removed; it lives on in git history
before this release.

- `prefix a` opens a sidebar pane in every window (or closes them all);
  `prefix A` focuses the sidebar or returns to your pane. New windows get
  one automatically; a window left holding only a sidebar closes itself.
- One daemon builds the session snapshot and streams it to every sidebar
  over a Unix socket, so panes never move and switching windows never
  resizes anything.
- Sessions discovered from Claude Code's own registry
  (`~/.claude/sessions/*.json`), including their exact tmux pane.
- Status from observe-only hooks installed by `agentbar install`, with
  registry and transcript fallbacks for sessions started before install.
- Jump (sidebar follows), accept a permission prompt, kill, filter.
- `agentbar doctor`, `agentbar.tmux` plugin script with `@agentbar-*` options.
