# probeFailResult Discards Probe Exit Code

## Problem

`probeFailResult()` at `roundtable.mjs:497` hardcodes `exit_code: null` regardless of the actual probe result:

```js
const probeFailResult = (name, model, reason) => ({
  response: '', model, status: 'probe_failed',
  exit_code: null,  // always null — actual probe exit code discarded
  stderr: `${name} CLI probe failed: ${reason}...`,
  // ...
});
```

The probe results in `probeResults.gemini` / `probeResults.codex` contain the actual `exit_code` from `probeCli()`, but they're not forwarded to the output.

## Impact

- Can't distinguish probe crash (exit 1) from probe timeout (no exit) from probe auth failure (exit 2+)
- Diagnostic value lost — probe exit codes can indicate specific failure modes (auth, missing deps, config error)
- Inconsistent with the main `buildResult()` path which preserves exit codes

## Fix

Pass the probe exit code through to `probeFailResult()`:

```js
const probeFailResult = (name, model, probeResult) => ({
  response: '', model, status: 'probe_failed',
  exit_code: probeResult?.exit_code ?? null,
  stderr: `${name} CLI probe failed: ${probeResult?.reason || 'unknown'}. Run ${name.toLowerCase()} --version to diagnose.`,
  elapsed_ms: 0, parse_error: null, truncated: false, session_id: null,
});
```

Update the call sites to pass the full probe result:

```js
gemini: (geminiPath && !geminiHealthy)
  ? probeFailResult('Gemini', args.geminiModel, probeResults.gemini)
  : buildResult('gemini', geminiPath, args.geminiModel, args, geminiSettled),
```

## Verification

1. Break Gemini auth (rename credentials file temporarily)
2. Run roundtable — probe should fail with actual exit code visible in output
3. Verify `exit_code` is a number, not null

## Lines

- `roundtable.mjs:497-502` — `probeFailResult()` definition
- `roundtable.mjs:504-509` — call sites passing `probeResults.gemini?.reason`
