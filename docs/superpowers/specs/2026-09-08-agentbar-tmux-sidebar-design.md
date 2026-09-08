# agentbar: a tmux sidebar for running Claude Code sessions

Date: 2026-09-08
Status: approved in chat, implementation follows this spec

## Goal

The user runs every Claude Code session by hand in their own tmux. They do
not want an orchestration layer. They want visibility: how many sessions
exist, which tmux pane each lives in, whether each one is working, waiting
for input, or sitting on a permission prompt, and a one-key jump to it.

`agentbar` is that sidebar. It is a tmux plugin backed by one Go binary.
The project replaces the previous `workflow` orchestrator in this repo.
Claude Code is the only supported agent for now; the discovery and state
layers are kept behind small interfaces so Codex or others can be added
later without touching the UI.

## Non-goals

- Starting, stopping, or scheduling agents. The user does that.
- Worktrees, branches, merging, task DB, inter-agent messaging.
- Any second tmux server. Only the user's default server is scanned.
- Desktop notifications. The user already has a Stop hook for that.

## User-facing behaviour

### Hotkey

`prefix a` (default; `@agentbar-key` overrides) runs `agentbar toggle`:

| State when pressed                          | Action                                   |
|---------------------------------------------|------------------------------------------|
| No sidebar anywhere                         | Open a sidebar pane in the current window, focus it |
| Sidebar in this window, focused             | Close it                                 |
| Sidebar in this window, not focused         | Focus it                                 |
| Sidebar in another window                   | Move it into this window (`join-pane`), focus it |

The sidebar is a 42-column pane (`@agentbar-width`) on the left
(`@agentbar-side` = left | right). It is tagged with the pane option
`@agentbar=1`; that tag is how every command finds it. tmux 3.2 or newer
is required for pane user options and `-e` on split-window.

The sidebar pane gets its own pane style equal to the active window style,
so the user's inactive-pane dimming does not grey it out.

### Sidebar layout

Grouped, two-line rows, as approved:

```
 claude ─────────── 4 · 1 needs you
 
 miivo-api
  ● needs you  fix auth middleware
    work:2.1 · fable · fix/auth 12m
  ◐ working    add rate limiter
    work:3.1 · opus · main      3m
 
 workflow
  ○ waiting    tmux sidebar plugin
    dev:1.2 · fable · main      1m
 
 enter jump  y accept  / filter  ?
```

- Header: app name, total sessions, count of needs-you sessions.
- Groups: one per project directory (basename of cwd, with the parent dir
  added when two projects share a basename). Groups are sorted by their
  most urgent session, then by name. Within a group: needs-you, error,
  waiting, working, then by last activity.
- Row line 1: status icon and label, then the title. Title comes from the
  transcript `aiTitle`, then the first user prompt, then Claude's registry
  display name.
- Row line 2, dim: tmux target as `session:window.pane` using the user's
  base-index numbering, model short name, git branch, age of last activity.
  A row for a session outside tmux shows `not in tmux` and cannot be jumped
  to.
- A needs-you row adds a third line with the permission detail, for example
  `permission: Bash git push origin main`.
- Selected row: bold with a background band. Working icon animates.
- Footer: key hints for the current context (`y` only shows on a needs-you
  row, `x` always, `/` always, `?` expands the full list).

Statuses and icons:

| Status    | Icon | Meaning                                                   |
|-----------|------|-----------------------------------------------------------|
| needs-you | ●    | permission prompt, elicitation, or agent asked for input  |
| working   | ◐◑   | a turn is in progress                                     |
| waiting   | ○    | turn ended, prompt is idle, it wants the next instruction |
| error     | ✗    | StopFailure (rate limit, API error)                       |
| unknown   | ◌    | discovered but no hook state and heuristics inconclusive  |

### Keys

| Key        | Action                                                          |
|------------|-----------------------------------------------------------------|
| j / k, ↓/↑ | move selection                                                  |
| enter      | jump: select the target window and pane; the sidebar follows    |
| tab        | select the next needs-you row                                   |
| y          | accept: send Enter to the session pane. Only on a needs-you row whose pane still runs claude. Otherwise a footer message explains why not. |
| x          | kill: confirm with `y`, then SIGTERM the pid                    |
| /          | filter rows by project, title, branch, or status. esc clears    |
| r          | refresh now                                                     |
| ?          | toggle full key help                                            |
| q          | close the sidebar                                               |

### Following on jump

Jump is `agentbar jump <pane-id>` executed by the UI: `join-pane -hb -l W
-s <sidebar> -t <target pane>`, then `select-pane -t <target>`. tmux
restores the source window's layout when a pane leaves it, so nothing else
is needed.

## Architecture

```
~/.claude/settings.json hooks ──▶ agentbar event ──▶ ~/.local/share/agentbar/state/<session_id>.json
~/.claude/sessions/<pid>.json  ──▶ discovery ──┐
~/.claude/projects/**/*.jsonl  ──▶ enrich    ──┼──▶ merge ──▶ agentbar ui (bubbletea, 1s poll)
state/<session_id>.json        ──▶ hook state ─┘                 │
                                                                 ▼
                                            tmux: select-window, select-pane, join-pane, send-keys
```

### Packages

```
cmd/agentbar/main.go        subcommand dispatch
internal/state              hook state files: types, write, read, sweep
internal/hooks              hook payload parsing, event→status mapping, settings.json merge
internal/registry           Claude's ~/.claude/sessions/*.json reader, pid liveness
internal/transcript         transcript tail parser (moved from internal/external)
internal/session            Session model, Discover() = registry + state + transcript merge
internal/tmuxctl            thin tmux runner for the default server: pane lookup by option,
                            split, join, select, send-keys, resolve target names
internal/ui                 bubbletea sidebar
internal/cli                event, toggle, jump, install, doctor commands
agentbar.tmux               TPM-style plugin entry
```

Everything under `internal/` from the old orchestrator is deleted:
`db`, `manager`, `msg`, `gitx`, `repos`, `notify`, `config`, `tui`, the old
`cli`, `hooks` (rewritten), `external` (split into `registry`,
`transcript`, `session`), `assets/`, migrations, `ROADMAP.md`,
`CHANGELOG.md` (restarted), `docs/dashboard.png`. Old code stays in git
history. Module path becomes `github.com/ssnxd/agentbar`. Renaming the
GitHub repository is the user's action and is not part of this work.

### Hook receiver: `agentbar event`

Installed into the user's global `~/.claude/settings.json` by
`agentbar install`. All hooks are observe-only: read stdin JSON, write a
file, exit 0, never print JSON to stdout. Timeout 5s. The binary path is
absolute.

| Hook                                                    | Status written | Extra              |
|---------------------------------------------------------|----------------|--------------------|
| SessionStart                                            | working        | session id is the file key |
| UserPromptSubmit                                        | working        |                    |
| PreToolUse (async)                                      | working        | tool name          |
| PermissionRequest                                       | needs-you      | tool name + one-line summary of tool_input |
| Notification matcher `permission_prompt\|elicitation_dialog\|agent_needs_input` | needs-you | message |
| Notification matcher `idle_prompt`                      | waiting        |                    |
| Stop                                                    | waiting        |                    |
| StopFailure                                             | error          | message            |
| SessionEnd                                              | (delete file)  |                    |

State file, written atomically (temp file + rename):

```json
{
  "session_id": "…",
  "cwd": "/Users/miivo/code/miivo",
  "status": "needs-you",
  "detail": "Bash git push origin main",
  "tool": "Bash",
  "event": "PermissionRequest",
  "updated_at": "2026-09-08T13:40:00+02:00"
}
```

The tool-input summary: for Bash the `command`, for Edit/Write/Read the
`file_path` basename, otherwise the first string value found. Truncated to
80 runes.

The settings merge keeps every existing hook entry (the user's osascript
Stop hook stays), adds agentbar entries only if an identical command is not
already present, writes a timestamped backup next to the file first, and is
idempotent. `agentbar install --uninstall` removes exactly the entries it
added.

### Discovery: `internal/registry`

Reads every `~/.claude/sessions/*.json`. Fields used: `pid`, `sessionId`,
`cwd`, `name`, `startedAt`, `kind`, `tmux` (format `session:@window.%pane`),
`status`, `waitingFor`, `statusUpdatedAt`. Entries whose pid is not alive
(`kill -0`) are ignored; their files are Claude's to clean up. Only
`kind == "interactive"` entries are shown by default.

This registry is undocumented and observed on Claude Code 2.1.263. The
reader tolerates missing fields and is covered by a fixture test with the
real file shape, so a format change fails loudly in tests rather than
silently in the UI.

### Enrichment: `internal/transcript`

The existing tail parser, unchanged in behaviour: last 64 KB of the newest
`.jsonl` under `~/.claude/projects/<munged cwd>/`, preferring the file
named `<sessionId>.jsonl` when it exists. Yields title, first prompt,
model, branch, context tokens, permission mode, last activity time, and the
turn-open/turn-closed heuristic. Results are cached per file by size and
mtime so the 1s poll does not re-read unchanged transcripts.

### Merge: `internal/session`

For each live registry entry, status is decided in this order:

1. Hook state file with the same session id, if its `updated_at` is newer
   than the session's `startedAt`. Exact.
2. Registry `status == "waiting"` with a `waitingFor` value: needs-you,
   detail is `waitingFor`.
3. Transcript heuristic: turn closed → waiting; turn open and written in
   the last 30s → working; turn open and silent → needs-you with detail
   `probably a prompt (no hook data)`.
4. Otherwise unknown.

The registry's `busy` and `idle` values are not trusted for status: a
session was observed reporting `busy` minutes after its turn ended.

The tmux location comes from the registry `tmux` field, verified against
`list-panes -a` so the window and pane indexes shown match what the user's
`base-index` produces. If the registry has no tmux field, the pane is
looked up by pane_tty against the process tty as a fallback.

### State sweep

On every UI poll, state files whose session id is not in the live registry
and whose `updated_at` is older than one hour are deleted.

### tmux control: `internal/tmuxctl`

A small runner with an interface so tests can substitute a fake:

```go
type Runner interface{ Run(args ...string) (string, error) }
```

Functions: `FindSidebar() (paneID, windowID string, ok bool)`, `Current()
(paneID, windowID string)`, `OpenSidebar(window, side, width, cmd)`,
`JoinSidebar(sidebar, targetPane, side, width)`, `SelectPane`,
`SelectWindow`, `KillPane`, `SendEnter(pane)`, `PaneCommand(pane)`,
`ResolveTarget(sessionName, windowID, paneID) (display string)`.
`TMUX`/`TMUX_PANE` are stripped from the environment for every call so the
default server is always addressed.

### UI: `internal/ui`

Bubble Tea v2, Lipgloss v2. Palette is Catppuccin Mocha to match the
user's tmux theme. One model: sessions, selection (by session id, so the
cursor survives reorders), filter text, help toggle, kill-confirm state,
footer message with expiry, tick counter for the spinner. Poll every 1s
via a tick command; rendering is pure over the model. Width comes from the
pane; if narrower than 30 columns, line 2 is dropped.

### Plugin script: `agentbar.tmux`

```sh
#!/usr/bin/env bash
# TPM-compatible. Reads @agentbar-key (default a), @agentbar-bin (default: agentbar on PATH).
```

It binds `prefix <key>` to `run-shell "<bin> toggle"`. Installation for
this user: one `run-shell ~/code/agentbar/agentbar.tmux` line appended to
`~/config/tmux/tmux.conf`, then `tmux source-file`. `agentbar install`
prints that line; it does not edit the tmux config itself, because the
user keeps that file in dotfiles. I add the line as part of this work,
once, with the user's approval already given ("setup it up in our
config").

### Install and doctor

- `agentbar install` merges hooks into `~/.claude/settings.json` and prints
  the tmux line.
- `agentbar doctor` checks: tmux version ≥ 3.2, binary is absolute and
  executable, hooks present in settings, registry dir readable, at least
  one live session or a clear "none running" line.
- `make install` builds into `~/.local/bin/agentbar` (on PATH already).

## Error handling

- Hook receiver: any failure is logged to
  `~/.local/share/agentbar/agentbar.log` and exits 0. It must never make
  Claude wait or show an error.
- Discovery: a bad registry file or unreadable transcript degrades that one
  row to unknown status; the sidebar keeps rendering.
- tmux failures on jump/accept/kill: footer message with the stderr line,
  no crash.
- `toggle` outside tmux prints one line and exits 1.

## Testing

Unit, with fixtures captured from the real machine:

- `hooks`: every hook event → status and detail; tool-input summary; the
  settings merge is idempotent, preserves foreign hooks, and uninstall
  removes only its own entries.
- `registry`: parses the real 2.1.263 file shape; skips dead pids; parses
  the `tmux` field.
- `transcript`: existing tests carried over; add first-prompt and
  `<sessionId>.jsonl` preference.
- `session`: merge priority table (hook beats registry beats heuristic;
  stale hook state is ignored).
- `tmuxctl`: toggle decision table and jump against a fake runner.
- `ui`: render snapshot of the grouped layout at 42 columns, the narrow
  layout, and the needs-you third line.

End to end, in the user's live tmux, shown as output before completion:

1. `agentbar doctor` passes.
2. `prefix a` opens the sidebar listing the currently running sessions
   with correct tmux targets.
3. Enter on a row lands in that pane; the sidebar follows.
4. A scratch `claude` session in a temp dir runs a command that prompts
   for permission; the sidebar shows needs-you with the command; `y`
   accepts it; the row returns to working then waiting.
5. `prefix a` twice: focus, then close.

## Execution order

1. Strip the repo, rename the module, keep and move the transcript parser.
2. `state` + `hooks` + `agentbar event`, with tests.
3. `registry` + `session` merge, with fixtures.
4. `tmuxctl` + `toggle` / `jump`, with the fake-runner tests.
5. `ui`.
6. `install`, `doctor`, `agentbar.tmux`, Makefile, README.
7. Install hooks, add the tmux line, live verification.

Steps 2, 3, and 4 are independent of each other once step 1 is done.
