#!/usr/bin/env bash
# Measures codex app-server cold-start latency across N iterations.
#
# Methodology: pipe one initialize request to a fresh `codex app-server`
# process, let it respond, then close stdin so the server exits. Measure
# wall-clock time between start and exit.
#
# The measurement includes: process exec + codex init + initialize
# handshake round-trip + process cleanup. It does NOT include thread/start
# or turn/start, so it is a lower bound on what a user's "first codex
# request" will experience — the real first-call latency is at minimum
# this plus model-inference time.
set -euo pipefail
ITERATIONS=${ITERATIONS:-20}
TIMES=()
for i in $(seq 1 $ITERATIONS); do
  start=$(date +%s%3N)
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"bench","version":"0"}}}' \
    | timeout 30 codex app-server --listen stdio:// >/dev/null 2>&1 \
    || true
  end=$(date +%s%3N)
  elapsed=$((end - start))
  TIMES+=("$elapsed")
  printf 'run %d: %d ms\n' "$i" "$elapsed" >&2
done
printf '%s\n' "${TIMES[@]}" | sort -n
