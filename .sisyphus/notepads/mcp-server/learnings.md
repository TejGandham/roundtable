# Learnings — mcp-server

## Codebase Structure
- `lib/roundtable.ex` — main module: `main/1` (escript entrypoint) + private `run/1` (core dispatch logic)
- `lib/roundtable/args.ex` — CLI arg parsing (OptionParser) → produces args map
- `lib/roundtable/dispatcher.ex` — Task.Supervisor based parallel dispatch, manages probe + run tasks
- `lib/roundtable/output.ex` — normalizes results, `encode/1` = Jason.encode! pretty
- `lib/roundtable/cli/` — per-CLI modules (gemini, codex, claude) with probe_args, build_args, parse_output
- `mix.exs` — currently has NO OTP application mod, no extra_applications except empty list; escript main_module: Roundtable

## Args Map Structure (from args.ex:51-69)
```
%{prompt, role, gemini_role, codex_role, files, gemini_model, codex_model, 
  timeout, roles_dir, project_roles_dir, codex_reasoning, 
  gemini_resume, codex_resume, claude_role, claude_model, claude_resume}
```
- `role` defaults to "default", others nil
- `files` is a list of strings (parsed from comma-separated)
- `timeout` is integer seconds (default 900)
- `roles_dir` defaults to `default_roles_dir()` — adjacent to script or Path.expand("roles")

## run/1 Logic (roundtable.ex:41-103)
1. Resolve gemini_role/codex_role/claude_role from args.role fallback
2. Load role prompts (Roles.load_role_prompt)
3. Assemble prompts (Assembler.assemble for each CLI)
4. Build cli_configs list with name/module/model/role/files/args/prompt
5. Dispatcher.dispatch(%{cli_configs, timeout_ms})
6. Telemetry.emit + IO.puts(Output.encode) + System.halt(0)

## Key Insight for MCP
- MCP tools replace step 1-5 but NOT System.halt(0) — instead return {:ok, json}
- Dispatcher.dispatch is UNCHANGED — MCP tools just call it the same way
- Need to extract core logic from run/1 into a shared function (Task 4/5)
- escript entrypoint must move to Roundtable.CLI module (Task 5)

## 2026-03-30 Implementation Notes
- MCP tool execute callbacks can return directly from `Roundtable.MCP.Tools.Common.dispatch/2` with role maps per command; xray requires per-model roles and `role: nil`.
- Shared `Common.dispatch/2` should build an args map that mirrors `Roundtable.Args.parse/1` output keys so existing pipeline (`Roundtable.run/1`) works unchanged.
- `Roundtable.run/1` should be side-effect free for transport reuse: return `{:ok, json}` / `{:error, message}` and leave `IO.puts` + `System.halt` in CLI layer.
- Default roles directory should fall back to project-root `roles/` when OTP priv dir is unavailable (`:code.priv_dir(:roundtable)` returns `{:error, _}`).

## 2026-03-30 Test Coverage (Task 6)
- Common.dispatch/2 tests use mock CLIs from test/support/bin/ via PATH override
- Prompt enhancement verified via gemini_echo mock that writes received prompt to /tmp file
- Erlang :trace_pattern works (returns 1 match) but trace MESSAGES are never delivered in this OTP build — possibly JIT-related; echo-mock approach used instead
- priv/roles/ must contain role .txt files for OTP-aware default_roles_dir; without them, all MCP tool tests fail with "Role prompt not found" against _build/test/lib/roundtable/priv/roles

## 2026-03-30 SKILL.md Rewrite (Task 7)
- Moved MCP tools to primary invocation section; CLI demoted to secondary/scripting
- MCP params use underscore naming (gemini_resume, codex_resume, etc.) to match tool schemas
- Removed Bash invocation examples from primary path; kept CLI section with all flags intact
- Follow-up conversation examples rewritten as MCP tool calls (no bash blocks)
- Added "Using Bash tool to call roundtable" as a Mistakes to Avoid entry
- Synthesis template preserved verbatim
- YAML frontmatter unchanged
# Learnings

## Task 8: INSTALL.md MCP Registration (2026-03-30)

- Positioned MCP registration as the primary section, before skill discovery and CLI install
- Used `claude mcp add -s user roundtable -- mix run --no-halt` from project root as the canonical Claude Code registration command (matches plan's success criteria)
- OpenCode MCP config uses `cwd` field pointing to cloned repo — no build step needed, just `mix run --no-halt`
- Preserved all CLI install sections (release download, multi-agent symlink, workspace-level) but moved them under "CLI Installation (Alternative)" heading
- Updated binary name from `roundtable` to `roundtable-cli` throughout CLI sections to match Task 5 rename
- Kept Gemini participant/orchestrator note and OpenCode skill format note at the bottom
- Removed the old OpenCode Option A/B workaround section — MCP registration makes it obsolete
- Tool names documented as `roundtable_hivemind`, `roundtable_deepdive`, `roundtable_architect`, `roundtable_challenge`, `roundtable_xray` (matching SKILL.md from Task 7)

## 2026-03-30 Stdio MCP Init Fix (Task 9)

- `config/config.exs` already present with three key settings:
  1. `config :hermes_mcp, log: false` — suppresses Hermes internal logging on stdio
  2. `config :logger, level: :warning` — reduces OTP logger noise
  3. `config :logger, :default_handler, config: %{type: :standard_error}` — redirects logger to stderr
- These three lines are sufficient to prevent JSON-RPC corruption on stdio transport
- `mix test` passes (147 tests, 0 failures) — GenServer `:eof` errors during tests are expected (stdin closed in test env)
- `mix hermes.stdio.interactive --command mix --args=run,--no-halt` initializes cleanly, lists all 5 tools
