# Learnings

- `Roundtable.Prompt.Roles.load_role_prompt/3` should mirror Node fallback behavior exactly: local project file first, then global file, with ENOENT-only fallback.
- Focused role tests passed against the root `roles/*.txt` fixtures without needing any mix config changes.
- `Port.open({:spawn_executable, "/bin/sh"}, ...)` wrapper assembly must avoid assignment chaining in expression blocks; otherwise only the first assignment result is used and shell wrapper logic is silently bypassed.
- `setsid --wait` plus `/bin/sh -c 'exec <cmd> 2>stderr_file'` preserves exit codes while keeping stderr isolated to a tempfile for post-read assertions.
