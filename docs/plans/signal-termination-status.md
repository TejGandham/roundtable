# Signal Termination Masking

## Problem

When a CLI child process is killed by SIGTERM/SIGKILL, Node.js reports `code === null` and `signal === 'SIGTERM'` in the `close` event. `runCli()` at `roundtable.mjs:366` only captures `code`, ignoring `signal`. `buildResult()` at `roundtable.mjs:425` only downgrades parsed success when `exit_code !== 0 && exit_code !== null` — but `null` (signal death) passes this check, so a signaled CLI with parseable partial output reports as `status: "ok"`.

## Impact

- Interrupted runs silently report success
- Partial output from a killed model gets treated as a complete response
- `cleanupAndExit()` SIGTERM during shutdown can produce a "successful" result for an aborted review

## Fix

1. Capture both `code` and `signal` from the `close` event in `runCli()`:

```js
proc.on('close', (code, signal) => {
  activeChildren.delete(proc);
  clearTimeout(timeoutTimer);
  resolvePromise({
    stdout,
    stderr,
    exit_code: code,
    exit_signal: signal || null,
    elapsed_ms: Date.now() - startTime,
    timed_out: killed,
    truncated,
  });
});
```

2. In `buildResult()`, classify signal deaths as errors:

```js
// After existing timeout check
if (raw.exit_signal && !raw.timed_out) status = 'terminated';
// Existing non-zero exit check
if (raw.exit_code !== 0 && raw.exit_code !== null && status === 'ok') status = 'error';
```

3. Propagate `exit_signal` in the output envelope for diagnostic visibility.

## Verification

1. Start a roundtable call, then send SIGTERM to one child PID — result should show `status: "terminated"`, not `"ok"`
2. Verify that the existing timeout path still produces `status: "timeout"` (not `"terminated"`)
3. Verify normal completion still produces `status: "ok"`

## Lines

- `roundtable.mjs:366` — `proc.on('close')` in `runCli()`
- `roundtable.mjs:425-428` — status classification in `buildResult()`
- `roundtable.mjs:551-563` — `cleanupAndExit()` sends SIGTERM to children
