# agentbar

A tmux sidebar for the Claude Code sessions you run by hand.

You keep starting `claude` in tmux panes yourself. agentbar only adds
visibility: press `prefix a` and a narrow pane lists every running session,
grouped by project, with what it is doing right now and where it lives in
tmux. Press `enter` to jump to one; the sidebar follows you.

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

Add the plugin to your tmux config and reload it:

```tmux
# optional, before the run-shell line:
set -g @agentbar-key   a        # key after prefix (default a)
set -g @agentbar-side  left     # left | right
set -g @agentbar-width 42
set -g @agentbar-bin   ~/.local/bin/agentbar   # if not on PATH

run-shell ~/path/to/agentbar/agentbar.tmux
```

With TPM: `set -g @plugin 'ssnxd/agentbar'`.

Sessions already running when you install pick up the hooks on their next
event (Claude Code hot-reloads settings); until then the sidebar falls back
to inference from the transcript.

## Keys

| Key        | Action                                                          |
|------------|-----------------------------------------------------------------|
| `prefix a` | open the sidebar; focus it; bring it to this window; close it   |
| j / k      | move                                                            |
| enter      | jump to the session's pane (the sidebar moves into that window) |
| tab        | next session that needs you                                     |
| y          | accept: press Enter in a session sitting on a permission prompt |
| x          | kill the session (asks y/n, sends SIGTERM)                      |
| /          | filter by project, title, branch, or status; esc clears         |
| r          | refresh now                                                     |
| ?          | help                                                            |
| q          | close the sidebar                                               |

One press of `prefix a` does the right thing for where the sidebar is: none
anywhere opens one here; here and focused closes it; here and unfocused
focuses it; in another window moves it here.

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
- **The sidebar pane** is tagged with the tmux pane option `@agentbar`, which
  is how `toggle` and `jump` find it. It is given your `window-active-style`
  so inactive-pane dimming leaves it readable.

## Files

```
~/.local/share/agentbar/state/<session_id>.json   hook state
~/.local/share/agentbar/agentbar.log              hook receiver errors
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
