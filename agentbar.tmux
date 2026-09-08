#!/usr/bin/env bash
# agentbar tmux plugin entry. TPM-compatible; also fine via a plain
# `run-shell /path/to/agentbar.tmux` line in tmux.conf. Everything it does is
# `agentbar tmux-init`; if you would rather not depend on this repo's path,
# put `run-shell "$HOME/.local/bin/agentbar tmux-init"` in tmux.conf instead.
#
# Options (set -g in tmux.conf before this line):
#   @agentbar-key     key after prefix               (default: a)
#   @agentbar-bin     path to the agentbar binary    (default: agentbar on PATH, then ~/.local/bin/agentbar)
#   @agentbar-follow  on | off: move the sidebar on every window switch (default: on)
#   @agentbar-side    left | right                   (default: left)
#   @agentbar-width   sidebar width in columns       (default: 42)
set -eu

bin="$(tmux show-option -gqv @agentbar-bin)"
if [ -z "$bin" ]; then
  bin="$(command -v agentbar 2>/dev/null || true)"
fi
if [ -z "$bin" ] && [ -x "$HOME/.local/bin/agentbar" ]; then
  bin="$HOME/.local/bin/agentbar"
fi
if [ -z "$bin" ]; then
  tmux display-message "agentbar: binary not found (set @agentbar-bin)"
  exit 0
fi

exec "$bin" tmux-init
