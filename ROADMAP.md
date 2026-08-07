# Road to v1

v1 promise: **the trustworthy terminal home for a fleet of Claude agents —
spawn, watch, get pulled in only when needed, review, land.**

Strategic context (Aug 2026): the field is consolidating. Vibe Kanban is
sunsetting, Terragon and Opcode are dead; workmux, gwq, ccmanager remain.
Claude Code's native agent teams now covers part of our core (spawn +
mailboxes + worktrees), but lacks recovery, cross-repo fleet visibility, and
any review/merge UX. v1 differentiates exactly there — and the graveyard
shows thin, local, actively-maintained tools survive.

## P0 — table stakes (v1 blockers)

1. **Land flow** — the gap between "agent done" and "code merged" is the #1
   competitor delta (workmux) and the #2 user pain (review bottleneck).
   - `workflow merge` / TUI action: squash | rebase | merge, pre-merge hook
     (tests/lint), conflict surfacing as a task state, cleanup on land
     (worktree, branch, window, DB row).
   - PR creation (`gh pr create`) with generated description; PR status
     column (statusline `pr.*` fields are free).
2. **Diff review in the TUI** — full per-branch diff view; hunk comments
   that flow back to the agent through the existing inbox + nudge rails
   (few TUIs have this loop; we already own the transport).
3. **Notifications** — #1 user pain ("you lose track by lunchtime").
   Desktop notification + terminal bell on needs-you / done / rate-limited,
   from the existing hook pipeline; a "jump to next needs-attention" key;
   status icons pushed into tmux window names.
4. **Cost + limits** — per-task budget cap enforced by a statusline-fed
   watchdog (warn → interrupt), max-concurrent worker throttle, distinct
   "rate-limited" state via StopFailure(rate_limit) hooks.
5. **Reliability hardening**
   - Per-session settings *file* instead of inline `--settings` JSON —
     Claude hot-reloads file hooks, enabling live hook changes on running
     agents.
   - Inbox sweep on the poll tick (nudges must not depend on one Stop
     event); bulk resurrect after reboot; optional auto-recover policy.
   - E2E test with a fake `claude` binary that emits real hook calls, so
     the status pipeline is CI-covered.
   - Per-repo config overlay (worktree copy globs, post-create hook,
     allowlists) in `.workflow.json` at the repo root.

## P1 — differentiators

6. **Safe-yolo sandbox preset** — `bypassPermissions` composed with Claude's
   OS sandbox (`sandbox.enabled`, `strictAllowlist` network, credential
   file denies, Edit denied outside the worktree). "Yolo but safe" as a
   one-line config. Nobody in the TUI field ships this today.
7. **Conflict-aware orchestration** — semantic merge conflicts eat 30–50% of
   parallel-agent time and no tool addresses it: orchestrator skill rules
   for partitioning work by file ownership, a sequential merge queue, and
   rebase automation between merges.
8. **External session adoption** — `claude agents --json` as the discovery
   API; promote an external session into a managed task; global fzf jump
   across everything managed and external.
9. **Task artifacts** — `/export` transcript on SessionEnd, a task report
   view (what shipped, cost, per-agent summaries); `claude ultrareview` as
   a one-key review of a worker branch.

## P2 — post-v1

- Headless worker class (`--print --input-format stream-json`) for
  fire-and-forget tasks: exact per-run cost, `--max-budget-usd`,
  `--json-schema` results. Interactive stays the default — attachability is
  the product.
- Read-only visualization of native agent teams (`~/.claude/teams`,
  `~/.claude/tasks`) when the user opts into the experiment.
- Webhook/push notification channel (phone approvals, omnara-style).
- Multi-agent-CLI support (Codex, Gemini, OpenCode) behind an adapter.
- Kanban / web views: explicitly out — terminal-native is the identity.

## Non-goals for v1

Cloud execution, team/multi-user features, Windows, non-git projects.
