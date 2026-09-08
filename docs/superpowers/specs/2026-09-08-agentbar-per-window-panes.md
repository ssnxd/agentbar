# agentbar v2: one sidebar pane per window, one daemon

Date: 2026-09-08. Supersedes the "sidebar follows you" part of the
original design. Everything below the UI (hooks, registry, transcript,
merge, rendering) is unchanged.

## Why

Moving a single pane between windows with join-pane resizes the main pane
in both windows on every switch, so the app in it repaints and tmux
re-lays both windows: a visible flick. Every mature tmux sidebar (workmux,
tmux-pane-tree, tmux-agent-sidebar) avoids it the same way: a sidebar pane
in every window, and nothing ever moves.

## Shape

```
prefix a  ──▶ agentbar toggle ──▶ enabled? close all : open in every window
prefix A  ──▶ agentbar focus  ──▶ in sidebar? back to last pane : focus sidebar

tmux hooks (installed by agentbar tmux-init, all run-shell -b):
  after-new-window[97], after-new-session[97]  agentbar ensure #{window_id}
  window-resized[97]                           agentbar reflow
  after-kill-pane[97], pane-exited[97]         agentbar sweep
  after-select-window[97], client-session-changed[97]  agentbar nudge

Observed on tmux 3.7: in after-new-window `#{window_id}` is the new window
and `#{hook_window}` is empty; in after-kill-pane neither names the
affected window, so `sweep` scans every window for one left holding only a
sidebar.

agentbar daemon  ──socket──▶  agentbar view (one per sidebar pane)
```

- **Enabled flag:** global tmux option `@agentbar-enabled` (1/0). Hooks
  are always installed; `ensure` is a no-op when disabled.
- **Panes:** tagged `@agentbar=1`, created with
  `split-window -h -b -f -d -l W -t <window's active pane> -P -F #{pane_id}
  "<exe> view"` (right side drops `-b`). `-d` keeps focus where it was,
  `-f` spans the full window height. Width pinned with `resize-pane -x W`
  after creation and on every `window-resized`. Windows narrower than
  `W + 40` columns are skipped.
- **Daemon:** `agentbar daemon` listens on
  `~/.local/share/agentbar/daemon.sock`, writes `daemon.pid`, builds the
  snapshot (registry + hook state + transcripts + tmux panes) every second
  or immediately on SIGUSR1, and broadcasts it as one JSON line per
  snapshot to every connected viewer. It exits by itself after 60 s with
  no viewers. Single instance: a live socket means running.
- **Viewer:** `agentbar view` connects to the socket (starting the daemon
  if the connect fails), renders snapshots, and runs actions directly:
  jump (`switch-client` if another session, `select-window`,
  `select-pane`), accept (`send-keys Enter`), kill (SIGTERM). `q` closes
  every sidebar. A viewer whose window is not current skips its spinner
  tick so idle windows cost nothing.
- **pane-exited:** when a window is left with only sidebar panes, they are
  killed so the window closes as it would have without agentbar.
- **Sessions:** open/close act on all windows of all sessions; the ensure
  hook covers new windows anywhere.

## Keys (unchanged inside the sidebar)

j/k move · enter jump · tab next needs-you · y accept · x kill · / filter
· r refresh · ? help · q close all sidebars.

## Testing

Fake-runner tests for open-all, close-all, ensure idempotence, narrow-window
skip, pane-exited, focus, jump across sessions. Daemon round-trip test on a
temp socket. Live: open in all four windows, switch windows and confirm no
main-pane resize, new window gets a sidebar, shell exit closes a
sidebar-only window, `prefix A` focus round trip, `q` closes all.
