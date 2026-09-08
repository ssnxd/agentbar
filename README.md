# agentbar

A tmux sidebar for the Claude Code sessions you run by hand.

You keep starting `claude` in tmux panes yourself. agentbar only adds
visibility: press `prefix a` and every window gets a narrow pane listing
every running session, grouped by project, with what it is doing right now
and where it lives in tmux. Press `enter` to jump to one. Nothing ever
moves between windows, so switching windows never resizes or repaints your
panes.

```
 claude ─────────────────── 4 · 1 needs you

 miivo-api
  ● needs you  fix auth middleware
    work:2.1 · fable · fix/auth       12m
    Bash git push origin main
  ◐ working    add rate limiter
    work:3.1 · opus · main             3m

 dotfiles
  ○ waiting    ghostty theme
    dot:1.1 · sonnet · main           40m

 enter jump  y accept  x kill  / filter  ?
```

| Status    | Meaning                                                     |
|-----------|-------------------------------------------------------------|
| needs you | a permission prompt, elicitation, or question is waiting    |
| working   | a turn is in progress                                       |
| waiting   | the turn ended; it wants your next instruction              |
| error     | the last turn failed (rate limit, API error)                |
| unknown   | discovered, but no hook data yet and nothing to infer from  |

## Requirements

- tmux >= 3.2
- Claude Code CLI (`claude`) 2.1.26x or newer
- Go 1.25+ to build from source

## Install

```
go install github.com/ssnxd/agentbar/cmd/agentbar@latest   # into $GOPATH/bin
# or, from a checkout:
make install                                                # into ~/.local/bin
```

Then:

```
agentbar install     # adds hooks to ~/.claude/settings.json (backup written first)
agentbar doctor      # checks tmux, hooks, and Claude's session registry
```

Add one line to your tmux config and reload it:

```tmux
# optional, before the run-shell line:
set -g @agentbar-key        a       # toggle key after prefix (default a)
set -g @agentbar-focus-key  A       # focus key after prefix (default A)
set -g @agentbar-side       left    # left | right
set -g @agentbar-width      42

run-shell "$HOME/.local/bin/agentbar tmux-init"
```

Or, from a checkout or with TPM (`set -g @plugin 'ssnxd/agentbar'`), use
`run-shell ~/path/to/agentbar/agentbar.tmux`; it finds the binary
(`@agentbar-bin` overrides) and runs the same `tmux-init`.

`tmux-init` binds the two keys and installs tmux hooks (indexed `[97]`, so
your own hooks are untouched): new windows and sessions get a sidebar,
window resizes re-pin the width, a window left holding only a sidebar is
closed, and window or session switches refresh the daemon.

Sessions already running when you install pick up the hooks on their next
event (Claude Code hot-reloads settings); until then the sidebar falls back
to inference from the transcript.

## Keys

| Key        | Action                                                          |
|------------|-----------------------------------------------------------------|
| `prefix a` | open a sidebar in every window, or close them all               |
| `prefix A` | focus the sidebar, or go back to the pane you came from          |
| j / k      | move                                                            |
| enter      | jump to the session's pane (switches window or session as needed) |
| tab        | next session that needs you                                     |
| y          | accept: press Enter in a session sitting on a permission prompt |
| x          | kill the session (asks y/n, sends SIGTERM)                      |
| /          | filter by project, title, branch, or status; esc clears         |
| r          | refresh now                                                     |
| ?          | help                                                            |
| q          | close every sidebar                                             |

## How it works

- **Discovery** reads Claude Code's own registry, `~/.claude/sessions/<pid>.json`,
  which lists every running session with its pid, cwd, session id, and the
  exact tmux pane. Dead pids are skipped.
- **Status** comes from hooks. `agentbar install` adds observe-only hooks
  (SessionStart, UserPromptSubmit, PreToolUse, PermissionRequest,
  Notification, Stop, StopFailure, SessionEnd) that pipe into
  `agentbar event`, which writes one small JSON file per session under
  `~/.local/share/agentbar/state/`. Hooks exit 0 no matter what and never
  print, so they cannot block Claude.
- **Fallback** for sessions without hook data: the registry's own
  "waiting for" flag, then the transcript tail (a closed turn means waiting;
  an open turn with recent writes means working; an open turn gone silent
  probably means a prompt).
- **Enrichment** (title, model, branch, context size) is read from the last
  64KB of the session's transcript, cached by mtime.
- **One pane per window, one daemon.** Each sidebar pane is tagged with the
  tmux pane option `@agentbar` and runs a thin viewer. A single
  `agentbar daemon` builds the snapshot once a second (or at once when a
  hook nudges it) and streams it to every viewer over a Unix socket; it
  exits by itself a minute after the last viewer goes away. Viewers in
  windows that are not on screen skip their spinner tick. Panes are given
  your `window-active-style` so inactive-pane dimming leaves them readable.
  This is the same shape workmux, tmux-pane-tree, and tmux-agent-sidebar
  use, because it is the only one that never resizes your other panes.

## Files

```
~/.local/share/agentbar/state/<session_id>.json   hook state
~/.local/share/agentbar/daemon.sock, daemon.pid   snapshot daemon
~/.local/share/agentbar/agentbar.log              hook receiver and daemon errors
~/.claude/settings.json                           hooks (backups: settings.json.bak.<timestamp>)
```

`AGENTBAR_STATE_DIR` and `AGENTBAR_CLAUDE_DIR` override the locations.

## Troubleshooting

- `agentbar doctor` first.
- A row stuck on `unknown`: the session predates the hooks and has no
  transcript yet; it resolves on its next event.
- `y` does nothing: it only sends Enter when the row is needs-you and the pane
  still runs claude. The footer says why otherwise.
- Hook errors: `tail ~/.local/share/agentbar/agentbar.log`.
- Remove everything: `agentbar install --uninstall`, delete the tmux line,
  `rm -rf ~/.local/share/agentbar`.

## Development

```
make test      # unit tests (no tmux needed)
make ci        # fmt, vet, race tests, vulncheck
```

Design notes live in `docs/superpowers/specs/`.
