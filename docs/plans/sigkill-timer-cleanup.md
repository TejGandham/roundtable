# SIGKILL Fallback Timer Not Cancelled

## Problem

Both `probeCli()` (line 109) and `runCli()` (line 360) schedule a SIGKILL escalation timer after sending SIGTERM:

```js
const timer = setTimeout(() => {
  proc.kill('SIGTERM');
  setTimeout(() => {
    try { proc.kill('SIGKILL'); } catch { /* already dead */ }
  }, 2000);  // <-- this inner timer is never cleared
}, probeTimeoutMs);
```

If the process exits cleanly after SIGTERM but before the 2s/3s SIGKILL timer fires, the timer still executes. The `try/catch` around `kill()` swallows the "already dead" error, but with PID reuse an unrelated process could receive the signal.

## Impact

- Low in practice: PID reuse within 2-3 seconds is unlikely on most systems
- Higher risk on busy CI/container environments with rapid process churn
- The pattern is sloppy — resources not properly cleaned up

## Fix

Store the escalation timer handle and clear it in the `close` handler:

**In `runCli()`:**
```js
let killTimer = null;

const timeoutTimer = setTimeout(() => {
  killed = true;
  killTree('SIGTERM');
  killTimer = setTimeout(() => killTree('SIGKILL'), 3000);
}, timeoutMs);

proc.on('close', (code, signal) => {
  activeChildren.delete(proc);
  clearTimeout(timeoutTimer);
  if (killTimer) clearTimeout(killTimer);
  // ... resolve
});
```

**In `probeCli()`:**
Same pattern — store the inner timer, clear on `close` and `error`.

## Verification

1. Run with a very short timeout (e.g., `--timeout 1`) so the timeout fires
2. Verify child exits on SIGTERM (check logs)
3. Verify no SIGKILL is sent after the child is already dead (add a temporary log in the SIGKILL callback)

## Lines

- `roundtable.mjs:109-115` — probe timeout escalation
- `roundtable.mjs:356-363` — runCli timeout escalation
- `roundtable.mjs:366` — close handler (needs killTimer clearance)
