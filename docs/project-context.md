---
project_name: 'Roundtable'
date: '2026-04-03'
sections_completed:
  ['technology_stack', 'otp_runtime', 'cli_backend', 'testing', 'cross_platform_build', 'critical_rules']
status: 'complete'
rule_count: 80
optimized_for_llm: true
---

# Project Context for AI Agents

_Critical rules and patterns for implementing code in Roundtable — an Elixir/OTP MCP server that dispatches prompts to Claude, Gemini, and Codex CLIs in parallel. Focus on non-obvious details that agents might otherwise miss._

---

## Technology Stack & Versions

- **Elixir** ~> 1.19 (mix.exs constraint) — code to 1.19+ features; target this, not dev env version
- **Erlang/OTP** 28 (ERTS 16.x)
- **Hermes MCP** sourced from `TejGandham/hermes-mcp` fork (GitHub, `main` branch) — includes STDIO transport fixes for task crash recovery, batch message dispatch, and JSON-RPC error responses on server_call_failed. Upstream PR: cloudwalk/hermes-mcp#249.
- **Jason** ~> 1.4 — JSON encoding/decoding (only other direct dep)
- **Release target:** `roundtable_mcp` (no embedded ERTS, `include_erts: false`)
- **External CLIs:** `gemini`, `codex`, `claude` — resolved at runtime via `ROUNDTABLE_<NAME>_PATH` > `ROUNDTABLE_EXTRA_PATH` > system PATH
- **Telemetry:** Optional OTEL via direct HTTP POST using OTP built-ins (`:inets`/`:httpc`, `:crypto`). These are started on-demand, not in `extra_applications`. HTTPS endpoints also need `:ssl`.
- **Transitive deps** (finch, mint, peri, telemetry, etc.) are Hermes internals — do NOT import or depend on them as project APIs
- **Dep source:** `hermes_mcp` is pulled from the GitHub fork, not Hex. No local patches or post-fetch hooks needed.

## Critical Implementation Rules

### OTP & Runtime Rules

- **Conditional boot:** MCP server only starts when `ROUNDTABLE_MCP=1` is set — no long-lived app children otherwise. Dynamic `Task.Supervisor` per dispatch still works in all modes.
- **Timeout budget:** MCP `request_timeout` defaults to 16 min (configurable via `ROUNDTABLE_REQUEST_TIMEOUT_MS` env var); tool timeout caps at 900s; `Task.await` adds 10s margin; probe `Task.yield` adds 1s margin. These form a deliberate budget chain — do not change any timeout independently.
- **Transport error response:** The STDIO transport sends a JSON-RPC error response on every failure path (GenServer.call timeout, server crash, nil server PID). Previously it only logged — leaving MCP clients hanging indefinitely. The error uses JSON-RPC code `-32603` (Internal Error) with a human-readable message.
- **Supervisor restart policy:** Outer supervisor uses `max_restarts: 3, max_seconds: 30` to allow recovery from transient failures. The inner Hermes supervisor uses `:one_for_all` with its own `max_restarts: 3, max_seconds: 5` — a single transient failure exhausts the inner limit, so the outer needs headroom. A `TransportWatchdog` monitors the transport via `:sys.get_status` liveness ping (not just PID existence) and halts the BEAM if it stays dead or unresponsive for 15s. `Application.stop/1` calls `System.halt(1)` in MCP mode to prevent stale BEAM processes under `--no-halt`.
- **Stdio purity:** Logger routes to stderr only (`config :default_handler, config: %{type: :standard_error}`), Hermes logging disabled (`config :hermes_mcp, log: false`), log level `:warning`. Never log to stdout or re-enable Hermes logging — it corrupts JSON-RPC.
- **Re-entrance guard (CLI-only):** `ROUNDTABLE_ACTIVE=1` is injected into subprocess env and checked in `CLI.main/1`. Does NOT block direct `Roundtable.run/1` calls within the same VM.
- **Process cleanup (3 layers, Unix):** (1) Shell wrapper: `trap 'kill 0' EXIT` kills the CLI's process group on shell exit. (2) Orphan monitor: `run_cli` spawns a process that watches the caller and kills the group if the caller dies. (3) `after` block: `Platform.kill_tree/1` sends `kill -KILL -<os_pid>` (group kill) then `kill -KILL <os_pid>` (direct fallback). On Windows: `taskkill /F /T` instead. Always use `Platform` helpers, never hardcode Unix commands.
- **No setsid:** `setsid` would create a new session whose PGID differs from the PID returned by `Port.info`, breaking the `-<os_pid>` group kill. Port.open already gives children their own PGID.
- **Stdin isolation:** All CLI spawns redirect stdin from `Platform.null_device()` to prevent consuming MCP stdio bytes.
- **Stderr capture via temp file:** Stderr is routed to a private temp dir (`chmod 0700`) and read back after exit — not multiplexed over the Port. Do not refactor to read stderr from the Port; Erlang ports cannot cleanly demux stdout/stderr.
- **Output size caps:** stdout 1MB, probe stdout 64KB, stderr 512KB — all silently truncate. Handle `truncated: true` in results.
- **Shell escape mandatory:** All CLI arguments pass through `Platform.shell_escape/1`. Any new Port.open calls or CLI args MUST use it to prevent injection.
- **Ephemeral Task.Supervisor:** Dispatcher creates a fresh supervisor per dispatch, torn down in `after`. Scoped cleanup is intentional — do not refactor into a named persistent supervisor.
- **MCP tool error boundary:** `Common.dispatch/3` wraps in `rescue` + `catch`. All MCP tools MUST route through it, never call `Roundtable.run/1` directly.
- **ERL_CRASH_DUMP_SECONDS=0 (CLI-only):** Set in escript entrypoint to prevent crash dump stalls on `System.halt/1`.
- **ERL_CRASH_DUMP redirect (MCP release):** Set to `${RELEASE_ROOT}/erl_crash.dump` in `rel/env.sh.eex` so crash dumps land in the release directory, not the user's working directory.
- **Telemetry bounded flush:** OTEL gets up to 2s to transmit (`Task.yield` then `Task.shutdown(:brutal_kill)`). This intentionally blocks the caller briefly — it is not fire-and-forget.

### CLI Backend Contract

- **Behaviour:** All backends implement `Roundtable.CLI.Behaviour` — 3 callbacks: `probe_args/0`, `build_args/2`, `parse_output/2`. Return type for `parse_output`: map with `:response`, `:status` (`:ok | :error`, extensible — Gemini adds `:rate_limited`), `:parse_error`, `:metadata`, `:session_id`.
- **Executable resolution:** `ROUNDTABLE_<NAME>_PATH` > `ROUNDTABLE_EXTRA_PATH` dirs > system PATH. Missing executable produces `status: "not_found"` before any probe/run.
- **Probe before run:** Probes with `probe_args()` (`["--version"]`). Failure skips CLI with `status: "probe_failed"`. Probe has 5s internal timeout; `Task.yield` adds 1s grace.
- **Parallel dispatch:** All healthy CLIs run simultaneously. CLI command failures do not cancel sibling runs.
- **Output parsing per CLI:**
  - **Gemini:** Three-stage: (1) `Jason.decode(stdout)`, (2) `Jason.decode(stderr)`, (3) raw text with rate-limit classification. Extracts `response`, `session_id`, `stats.models.<name>.tokens`. Detects 429 from error fields/status/code/raw text.
  - **Codex:** JSONL stream. Collects `item.completed` where `item.type == "agent_message"`, joined with `\n\n`. Messages win over errors when both present. Thread ID from `thread.started`. Usage from `turn.completed`. Non-JSON lines skipped during reduction; if no events found, falls back to raw stdout with `parse_error`. Ignores stderr entirely.
  - **Claude:** Single JSON. Extracts `result`, `session_id`. Checks `is_error` boolean. Strips ANSI escape codes from model name strings in `modelUsage` keys. Ignores stderr.
- **Key type boundary:** Parsers return atom-keyed maps. `Output.build_result` converts to string-keyed maps. All output JSON uses string keys. Do not mix.
- **Status values (output strings):** `"ok"` | `"error"` | `"timeout"` | `"not_found"` | `"probe_failed"` | `"terminated"` | `"rate_limited"`.
- **Status priority in `build_result`:** `not_found` and `probe_failed` are separate clauses (fire before run). For run results: `timed_out` > `exit_signal` (`"terminated"`) > non-zero exit + parser `:ok` (forced to `"error"`) > parser status passthrough. Custom statuses like `:rate_limited` survive non-zero exit because the override only targets `:ok`.
- **Timeout replaces response:** When timed out, the response field is overwritten with a canned message. Partial CLI output is lost.
- **Model fallback:** `parsed.metadata.model_used` > caller-provided model > `"cli-default"`.
- **Session resume:** Gemini: `--resume <id|latest>` before `-p`. Codex: `resume <id|--last> <prompt>` appended to `exec --json ...` args (resume is nested under exec). Claude: `-r <session_id>`. Resume flags go before the prompt.
- **Codex arg structure:** Always starts with `exec --json --dangerously-bypass-approvals-and-sandbox`. Model/reasoning via `-c key=value`. Note: `codex_reasoning` is hardcoded nil in MCP dispatch — only available via CLI escript.
- **Role prompt resolution:** Project-local dir checked first, then global. MCP resolves global via `:code.priv_dir(:roundtable)` with fallback to `Path.expand("roles")` in dev. Raises on missing role.
- **Prompt assembly:** `<role prompt>\n\n=== REQUEST ===\n<user prompt>[\n\n=== FILES ===\n<file refs>\n\nReview the files listed above using your own tools to read their contents.]` — FILES block omitted when no files. Missing files rendered as `(unavailable)`.
- **Selective agent dispatch (`agents` param):** Optional JSON array on all MCP tools and `--agents` CLI flag. When provided, replaces the default 3-agent dispatch. Each entry: `{name, cli, model, role, resume}` — `cli` is required, `name` defaults to `cli` (must be unique, `"meta"` is reserved). Parsed/validated in `Common.parse_agents/1`, consumed by `Roundtable.agents_or_default/1`. When `agents` is present, per-tool model params (`gemini_model` etc.) are ignored. Allows same CLI with different names/models (e.g., two Codex instances). Existing per-tool role_config serves as default role; agent-level `role` overrides it.
- **Adding a new CLI backend:** Implement `Behaviour`, add to `cli_configs` in `Roundtable.run/1`, add param wiring in `common.ex` and `args.ex`, create MCP tool using `Common.dispatch/3`. Add fixture files and parser tests.

### Testing Rules

- **Platform-conditional tests:** `test_helper.exs` excludes `:unix` and `:linux` tags by OS. Tag Unix-only tests with `@moduletag :unix` or `@tag :unix`, Linux-specific with `@tag :linux`.
- **Fake CLI scripts:** Shell scripts in `test/support/bin/` (`gemini`, `codex`, `claude`, `gemini_timeout`, `gemini_rate_limited`, `gemini_echo`) and `test/support/fake_cli_*.sh` serve as mock CLIs. Must be `chmod 0o755` in test setup.
- **Mock CLI modules:** `test/support/mock_cli.ex` provides `Roundtable.MockCLI` and `Roundtable.MockErrorCLI`. Must implement `@behaviour Roundtable.CLI.Behaviour` with `@impl true` — do not duck-type.
- **Fixture files:** JSON/JSONL in `test/fixtures/`. Add fixtures when adding parser test cases.
- **E2E tests:** `server_e2e_test.exs` spins up a real MCP server via `mix run --no-halt` with `ROUNDTABLE_MCP=1`, sends JSON-RPC messages over stdio, verifies full round-trip. Uses `with_script_replacement/3` for scenario variants (rate limit, timeout).
- **CLI entrypoint tests:** `cli_test.exs` spawns a separate Elixir process via `System.cmd("elixir", ...)` with `-pa` flags to compiled EBEAMs. Cannot run `CLI.main/1` in-process because `System.halt` would kill the test VM. Args passed via `ROUNDTABLE_ARGS` env var.
- **Prompt echo pattern:** Tool tests use `with_echo_gemini/1` which swaps the gemini script with one that writes the received prompt to a temp file. This verifies prompt modifications by deepdive/architect/challenge tools.
- **MCP protocol compliance:** Every tool is tested for non-nil description and correct schema types (timeout as integer). New tools must pass these.
- **Orphan tests:** `runner_orphan_test.exs` verifies no orphaned processes after brutal task kills. Linux-only, `async: false`, 30s timeout.
- **Temp file leak tests:** `runner_test.exs` verifies no `rt_stderr_*` temp files left after `run_cli`. New file-based capture must include leak tests.
- **Async rules:** Parser and unit tests use `async: true`. Tests that manipulate PATH, spawn ports, or test process cleanup use `async: false`.
- **Timeout tags:** Long-running tests set `@moduletag timeout:` (30s for orphan tests, 60s for tool/CLI tests, 120s for E2E).
- **Env var cleanup:** Tests using `System.put_env` must restore via `after` or `on_exit`. PATH manipulation reversed in teardown.

### Cross-Platform & Build Rules

- **Platform abstraction:** Shell and process helpers live in `Roundtable.CLI.Platform`. Use `Platform.shell/0`, `Platform.shell_flag/0`, `Platform.null_device/0`, `Platform.kill_tree/1`, `Platform.shell_escape/1`, `Platform.wrap_run_command/1`, `Platform.path_separator/0`. Never hardcode shell commands or paths.
- **Shell execution:** Production Port.open calls use `:spawn_executable` with the system shell. Commands passed via `args: [Platform.shell_flag(), cmd]`.
- **Windows shell_escape is complex:** Strips `\r\n` (injection prevention), doubles `"` and `%`, escapes `! & | < > ( )` with `^`, wraps in double quotes. See `platform.ex` for full logic.
- **PGID isolation (Unix only):** `platform_test.exs` verifies Port.open children get a different PGID from the BEAM. This is a safety invariant — `kill 0` in the trap and `-<os_pid>` in kill_tree depend on it. Verified on macOS and Linux.
- **No distributed Erlang:** Release sets `RELEASE_DISTRIBUTION=none` via `rel/env.sh.eex`. No epmd. Do not introduce features requiring `:net_kernel`.
- **Release build:** `MIX_ENV=prod mix release roundtable_mcp`. `include_erts: false` — target host must have compatible Erlang/OTP installed. Output: `_build/prod/rel/roundtable_mcp/`.
- **Release entry point (Unix only):** `rel/overlays/bin/roundtable-mcp` is a POSIX shell wrapper that sets `ROUNDTABLE_MCP=1` and execs the release binary. No Windows equivalent currently exists.
- **`ROUNDTABLE_MCP` must be exactly `"1"`:** The check is `== "1"`, not truthy. `true`/`yes` will not start the MCP server.
- **Escript:** Tests assert `:escript` config with `main_module: Roundtable.CLI` and `name: "roundtable-cli"`. Integration tests build it in `setup_all`. Verify escript config exists in `mix.exs` before building.
- **Dev mode:** `ROUNDTABLE_MCP=1 mix run --no-halt`.
- **Hermes fork:** `hermes_mcp` is sourced from `github: "TejGandham/hermes-mcp"`. To sync with upstream: fetch, merge, push the fork's main branch.
- **Role directory resolution differs by entrypoint:** MCP uses `:code.priv_dir(:roundtable)` with fallback to `Path.expand("roles")`. CLI escript uses sibling `roles/` dir relative to the binary. Both support `project_roles_dir` override.
- **`priv/` directory:** Role prompts in `priv/roles/` are auto-bundled by the release — no overlay needed. `rel/overlays/` only adds the entry script.
- **Timeout validation differs:** CLI accepts any positive integer. MCP tools cap at 1-900s. Do not assume the same validation everywhere.
- **Exit code semantics:** Exit 0 for all dispatch results (including `not_found`, `probe_failed`, `timeout`, `rate_limited`). Exit 1 only for: arg errors, recursion guard, role file errors, or `Roundtable.run/1` returning `{:error, msg}`.

### Critical Don't-Miss Rules

**Never do:**
- **Write to stdout in MCP server/tool paths.** Stdout is the JSON-RPC transport. The escript entrypoint intentionally writes JSON to stdout — this rule applies only when running as an MCP server.
- **Let child CLIs inherit MCP stdin.** Always redirect stdin to `Platform.null_device()`. Without this, child processes consume JSON-RPC bytes from the MCP transport.
- **Remove the `trap 'kill 0' EXIT` wrapper in `run_cli`.** First cleanup layer for orphan prevention on Unix. Note: probes bypass this wrapper intentionally — they use shorter timeouts and the probe_receive loop handles cleanup directly.
- **Use `setsid`.** Creates a new session whose PGID differs from `Port.info(:os_pid)`, breaking group kill.
- **Refactor stderr to read from the Port.** Erlang ports cannot cleanly demux stdout/stderr. Temp file indirection is mandatory.
- **Unset `ROUNDTABLE_ACTIVE` in child env.** It's the recursion guard. Removing it allows infinite recursive invocations.
- **Change one timeout without checking the chain.** Run chain: MCP request (960s) > tool max (900s) > Task.await (tool + 10s). Probe chain: probe (5s) > Task.yield (probe + 1s). These are separate sequential phases, not nested.
- **Call `Roundtable.run/1` directly from MCP tools.** Use `Common.dispatch/3` — it adds Hermes reply formatting, timeout/file normalization, and `rescue`/`catch` for unexpected exceptions. `Roundtable.run/1` handles normal `{:ok|:error}` but doesn't format Hermes responses.
- **Skip `Platform.shell_escape/1` for CLI arguments.** Missing escaping is a command injection vulnerability.
- **Add your own try/rescue around role loading.** `Roles.load_role_prompt/3` raises `RuntimeError` with both searched paths. `Roundtable.run/1` already catches this and converts to `{:error, msg}`. Adding another rescue loses the error detail.
- **Return non-JSON from escript error paths.** All escript errors emit valid JSON. Arg/usage errors include `{"error": "...", "usage": "..."}`. Runtime errors include only `{"error": "..."}`. MCP tool errors use `Response.error()`.

**Always do:**
- **Clean up temp files in `after` blocks.** `run_cli` creates 0700 temp dirs for stderr capture. Note: the orphan cleanup monitor kills the process group but does not delete the temp dir — the `after` block handles that. If you add new file-based capture, ensure cleanup happens in `after`, and add leak tests.
- **Return `{:reply, Response.t(), frame}` from MCP tool callbacks** — matching what Hermes expects.
- **Be aware that `parse_files/1` splits on commas** — file paths containing literal commas will be corrupted. This is a known limitation of the comma-separated format.

---

## Usage Guidelines

**For AI Agents:**
- Read this file before implementing any code in this project
- Follow ALL rules exactly as documented
- When in doubt, prefer the more restrictive option
- Update this file if new patterns emerge

**For Humans:**
- Keep this file lean and focused on agent needs
- Update when technology stack or patterns change
- Review quarterly for outdated rules
- Remove rules that become obvious over time

Last Updated: 2026-04-05
