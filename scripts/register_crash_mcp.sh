#!/usr/bin/env bash
# PHASE B2 TEMPORARY — remove in Phase C.
# Registers the roundtable binary as `roundtable-crash` so we can test
# Claude Code's stdio crash recovery behavior. See
# docs/plans/2026-04-11-phase-b-verification-results.md.
set -euo pipefail

BINARY="${ROUNDTABLE_BIN:-$HOME/.local/bin/roundtable}"

if [ ! -x "$BINARY" ]; then
    echo "error: $BINARY not found or not executable" >&2
    echo "hint: run 'make build && cp roundtable-http-mcp $BINARY'" >&2
    exit 1
fi

echo "removing any existing roundtable-crash registration..."
claude mcp remove roundtable-crash 2>/dev/null || true

echo "registering roundtable-crash with __crash subcommand..."
claude mcp add -s user roundtable-crash -- "$BINARY" __crash

echo "done. Verify with:"
echo "    claude mcp list | grep roundtable-crash"
echo
echo "then restart Claude Code and call a tool on roundtable-crash to trigger"
echo "the crash. Observe Claude Code's behavior and record results in"
echo "docs/plans/2026-04-11-phase-b-verification-results.md."
