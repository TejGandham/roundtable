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
