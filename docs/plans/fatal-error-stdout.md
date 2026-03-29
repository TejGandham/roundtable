# Fatal Error Stdout Contract

## Problem

`main().catch` at `roundtable.mjs:567` writes structured error JSON to **stderr** via `console.error`, not stdout. The normal success path writes JSON to stdout via `console.log`. Any consumer parsing stdout gets an empty string on argument errors, role-loading failures, or unexpected exceptions.

## Impact

- Claude Code (the primary consumer) parses stdout for the JSON envelope — fatal errors produce no parseable output
- Callers must check both stdout and stderr to get complete error information
- Breaks the "JSON on stdout, diagnostics on stderr" contract that every other code path follows

## Fix

Replace `console.error` with `console.log` in the catch handler so all exits emit JSON on stdout:

```js
main().catch(err => {
  const msg = err instanceof ArgError
    ? { error: err.message, usage: 'roundtable --prompt "..." [--role ...] [--files ...]' }
    : { error: err.message };
  console.log(JSON.stringify(msg));  // stdout, not stderr
  process.exit(1);
});
```

Optionally also write to stderr for human-readable diagnostics, but stdout must always have the machine-readable envelope.

## Verification

1. Run with missing `--prompt` — stdout should contain `{ "error": "Missing required --prompt argument", "usage": "..." }`
2. Run with nonexistent role — stdout should contain `{ "error": "Role prompt not found: ..." }`
3. Pipe stdout through `jq .error` — should parse cleanly for all failure modes

## Lines

- `roundtable.mjs:567-573` — main().catch handler
