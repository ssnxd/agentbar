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
- In the sidebar: `j`/`k` move, `enter` jump, `tab` next session that
  needs you, `y` accept a permission prompt, `x` kill, `/` filter, `q`
  close.

Options for `tmux.conf`: `@agentbar-key`, `@agentbar-focus-key`,
`@agentbar-side` (`left`/`right`), `@agentbar-width`.

Something off? `agentbar doctor`.
