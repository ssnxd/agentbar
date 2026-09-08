#!/usr/bin/env bash
# agentbar tmux plugin entry. TPM-compatible; also fine via a plain
# `run-shell /path/to/agentbar.tmux` line in tmux.conf.
#
# Options (set -g in tmux.conf before this line):
#   @agentbar-key    key after prefix               (default: a)
#   @agentbar-bin    path to the agentbar binary    (default: agentbar on PATH, then ~/.local/bin/agentbar)
#   @agentbar-side   left | right                   (default: left)
#   @agentbar-width  sidebar width in columns       (default: 42)
set -eu

get() { tmux show-option -gqv "$1"; }

key="$(get @agentbar-key)"
key="${key:-a}"

bin="$(get @agentbar-bin)"
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

# #{pane_id} is expanded by run-shell when the key is pressed, so toggle
# always knows which pane it was invoked from.
tmux bind-key "$key" run-shell "$bin toggle #{pane_id}"
