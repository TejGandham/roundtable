---
title: 'Selective Agent Dispatch'
type: 'feature'
created: '2026-04-03'
status: 'in-progress'
baseline_commit: '1a7fedb'
context: ['docs/project-context.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Roundtable hardcodes dispatch to all 3 CLI agents (gemini, codex, claude). Callers cannot skip agents or invoke the same CLI type with different models (e.g., codex with gpt-5.4 AND codex with gpt-5.3-codex). This limits flexibility for callers who want targeted consensus or cost control.

**Approach:** Add an optional `agents` JSON parameter to all MCP tools. When provided, it replaces the default 3-agent config with a caller-defined list of `{name, cli, model, role}` entries. When omitted, behavior is unchanged (backwards compatible). `Roundtable.run/1` builds `cli_configs` from the agents list instead of hardcoding three entries.

## Boundaries & Constraints

**Always:**
- Backwards compatible: omitting `agents` dispatches all 3 CLIs with existing behavior
- Each agent entry must specify at least `cli` (which CLI backend to use: "gemini", "codex", or "claude")
- Agent `name` defaults to `cli` value if omitted; must be unique across entries (used as result key)
- Existing per-tool model/role params still work when `agents` is not provided
- Dispatcher and Output remain agent-count-agnostic (they already are)

**Ask First:**
- Changes to the JSON output schema beyond adding new agent keys

**Never:**
- Break existing MCP tool signatures (all current params remain)
- Add new CLI backend types in this feature (only gemini/codex/claude)
- Change Dispatcher or Output internals (they already handle variable agent counts)

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|-|-|-|-|
| Default (no agents) | `{prompt: "hi"}` | Dispatches gemini + codex + claude as today | N/A |
| Skip one agent | `{prompt: "hi", agents: [{"cli":"gemini"}, {"cli":"codex"}]}` | Only gemini + codex in results, no claude key | N/A |
| Custom model | `{prompt: "hi", agents: [{"cli":"gemini","model":"gemini-2.5-pro"}]}` | Gemini invoked with specified model | N/A |
| Named agents | `{prompt: "hi", agents: [{"name":"fast","cli":"codex","model":"gpt-5.4"}, {"name":"deep","cli":"codex","model":"gpt-5.3-codex"}]}` | Two results keyed "fast" and "deep", both using Codex backend | N/A |
| Duplicate names | `agents: [{"cli":"gemini"}, {"cli":"gemini"}]` | Error: duplicate agent names | Return error via Common.dispatch |
| Invalid CLI | `agents: [{"cli":"bard"}]` | Error: unknown CLI type | Return error via Common.dispatch |
| Empty agents list | `agents: []` | Error: agents list cannot be empty | Return error via Common.dispatch |

</frozen-after-approval>

## Code Map

- `lib/roundtable/mcp/tools/common.ex` -- Parse+validate `agents` param, build args
- `lib/roundtable.ex` -- Replace hardcoded 3-agent cli_configs with dynamic builder from agents list
- `lib/roundtable/mcp/tools/hivemind.ex` -- Add `agents` field to schema (same for all 5 tools)
- `lib/roundtable/mcp/tools/deepdive.ex` -- Add `agents` field to schema
- `lib/roundtable/mcp/tools/architect.ex` -- Add `agents` field to schema
- `lib/roundtable/mcp/tools/challenge.ex` -- Add `agents` field to schema
- `lib/roundtable/mcp/tools/xray.ex` -- Add `agents` field to schema + adapt per-model role logic
- `lib/roundtable/args.ex` -- Add `--agents` CLI flag (JSON string)
- `test/roundtable/mcp/tools/common_test.exs` -- Test agents param parsing and validation
- `test/roundtable/mcp/tools/tools_test.exs` -- Test tools with agents param

## Tasks & Acceptance

**Execution:**
- [x] `lib/roundtable/mcp/tools/hivemind.ex` + 4 siblings -- Add `agents` string field to all 5 tool schemas
- [x] `lib/roundtable/mcp/tools/common.ex` -- Parse `agents` JSON string into list, validate (known CLIs, unique names, non-empty), map to internal agent configs. When nil, fall back to current 3-agent default.
- [x] `lib/roundtable.ex` -- Refactor `run/1` to accept `agents` list in args and build `cli_configs` dynamically. When `agents` is nil/empty, preserve current hardcoded 3-agent behavior.
- [x] `lib/roundtable/args.ex` -- Add `--agents` string flag for CLI escript path
- [x] `test/roundtable/mcp/tools/common_test.exs` -- Test agents parsing: default (nil), valid list, skip agent, duplicate names, invalid CLI, named agents
- [x] `test/roundtable/mcp/tools/tools_test.exs` -- Test hivemind with agents param selecting subset

**Acceptance Criteria:**
- Given no `agents` param, when calling any MCP tool, then all 3 default agents dispatch (backwards compatible)
- Given `agents` with 2 entries, when calling hivemind, then only those 2 agents appear in results
- Given `agents` with duplicate names, when calling any tool, then error returned
- Given `agents` with same CLI but different names/models, when calling hivemind, then both run in parallel with separate result keys

## Verification

**Commands:**
- `mix test` -- expected: all tests pass including new agents tests
- `mix test test/roundtable/mcp/tools/common_test.exs` -- expected: agents parsing tests pass
- `mix test test/roundtable/mcp/tools/tools_test.exs` -- expected: tool tests with agents param pass

## Design Notes

The `agents` param is a JSON string (MCP tools receive strings, not structured types):
```json
[
  {"name": "fast", "cli": "codex", "model": "gpt-5.4"},
  {"name": "deep", "cli": "codex", "model": "gpt-5.3-codex", "role": "codereviewer"}
]
```

Each entry fields:
- `name` (optional, defaults to `cli`) -- Result key in output JSON
- `cli` (required) -- Backend: "gemini" | "codex" | "claude"  
- `model` (optional) -- Model override for this agent
- `role` (optional) -- Role override for this agent (overrides tool's role_config)

When `agents` is provided, the per-tool role_config serves as the default role, but individual agent `role` overrides take precedence. The existing `gemini_model`/`codex_model`/`claude_model` params are ignored when `agents` is present.

For xray's per-model role assignment: when `agents` is provided, xray's built-in role mapping (gemini=planner, codex=codereviewer) becomes the default role for agents using those CLIs, but the agent's explicit `role` field still overrides.
