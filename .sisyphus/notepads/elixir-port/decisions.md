# Decisions

- Kept the implementation strict to the requested two-tier search path and `.txt` suffix only.
- Preserved permission/error re-raising instead of swallowing non-ENOENT failures.
- Implemented CLI execution through `/bin/sh` + `setsid --wait` wrapper, redirected stderr to a temp file, and enforced SIGTERM→SIGKILL escalation with port mailbox draining on timeout.
- Retained a 1MB stdout cap (`truncated` flag) and direct script-path invocation (`run_cli(script, [], timeout)`) to match expected runner contract.
- Replaced probe execution with `Port.open({:spawn_executable, ...})` so timeout cleanup can kill the actual OS process.
- Removed `pkill -f` fallback from process-group cleanup to avoid host-wide collateral kills from executable-name matching.
