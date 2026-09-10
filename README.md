# agentbar

A tmux sidebar for your running Claude Code sessions: what each one is
doing, where it is, and one key to jump there.

![agentbar](docs/screenshot.png)

## Install

```sh
go install github.com/ssnxd/agentbar/cmd/agentbar@latest
agentbar install
```

Add to `tmux.conf` and reload:

```tmux
run-shell "$HOME/.local/bin/agentbar tmux-init"
```

Needs tmux 3.2+ and Claude Code 2.1.26x+.

## Use

- `prefix a` shows the sidebar in every window, or hides it.
- `prefix A` focuses the sidebar, or takes you back.
- In the sidebar: `j`/`k` move, `enter` jump, `1`-`9` jump to that card,
  `tab` next session that needs you, `y` accept a permission prompt, `x`
  kill, `/` filter, `q` close.
- Each card: its number, repo and branch, the status with how long it has
  sat there (or the context size while working), the title, then what it
  needs you for or what it is doing right now.
- Running subagents show under their session: the task and the last tool.

After upgrading, run `agentbar install` again to add any new hooks.

Options for `tmux.conf`: `@agentbar-key`, `@agentbar-focus-key`,
`@agentbar-side` (`left`/`right`), `@agentbar-width`, `@agentbar-bell`
(`on` rings the bell in a session's pane when it needs you, so tmux flags
the window).

With the sidebar hidden, the count still reaches your status line:

```tmux
set -g status-right '#{@agentbar_status} '
```

`@agentbar_status` reads like `5 · 2 need you`; `@agentbar_hot` is the
bare count for conditionals such as `#{?#{@agentbar_hot},#[fg=orange],}`.

Something off? `agentbar doctor`.
