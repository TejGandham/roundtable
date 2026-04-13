#!/usr/bin/env bash
# Phase A dogfood observability shim.
#
# Wraps the real roundtable binary and tees its stderr to a per-session log
# file under ~/.local/share/roundtable/logs/. stdin and stdout are untouched
# so MCP JSON-RPC framing is preserved. Any args passed by Claude Code (e.g.
# "stdio", or future flags) are forwarded unchanged via "$@".
#
# This is a temporary observability aid for the 1-week dogfood window before
# Phase C. Phase D (install.sh + release packaging) will ship the single
# binary directly without this wrapper.
#
# Usage:
#   1. Build and install the real binary as roundtable-bin:
#        make build
#        cp roundtable-http-mcp $HOME/.local/bin/roundtable-bin
#   2. Install this script as roundtable:
#        install -m 755 scripts/roundtable-stdio-wrapper.sh \
#          $HOME/.local/bin/roundtable
#   3. Existing `claude mcp add ... -- $HOME/.local/bin/roundtable stdio`
#      registration keeps working. Each new Claude Code session creates a
#      fresh log file at:
#        $HOME/.local/share/roundtable/logs/stdio-<timestamp>-<pid>.log
set -euo pipefail
umask 077

LOG_DIR="${ROUNDTABLE_LOG_DIR:-$HOME/.local/share/roundtable/logs}"
mkdir -p "$LOG_DIR"
LOG_FILE="$LOG_DIR/stdio-$(date +%Y%m%d-%H%M%S)-$$.log"

exec "$HOME/.local/bin/roundtable-bin" "$@" 2>"$LOG_FILE"
