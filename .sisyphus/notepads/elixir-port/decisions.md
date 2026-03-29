# Decisions

- Kept the implementation strict to the requested two-tier search path and `.txt` suffix only.
- Preserved permission/error re-raising instead of swallowing non-ENOENT failures.
- Implemented CLI execution through `/bin/sh` + `setsid --wait` wrapper, redirected stderr to a temp file, and enforced SIGTERM→SIGKILL escalation with port mailbox draining on timeout.
- Retained a 1MB stdout cap (`truncated` flag) and direct script-path invocation (`run_cli(script, [], timeout)`) to match expected runner contract.
