# Learnings

- `Roundtable.Prompt.Roles.load_role_prompt/3` should mirror Node fallback behavior exactly: local project file first, then global file, with ENOENT-only fallback.
- Focused role tests passed against the root `roles/*.txt` fixtures without needing any mix config changes.
- `Port.open({:spawn_executable, "/bin/sh"}, ...)` wrapper assembly must avoid assignment chaining in expression blocks; otherwise only the first assignment result is used and shell wrapper logic is silently bypassed.
- `setsid --wait` plus `/bin/sh -c 'exec <cmd> 2>stderr_file'` preserves exit codes while keeping stderr isolated to a tempfile for post-read assertions.

- Plan compliance audit found `mix.exs` still enables `extra_applications: [:logger]`, which violates the plan's no-Logger guardrail even though no `Logger` calls exist in `lib/`.
- `Roundtable.CLI.Runner.run_cli/3` currently hardcodes `exit_signal: nil` in both completion paths, so `terminated` exists in `Roundtable.Output` but is unreachable from real execution.
- Concrete deliverables/evidence still missing after implementation: `test/support/fake_cli.sh`, `.sisyphus/evidence/task-2-missing-callback.txt`, `.sisyphus/evidence/task-3-not-found.txt`, `.sisyphus/evidence/task-4-unavailable.txt`, and `.sisyphus/evidence/task-13-readme.txt`.
- `SKILL.md` no longer invokes `node roundtable.mjs`, but some examples still point at non-project paths instead of `./roundtable`.
- `probe_cli/3` needed a Port-based spawn path so timeout cleanup can terminate the real OS process instead of only killing the Elixir task wrapper.
- Removing `pkill -f` fallback from `kill_process_group/2` is safe when the process tree is already tracked via `pgrep -P` and direct PID kills.
