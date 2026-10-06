# Changelog

## Unreleased

- `✓ done`: a session that finishes while you are not looking stays done,
  in green, until its pane has been on your screen for a second. The
  indicator counts it (`✓ 2 ◐ 5 ○ 1`), the picker opens on it when
  nothing is asked, and `tab` goes through the done sessions then.
- `@agentbar_win` and `@agentbar_sess` mark the windows and tmux sessions
  that hold a session needing you, for your own status line.
- `@agentbar-notify on`: a sound when a session needs you or is done, and
  a desktop notification through the terminal while you are in another
  app. Nothing when its pane is on your screen. `@agentbar-sound off`
  drops the sound.
- `agentbar doctor` checks `allow-passthrough` when notifications are on.

## 0.1.0 (2026-10-01)

The project is now agentbar. It plugs Claude into tmux so you know when
you're needed, without getting in your way. The previous `workflow`
orchestrator (tasks, worktrees, agent-to-agent messaging, dashboard) is
removed; it lives on in git history before this release.

- An indicator in the status line (`@agentbar_pill`): filled with the age
  of the oldest request when a session needs you, soft while sessions
  work or wait, gone with no sessions.
- `prefix a`, or a click on the indicator, opens a picker in the centre:
  type to search, `enter` jumps, `tab` goes to the next request, `ctrl-y`
  accepts a permission prompt, `ctrl-x` kills.
- Status-first cards: repo and branch, status with its age or the context
  size, title, then the request or the current tool. Running subagents
  show under their session with their task and last tool.
- `prefix A` opens the same list as a sidebar in every window.
- `@agentbar_status` and `@agentbar_hot` for your own status line; an
  optional bell when a session needs you.
- Sessions come from Claude Code's own registry
  (`~/.claude/sessions/*.json`), including their exact tmux pane. Status
  comes from observe-only hooks installed by `agentbar install`, with
  registry and transcript fallbacks.
- One daemon builds the snapshot for every view and outlives them, so the
  indicator stays current with no view open.
- `agentbar doctor`, and the `agentbar.tmux` plugin script with
  `@agentbar-*` options.
