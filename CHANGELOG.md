# Changelog

All notable changes to workflow will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses semantic versioning. Before v1.0.0 minor versions may
change behavior.

## [Unreleased]

## [0.0.1] - 2026-08-07

Initial public release.

### Added

- Task → orchestrator → worker agent model on a dedicated tmux server, with
  a git worktree and branch per agent and orchestrator-driven merges into a
  reviewable task branch.
- Hook-driven agent status (working / idle / needs-you / error / done / dead)
  recorded in SQLite, with statusline telemetry (cost, context usage, diff
  size) per agent.
- Bubble Tea v2 TUI: dashboard, task view with live pane preview,
  which-key style contextual keymap footer with `?` overlay, ASCII home
  banner.
- New-task wizard with an fzf repo picker (git-log preview; built-in fuzzy
  fallback) over repos discovered under `repo_roots`.
- External Claude session monitoring: title, model, permission mode,
  derived state (working / waiting / blocked?), context size, and
  jump-to-session in the user's own tmux.
- Attach/detach round-trip (`Ctrl-Q`) that works in plain terminals and
  nested inside the user's tmux.
- Recovery of dead agents after reboot via `claude --resume`; startup
  reconciliation of DB state against the tmux server.
- File-inbox messaging between agents (`workflow msg`), with idle-nudge
  delivery; `workflow agent spawn/list/done` plumbing; `workflow doctor`
  preflight; `workflow new` scripted task creation.
- Yolo mode: agents launch with `bypassPermissions` by default
  (`permission_mode` in config.json).
