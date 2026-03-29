# Elixir Port of Roundtable

## TL;DR

> **Quick Summary**: Port roundtable from Node.js (585 lines, single file) to Elixir as an escript with identical CLI interface and JSON output contract. Uses OTP Task.Supervisor for parallel CLI dispatch, Behaviour for extensible CLI backends, and TDD with ExUnit throughout.
> 
> **Deliverables**:
> - Elixir Mix project in this repo with escript build
> - `./roundtable` binary with identical CLI flags and JSON output to `node roundtable.mjs`
> - Full ExUnit test suite (TDD — tests written first)
> - Updated SKILL.md pointing to Elixir binary
> - Behaviour-based CLI backend interface for future extensibility
> 
> **Estimated Effort**: Medium-Large
> **Parallel Execution**: YES — 4 waves + final verification
> **Critical Path**: Task 1 → Tasks 2-7 (parallel) → Tasks 8-10 (parallel) → Tasks 11-12 → Task 13 → F1-F4

---

## Context

### Original Request
Port the roundtable Node.js CLI tool to Elixir, using latest Elixir features. The tool dispatches prompts to Gemini and Codex CLIs in parallel, captures their JSON/JSONL output, and produces a unified JSON result.

### Interview Summary
**Key Discussions**:
- **Deployment**: Escript (Burrito researched and rejected — adds Zig build complexity for minimal benefit since users already install gemini/codex CLIs)
- **Test strategy**: TDD with ExUnit — tests written before implementation
- **Project location**: Current repo (`/projects/roundtable`), standalone
- **Extensibility**: Behaviour-based CLI backends so new CLIs can plug in later
- **Elixir version**: Latest (1.18+), all modern features available

**Research Findings**:
- Full Node.js source analyzed: 15 functions, detached process groups, SIGTERM→SIGKILL escalation, 1MB output cap, health probes
- Elixir Port.open has NO separate stderr capture — requires shell wrapper with stderr redirect to temp file
- Elixir Port.open does NOT call setsid() — requires shell wrapper for process group creation
- Solution: `/bin/sh -c 'exec 2>/tmp/rt_stderr_$$; exec setsid <command>'` solves both problems
- Task.yield_many provides allSettled-like semantics, but timed-out tasks need explicit shutdown + OS process kill
- System.find_executable/1 is equivalent to Node.js findExecutable (checks execute permission)
- Jason encodes maps with undefined key ordering — use `jq -S` for structural JSON comparison, not byte-for-byte

### Metis Review
**Identified Gaps** (addressed):
- **Stderr separation**: Shell wrapper approach adopted (setsid + stderr redirect to temp file)
- **JSON parity definition**: Structural equivalence via `jq -S` sorting, not byte-for-byte
- **OTP over-engineering**: Removed Application + GenServer. Direct Task.Supervisor.start_link in main/1
- **Recursion guard**: Stays as ROUNDTABLE_ACTIVE env var (cross-process detection requires env var, not process registry)
- **Timeout default**: 900s from source code (DESIGN.md's 120s was wrong)
- **Role file location**: Relative to escript binary via `:filename.dirname(:escript.script_name())`
- **Positional arg as prompt**: Handle OptionParser `rest` tuple for bare prompt without `--prompt` flag
- **Probes must be parallel**: Both probes via Task.async, not sequential System.cmd
- **Explicit System.halt**: Required at end of main/1 to prevent hanging on Task.Supervisor children
- **Known Node.js bugs**: Fixed in Elixir port (killTimer cleanup, probeFailResult exit_code, status downgrade order)

---

## Work Objectives

### Core Objective
Create a functionally equivalent Elixir escript that produces structurally identical JSON output to the Node.js version for all inputs and error conditions, while leveraging OTP patterns to eliminate the manual process management complexity.

### Concrete Deliverables
- `mix.exs` with escript config and Jason dependency
- `lib/roundtable.ex` — main/1 entry point + OptionParser
- `lib/roundtable/cli/behaviour.ex` — callback definitions
- `lib/roundtable/cli/runner.ex` — Port + shell wrapper + timeout
- `lib/roundtable/cli/gemini.ex` — Gemini arg builder + JSON parser
- `lib/roundtable/cli/codex.ex` — Codex arg builder + JSONL parser
- `lib/roundtable/prompt/assembler.ex` — prompt assembly
- `lib/roundtable/prompt/roles.ex` — role file resolution
- `lib/roundtable/dispatcher.ex` — parallel dispatch via Task.Supervisor
- `lib/roundtable/output.ex` — result builder + JSON encoding
- `test/` directory with ExUnit tests for every module
- `test/fixtures/` with JSON/JSONL fixture files
- `test/support/fake_cli.sh` — mock CLI scripts for integration tests
- Updated `SKILL.md` pointing to `./roundtable`

### Definition of Done
- [ ] `mix test` passes with zero failures
- [ ] `mix escript.build` produces working `./roundtable` binary
- [ ] `./roundtable --prompt "test" --timeout 30` produces valid JSON to stdout
- [ ] Output matches Node.js version structurally (`jq -S` comparison) for: success, one-fail, both-fail, timeout, not-found, probe-failed scenarios
- [ ] `ROUNDTABLE_ACTIVE=1 ./roundtable --prompt "test"` exits 1 with error JSON
- [ ] No orphan processes after SIGTERM (verified via `pgrep`)

### Must Have
- Identical CLI flags with same defaults (including 900s timeout)
- Identical JSON output structure (all field names and types match)
- All 6 status values: ok, error, timeout, terminated, not_found, probe_failed
- Recursion guard via ROUNDTABLE_ACTIVE env var
- Health probes with 5s timeout (parallel, not sequential)
- 1MB output cap with truncated flag
- Process group kill on timeout (SIGTERM → 3s → SIGKILL)
- Separate stderr capture via shell wrapper
- Role file resolution: project-local → global fallback
- Positional argument accepted as prompt (no --prompt flag required)
- Behaviour-based CLI backend interface

### Must NOT Have (Guardrails)
- **No OTP Application module** — no application.ex, no named supervisors in registry
- **No GenServer** — all state flows through function arguments, no process state
- **No dependencies beyond Jason** — no erlexec, no porcelain, no optimus
- **No Logger** — one JSON blob to stdout, nothing else. No debug mode.
- **No config system** — no config/config.exs, no Application env, no YAML/TOML
- **No streaming output** — buffer everything, emit one JSON blob (matching Node.js)
- **No retry logic** — run once, report result (matching Node.js)
- **No extra CLI backends** — Behaviour exists for extensibility, but ship with Gemini + Codex ONLY
- **No Hex publishing** — out of scope
- **No CI/CD pipeline** — out of scope for initial port
- **No Telemetry** — elapsed_ms in output is sufficient

---

## Verification Strategy

> **ZERO HUMAN INTERVENTION** — ALL verification is agent-executed. No exceptions.

### Test Decision
- **Infrastructure exists**: NO (greenfield Elixir project)
- **Automated tests**: TDD — tests written BEFORE implementation
- **Framework**: ExUnit (Elixir built-in)
- **TDD workflow**: Each task follows RED (failing test) → GREEN (minimal impl) → REFACTOR

### Test Infrastructure (created in Task 1)
- ExUnit configured in `test/test_helper.exs`
- Fixture files in `test/fixtures/` (JSON, JSONL samples from Node.js)
- Fake CLI scripts in `test/support/` for integration tests
- Behaviour-based mock injection for unit tests

### QA Policy
Every task MUST include agent-executed QA scenarios.
Evidence saved to `.sisyphus/evidence/task-{N}-{scenario-slug}.{ext}`.

- **Module tests**: Use `mix test test/module_test.exs` — assert exact shapes
- **Integration tests**: Use Bash — run escript with args, parse JSON output, assert fields
- **Process cleanup tests**: Use Bash — spawn, SIGTERM, pgrep for orphans

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Start Immediately — project scaffolding):
└── Task 1: Mix project setup + ExUnit + Jason + .formatter.exs [quick]

Wave 2 (After Wave 1 — pure modules, MAX PARALLEL):
├── Task 2: CLI Behaviour definition [quick]
├── Task 3: Prompt.Roles — file loading + fallback [quick]
├── Task 4: Prompt.Assembler — prompt assembly + file refs [quick]
├── Task 5: CLI.Gemini — arg builder + JSON parser [unspecified-high]
├── Task 6: CLI.Codex — arg builder + JSONL parser [unspecified-high]
└── Task 7: Output — result builder + JSON encoding [unspecified-high]

Wave 3 (After Wave 2 — IO + orchestration):
├── Task 8: CLI.Runner — Port + shell wrapper + timeout + kill (depends: 2) [deep]
├── Task 9: Argument parsing module (depends: none beyond Wave 1) [quick]
└── Task 10: Dispatcher — parallel dispatch (depends: 2, 8) [deep]

Wave 4 (After Wave 3 — entry point + end-to-end):
├── Task 11: Main entry point + escript build (depends: 3, 4, 7, 9, 10) [deep]
├── Task 12: Integration tests with mock CLI scripts (depends: 11) [unspecified-high]
└── Task 13: SKILL.md update + output parity verification (depends: 11) [quick]

Wave FINAL (After ALL tasks — 4 parallel reviews, then user okay):
├── Task F1: Plan compliance audit (oracle)
├── Task F2: Code quality review (unspecified-high)
├── Task F3: Real manual QA (unspecified-high)
└── Task F4: Scope fidelity check (deep)
→ Present results → Get explicit user okay

Critical Path: Task 1 → Task 2 → Task 8 → Task 10 → Task 11 → Task 12 → F1-F4 → user okay
Parallel Speedup: ~60% faster than sequential
Max Concurrent: 6 (Wave 2)
```

### Dependency Matrix

| Task | Blocked By | Blocks |
|-|-|-|
| 1 | — | 2, 3, 4, 5, 6, 7, 8, 9, 10, 11 |
| 2 | 1 | 5, 6, 8, 10 |
| 3 | 1 | 11 |
| 4 | 1 | 11 |
| 5 | 1, 2 | 12 |
| 6 | 1, 2 | 12 |
| 7 | 1 | 11 |
| 8 | 1, 2 | 10 |
| 9 | 1 | 11 |
| 10 | 2, 8 | 11 |
| 11 | 3, 4, 7, 9, 10 | 12, 13 |
| 12 | 11 | F1-F4 |
| 13 | 11 | F1-F4 |

### Agent Dispatch Summary

- **Wave 1**: **1** — T1 → `quick`
- **Wave 2**: **6** — T2 → `quick`, T3 → `quick`, T4 → `quick`, T5 → `unspecified-high`, T6 → `unspecified-high`, T7 → `unspecified-high`
- **Wave 3**: **3** — T8 → `deep`, T9 → `quick`, T10 → `deep`
- **Wave 4**: **3** — T11 → `deep`, T12 → `unspecified-high`, T13 → `quick`
- **FINAL**: **4** — F1 → `oracle`, F2 → `unspecified-high`, F3 → `unspecified-high`, F4 → `deep`

---

## TODOs

- [x] 1. Mix Project Scaffolding + ExUnit + Jason

  **What to do**:
  - **DO NOT run `mix new`** — it creates a nested subdirectory. Instead, manually scaffold the Mix project IN-PLACE at the repo root (`/projects/roundtable`):
    - Create `mix.exs` at repo root with: `app: :roundtable`, `version: "0.1.0"`, `elixir: "~> 1.18"`, `deps: [{:jason, "~> 1.4"}]`, `escript: [main_module: Roundtable]`
    - Create `lib/roundtable.ex` with stub `def main(_args), do: :ok`
    - Create `test/test_helper.exs` with `ExUnit.start()`
    - Create `.formatter.exs` with `[inputs: ["{config,lib,test}/**/*.{ex,exs}"]]`
  - Existing files (`roundtable.mjs`, `SKILL.md`, `DESIGN.md`, `docs/`, `roles/`, `README.md`) remain at repo root untouched — do NOT move or delete them
  - The resulting structure at repo root: `mix.exs`, `lib/`, `test/`, `.formatter.exs` alongside existing `roundtable.mjs`, `SKILL.md`, `roles/`, etc.
  - Create `test/fixtures/` directory with fixture files extracted from Node.js source:
    - `test/fixtures/gemini_success.json` — valid Gemini JSON output: `{"response":"test response","stats":{"models":{"gemini-2.5-pro":{"tokens":{"input":100,"output":50}}}},"session_id":"ses_abc123"}`
    - `test/fixtures/gemini_error.json` — Gemini error: `{"error":{"message":"Rate limit exceeded"}}`
    - `test/fixtures/gemini_stderr_error.json` — error in stderr format: `{"error":{"message":"Auth failed"}}`
    - `test/fixtures/codex_success.jsonl` — valid Codex JSONL with thread.started, item.completed (agent_message), turn.completed events
    - `test/fixtures/codex_errors.jsonl` — Codex JSONL with error events
    - `test/fixtures/codex_empty.jsonl` — empty/blank lines only
  - Create stub `lib/roundtable.ex` with `def main(_args), do: :ok`
  - Run `mix deps.get` and verify: `mix compile` succeeds, `mix test` runs (0 tests, 0 failures), `mix escript.build` produces `./roundtable` binary

  **Must NOT do**:
  - Run `mix new` (creates nested subdirectory — scaffold manually instead)
  - Delete any existing files (roundtable.mjs, SKILL.md, etc.)
  - Add any dependency beyond Jason
  - Add config/config.exs

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []
    - No specialized skills needed — standard Mix project setup

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 1 (solo)
  - **Blocks**: Tasks 2, 3, 4, 5, 6, 7, 8, 9, 10, 11
  - **Blocked By**: None (starts immediately)

  **References**:

  **Pattern References**:
  - `roundtable.mjs:1-8` — Existing file structure to preserve alongside new Elixir code
  - `docs/elixir-port.md:56-69` — Module structure from design doc (reference, not gospel — we simplified per Metis)

  **API/Type References**:
  - Elixir Mix.Project — escript configuration: `escript: [main_module: Roundtable]`

  **External References**:
  - Jason hex.pm: `{:jason, "~> 1.4"}` — only dependency

  **WHY Each Reference Matters**:
  - `roundtable.mjs` must be preserved because both Node.js and Elixir versions coexist during migration
  - Design doc module structure is the starting point but simplified (no Application/GenServer)

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Mix project compiles cleanly
    Tool: Bash
    Preconditions: In /projects/roundtable directory
    Steps:
      1. Run `mix deps.get`
      2. Run `mix compile --warnings-as-errors`
      3. Assert exit code 0
    Expected Result: Zero warnings, zero errors
    Failure Indicators: Non-zero exit code or warning output
    Evidence: .sisyphus/evidence/task-1-compile.txt

  Scenario: ExUnit runs with zero test count
    Tool: Bash
    Preconditions: mix compile succeeds
    Steps:
      1. Run `mix test`
      2. Assert output contains "0 tests"
      3. Assert exit code 0
    Expected Result: "0 tests, 0 failures" or similar
    Failure Indicators: Non-zero exit code
    Evidence: .sisyphus/evidence/task-1-test.txt

  Scenario: Escript builds successfully
    Tool: Bash
    Preconditions: mix compile succeeds
    Steps:
      1. Run `mix escript.build`
      2. Assert file `./roundtable` exists
      3. Run `./roundtable` (should exit 0 since main returns :ok)
    Expected Result: Binary produced, executes without crash
    Failure Indicators: Build failure or runtime crash
    Evidence: .sisyphus/evidence/task-1-escript.txt

  Scenario: Existing files preserved
    Tool: Bash
    Preconditions: After mix new
    Steps:
      1. Assert `roundtable.mjs` exists
      2. Assert `SKILL.md` exists
      3. Assert `roles/default.txt` exists
      4. Assert `roles/planner.txt` exists
      5. Assert `roles/codereviewer.txt` exists
    Expected Result: All original files intact
    Failure Indicators: Any file missing
    Evidence: .sisyphus/evidence/task-1-files-preserved.txt

  Scenario: Fixture files created correctly
    Tool: Bash
    Preconditions: Project setup complete
    Steps:
      1. Assert `test/fixtures/gemini_success.json` is valid JSON (`cat test/fixtures/gemini_success.json | jq .`)
      2. Assert `test/fixtures/codex_success.jsonl` has multiple lines, each valid JSON
    Expected Result: All fixtures parse correctly
    Failure Indicators: jq parse errors
    Evidence: .sisyphus/evidence/task-1-fixtures.txt
  ```

  **Commit**: YES
  - Message: `feat(elixir): scaffold Mix project with ExUnit and Jason`
  - Files: `mix.exs`, `mix.lock`, `lib/roundtable.ex`, `test/test_helper.exs`, `.formatter.exs`, `test/fixtures/*`
  - Pre-commit: `mix compile --warnings-as-errors`

- [x] 2. CLI Behaviour Definition

  **What to do**:
  - RED: Write `test/roundtable/cli/behaviour_test.exs` that verifies:
    - A module implementing the behaviour compiles without warnings
    - The behaviour defines the expected callbacks
  - GREEN: Create `lib/roundtable/cli/behaviour.ex` defining `Roundtable.CLI.Behaviour` with exactly these callbacks:
    ```elixir
    @callback probe_args() :: [String.t()]
    @callback build_args(args :: map(), prompt :: String.t()) :: [String.t()]
    @callback parse_output(stdout :: String.t(), stderr :: String.t()) :: map()
    ```
    - `probe_args/0` — returns args for health check (e.g., `["--version"]`)
    - `build_args/2` — takes parsed args map + assembled prompt, returns CLI arg list
    - `parse_output/2` — takes stdout + stderr strings, returns parsed result map with keys: `:response`, `:status`, `:parse_error`, `:metadata`, `:session_id`
  - Also define the common result type as a `@type`:
    ```elixir
    @type parse_result :: %{
      response: String.t(),
      status: :ok | :error,
      parse_error: String.t() | nil,
      metadata: map(),
      session_id: String.t() | nil
    }
    ```
  - REFACTOR: Add `@doc` annotations to each callback

  **Must NOT do**:
  - Add `configure/1`, `validate/1`, `capabilities/0`, or health check callbacks
  - Add `@optional_callbacks`
  - Create any implementation modules (those are Tasks 5, 6)

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Tasks 3, 4, 5, 6, 7)
  - **Blocks**: Tasks 5, 6, 8, 10
  - **Blocked By**: Task 1

  **References**:

  **Pattern References**:
  - `roundtable.mjs:189-237` — parseGeminiOutput signature and return shape → defines parse_output callback contract
  - `roundtable.mjs:241-312` — parseCodexOutput signature and return shape → same contract, different implementation
  - `roundtable.mjs:531-548` — buildGeminiArgs/buildCodexArgs → defines build_args callback contract
  - `roundtable.mjs:99-135` — probeCli uses `['--version']` → defines probe_args callback

  **WHY Each Reference Matters**:
  - The callback signatures must match what Gemini and Codex implementations need — derived directly from Node.js function signatures

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/cli/behaviour_test.exs`
  - [ ] `mix test test/roundtable/cli/behaviour_test.exs` → PASS

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Behaviour module compiles and defines callbacks
    Tool: Bash
    Preconditions: Task 1 complete
    Steps:
      1. Run `mix compile --warnings-as-errors`
      2. Run `mix test test/roundtable/cli/behaviour_test.exs`
      3. Assert exit code 0
    Expected Result: Compiles cleanly, tests pass
    Failure Indicators: Compilation warnings about undefined callbacks
    Evidence: .sisyphus/evidence/task-2-behaviour.txt

  Scenario: Missing callback produces compile warning
    Tool: Bash
    Preconditions: Behaviour module exists
    Steps:
      1. Create a temporary module that `@behaviour Roundtable.CLI.Behaviour` but omits `parse_output/2`
      2. Run `mix compile` and capture stderr
      3. Assert warning about missing callback
    Expected Result: Warning: "function parse_output/2 required by behaviour Roundtable.CLI.Behaviour is not implemented"
    Failure Indicators: No warning produced
    Evidence: .sisyphus/evidence/task-2-missing-callback.txt
  ```

  **Commit**: YES
  - Message: `feat(cli): define CLI behaviour callbacks`
  - Files: `lib/roundtable/cli/behaviour.ex`, `test/roundtable/cli/behaviour_test.exs`
  - Pre-commit: `mix test`

- [x] 3. Prompt.Roles — Role File Loading with Fallback

  **What to do**:
  - RED: Write `test/roundtable/prompt/roles_test.exs` with tests for:
    - Load role from project-local dir (first priority)
    - Fallback to global dir when project-local file doesn't exist (ENOENT)
    - Raise error when role not found in either location (with searched paths in message)
    - Permission errors are NOT swallowed (re-raised, not treated as ENOENT)
    - Load each built-in role: default, planner, codereviewer
  - GREEN: Create `lib/roundtable/prompt/roles.ex` with:
    ```elixir
    @spec load_role_prompt(role_name :: String.t(), global_dir :: String.t(), project_dir :: String.t() | nil) :: String.t()
    ```
    - If `project_dir` is non-nil, try `Path.join(project_dir, "#{role_name}.txt")` via `File.read/1`
    - On `{:error, :enoent}`, fall through to global dir
    - On other errors (`:eacces`, `:eisdir`), raise with clear message
    - Try `Path.join(global_dir, "#{role_name}.txt")`
    - On `:enoent`, raise `"Role prompt not found: #{role_name} (searched #{project_dir || "none"}, #{global_dir})"`
  - Use test fixtures: create temp dirs with role files in tests using `tmp_dir` ExUnit feature

  **Must NOT do**:
  - Add caching or memoization
  - Support non-.txt file extensions
  - Search additional directories beyond the two provided

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Tasks 2, 4, 5, 6, 7)
  - **Blocks**: Task 11
  - **Blocked By**: Task 1

  **References**:

  **Pattern References**:
  - `roundtable.mjs:139-158` — loadRolePrompt function: exact fallback logic, ENOENT-only fallthrough, permission error propagation
  - `roles/default.txt` — Example role file content (9 lines, plain text with `<SUMMARY>` protocol)
  - `roles/planner.txt` — Structured role (12 lines, numbered sections)
  - `roles/codereviewer.txt` — Review role (16 lines, severity ratings)

  **WHY Each Reference Matters**:
  - `loadRolePrompt` is the source of truth for fallback logic — Elixir version must match exactly (ENOENT only, not all errors)
  - Role files show the actual content format to verify loading works correctly

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/prompt/roles_test.exs`
  - [ ] `mix test test/roundtable/prompt/roles_test.exs` → PASS (5+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Load role from project-local directory
    Tool: Bash
    Preconditions: Test creates temp dir with custom role file
    Steps:
      1. Run `mix test test/roundtable/prompt/roles_test.exs --trace`
      2. Assert test "loads from project dir when file exists" passes
    Expected Result: Returns content of project-local role file
    Failure Indicators: Falls through to global when project-local exists
    Evidence: .sisyphus/evidence/task-3-roles-test.txt

  Scenario: Role not found raises descriptive error
    Tool: Bash
    Preconditions: Neither dir contains the role file
    Steps:
      1. Run `mix test test/roundtable/prompt/roles_test.exs --trace`
      2. Assert test "raises when role not found" passes
      3. Assert error message contains both searched paths
    Expected Result: Error message like "Role prompt not found: badname (searched /tmp/project, /tmp/global)"
    Failure Indicators: Generic error without path info
    Evidence: .sisyphus/evidence/task-3-not-found.txt
  ```

  **Commit**: YES (groups with Task 4)
  - Message: `feat(prompt): add role loading and prompt assembly`
  - Files: `lib/roundtable/prompt/roles.ex`, `test/roundtable/prompt/roles_test.exs`
  - Pre-commit: `mix test`

- [x] 4. Prompt.Assembler — Prompt Assembly + File References

  **What to do**:
  - RED: Write `test/roundtable/prompt/assembler_test.exs` with tests for:
    - Assemble prompt with role + request (no files)
    - Assemble prompt with role + request + file references
    - File references show path + size for existing files
    - File references show "(unavailable)" for missing files
    - Empty file list produces no FILES section
    - Whitespace trimming on role prompt and request
  - GREEN: Create `lib/roundtable/prompt/assembler.ex` with:
    ```elixir
    @spec assemble(role_prompt :: String.t(), user_request :: String.t(), file_paths :: [String.t()]) :: String.t()
    @spec format_file_references(file_paths :: [String.t()]) :: String.t() | nil
    ```
    - `format_file_references/1`: For each path, `File.stat/1` to get size → `"- #{path} (#{size} bytes)"`. On error → `"- #{path} (unavailable)"`. Prefix with `"=== FILES ===\n"`, suffix with `"\n\nReview the files listed above using your own tools to read their contents."`
    - `assemble/3`: Join `[String.trim(role_prompt), "=== REQUEST ===\n" <> String.trim(user_request), file_refs]` with `"\n\n"`, omitting nil file_refs

  **Must NOT do**:
  - Read file contents (only path + size metadata)
  - Add any formatting beyond what Node.js produces
  - Handle directories (files only)

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Tasks 2, 3, 5, 6, 7)
  - **Blocks**: Task 11
  - **Blocked By**: Task 1

  **References**:

  **Pattern References**:
  - `roundtable.mjs:162-176` — formatFileReferences: exact format string with `=== FILES ===` header, `(N bytes)` or `(unavailable)`, and the "Review the files..." suffix
  - `roundtable.mjs:180-185` — assemblePrompt: join sections with double newline, trim inputs
  - `DESIGN.md:73-86` — Prompt assembly format showing exact section delimiters

  **WHY Each Reference Matters**:
  - Format strings must be character-for-character identical to Node.js — CLIs parse based on `=== REQUEST ===` and `=== FILES ===` delimiters

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/prompt/assembler_test.exs`
  - [ ] `mix test test/roundtable/prompt/assembler_test.exs` → PASS (6+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Prompt assembly matches Node.js format
    Tool: Bash
    Preconditions: Tests use known role text and request
    Steps:
      1. Run `mix test test/roundtable/prompt/assembler_test.exs --trace`
      2. Assert test "assembles with files" produces exact format:
         "<role>\n\n=== REQUEST ===\n<request>\n\n=== FILES ===\n- path (N bytes)\n\nReview the files..."
    Expected Result: All tests pass, format matches Node.js exactly
    Failure Indicators: Missing section delimiters, wrong whitespace
    Evidence: .sisyphus/evidence/task-4-assembler-test.txt

  Scenario: Unavailable files handled gracefully
    Tool: Bash
    Preconditions: File reference points to non-existent path
    Steps:
      1. Run test that calls format_file_references(["/nonexistent/file.txt"])
      2. Assert output contains "- /nonexistent/file.txt (unavailable)"
    Expected Result: "(unavailable)" instead of crash
    Failure Indicators: Exception raised, or missing "(unavailable)"
    Evidence: .sisyphus/evidence/task-4-unavailable.txt
  ```

  **Commit**: YES (groups with Task 3)
  - Message: `feat(prompt): add role loading and prompt assembly`
  - Files: `lib/roundtable/prompt/assembler.ex`, `test/roundtable/prompt/assembler_test.exs`
  - Pre-commit: `mix test`

- [x] 5. CLI.Gemini — Arg Builder + JSON Parser

  **What to do**:
  - RED: Write `test/roundtable/cli/gemini_test.exs` using fixture files with tests for:
    - Parse successful Gemini JSON: extract response, model_used from stats, tokens, session_id
    - Parse Gemini error JSON: extract error.message, status = :error
    - Parse malformed stdout with valid stderr JSON: fall back to stderr error extraction
    - Parse completely malformed output: status = :error, parse_error set, raw stdout as response
    - `build_args/2` with model override → includes `-m MODEL`
    - `build_args/2` without model → no `-m` flag
    - `build_args/2` with resume → `--resume SESSION` before `-p`
    - `probe_args/0` returns `["--version"]`
  - GREEN: Create `lib/roundtable/cli/gemini.ex` implementing `Roundtable.CLI.Behaviour`:
    - `probe_args/0` → `["--version"]`
    - `build_args/2` → `["-p", prompt, "-o", "json", "--yolo"] ++ model_args ++ resume_args`
      - If `args.gemini_model`, append `["-m", model]`
      - If `args.gemini_resume`, prepend `["--resume", session_id]` before `-p`
    - `parse_output/2` → Parse stdout as JSON:
      1. `Jason.decode(stdout)` → extract `data["response"]`
      2. Capture `data["stats"]["models"]` → first key = model name, extract tokens
      3. If `data["error"]` exists → status :error, response = error message
      4. Capture `data["session_id"]`
      5. On JSON parse failure: try `Jason.decode(stderr)` for error block
      6. On all failures: response = `stdout || stderr`, status = :error, parse_error = message
  - Return shape must match Behaviour's `parse_result` type

  **Must NOT do**:
  - Handle streaming — Gemini outputs single JSON blob
  - Add retry or rate-limit handling
  - Import or use anything beyond Jason

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []
    - Parser logic has multiple code paths and error recovery — needs careful implementation

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Tasks 2, 3, 4, 6, 7)
  - **Blocks**: Task 12
  - **Blocked By**: Tasks 1, 2

  **References**:

  **Pattern References**:
  - `roundtable.mjs:189-237` — parseGeminiOutput: EXACT parsing logic — JSON.parse(stdout), data.response, data.stats.models, data.error, stderr fallback. Port this 1:1.
  - `roundtable.mjs:531-538` — buildGeminiArgs: exact flag ordering — base flags, model flag, resume flag placement

  **Test References**:
  - `test/fixtures/gemini_success.json` — created in Task 1, contains response + stats + session_id
  - `test/fixtures/gemini_error.json` — contains error.message
  - `test/fixtures/gemini_stderr_error.json` — error block in stderr format

  **WHY Each Reference Matters**:
  - parseGeminiOutput is the most complex parser with 4 code paths — must match every branch
  - buildGeminiArgs flag ordering matters because some flags are positional (--resume before -p)

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/cli/gemini_test.exs`
  - [ ] `mix test test/roundtable/cli/gemini_test.exs` → PASS (8+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Parse successful Gemini output
    Tool: Bash
    Preconditions: Fixture file test/fixtures/gemini_success.json exists
    Steps:
      1. Run `mix test test/roundtable/cli/gemini_test.exs --trace`
      2. Assert test "parses successful response" verifies:
         - response == "test response"
         - status == :ok
         - metadata.model_used == "gemini-2.5-pro"
         - session_id == "ses_abc123"
    Expected Result: All fields correctly extracted
    Failure Indicators: Wrong field values, missing metadata
    Evidence: .sisyphus/evidence/task-5-gemini-success.txt

  Scenario: Stderr fallback on malformed stdout
    Tool: Bash
    Preconditions: Fixture files exist
    Steps:
      1. Run test that passes malformed stdout + valid stderr error JSON
      2. Assert status == :error
      3. Assert response contains error message from stderr
      4. Assert parse_error is nil (stderr parse succeeded)
    Expected Result: Error recovered from stderr
    Failure Indicators: parse_error set when stderr was valid JSON
    Evidence: .sisyphus/evidence/task-5-gemini-stderr.txt
  ```

  **Commit**: YES (groups with Task 6)
  - Message: `feat(cli): add Gemini JSON and Codex JSONL parsers`
  - Files: `lib/roundtable/cli/gemini.ex`, `test/roundtable/cli/gemini_test.exs`
  - Pre-commit: `mix test`

- [x] 6. CLI.Codex — Arg Builder + JSONL Parser

  **What to do**:
  - RED: Write `test/roundtable/cli/codex_test.exs` using fixture files with tests for:
    - Parse JSONL with agent_message events → joined with "\n\n", status :ok
    - Parse JSONL with thread.started → extract thread_id as session_id
    - Parse JSONL with turn.completed → extract usage metadata
    - Parse JSONL with error events → status :error, joined error messages
    - Parse JSONL with mixed events (messages + errors) → messages win (status :ok)
    - Parse output with no valid JSONL events but raw text → status :error, parse_error set, raw text as response
    - Parse empty output → status :error, parse_error "No output from codex"
    - Skip non-JSON lines (preamble text) gracefully
    - `build_args/2` with model → `-c model=MODEL`
    - `build_args/2` with reasoning → `-c reasoning_effort=EFFORT`
    - `build_args/2` with resume "last" → `resume --last PROMPT`
    - `build_args/2` with resume session ID → `resume SESSION PROMPT`
    - `probe_args/0` returns `["--version"]`
  - GREEN: Create `lib/roundtable/cli/codex.ex` implementing `Roundtable.CLI.Behaviour`:
    - `probe_args/0` → `["--version"]`
    - `build_args/2` → `["exec", "--json", "--dangerously-bypass-approvals-and-sandbox"] ++ model_args ++ reasoning_args ++ resume_or_prompt`
    - `parse_output/2`:
      1. Split stdout by `"\n"`, filter empty/blank lines
      2. For each line starting with `{`, attempt `Jason.decode/1`
      3. Collect by event type: `item.completed` (agent_message text), `thread.started` (thread_id), `turn.completed` (usage), `error` (message)
      4. If messages found: join with `"\n\n"`, status :ok
      5. Else if errors: join, status :error
      6. Else if raw text: status :error, parse_error "No JSONL events found; using raw output"
      7. Else: status :error, parse_error "No output from codex"

  **Must NOT do**:
  - Stream JSONL line-by-line from a Port (this is a pure function — receives complete stdout string)
  - Handle partial JSON objects split across lines
  - Add event types beyond what Node.js handles

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Tasks 2, 3, 4, 5, 7)
  - **Blocks**: Task 12
  - **Blocked By**: Tasks 1, 2

  **References**:

  **Pattern References**:
  - `roundtable.mjs:241-312` — parseCodexOutput: EXACT parsing logic — line-by-line JSONL, event type switching (item.completed, thread.started, turn.completed, error), message joining, fallback chain. Port this 1:1.
  - `roundtable.mjs:540-548` — buildCodexArgs: exact flag ordering and resume logic (`resume --last` vs `resume SESSION PROMPT`)

  **Test References**:
  - `test/fixtures/codex_success.jsonl` — created in Task 1
  - `test/fixtures/codex_errors.jsonl` — error events
  - `test/fixtures/codex_empty.jsonl` — blank lines

  **WHY Each Reference Matters**:
  - parseCodexOutput has 5 distinct return paths — each must be tested
  - buildCodexArgs resume logic has 3 variants (no resume, resume --last, resume SESSION) — all from Node.js lines 540-548

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/cli/codex_test.exs`
  - [ ] `mix test test/roundtable/cli/codex_test.exs` → PASS (13+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Parse JSONL with mixed event types
    Tool: Bash
    Preconditions: codex_success.jsonl fixture has thread.started + item.completed + turn.completed
    Steps:
      1. Run `mix test test/roundtable/cli/codex_test.exs --trace`
      2. Assert "parses successful JSONL" test verifies:
         - response contains agent_message text(s) joined by "\n\n"
         - session_id matches thread_id from thread.started event
         - metadata.usage present from turn.completed
         - status == :ok
    Expected Result: All event types correctly extracted
    Failure Indicators: Missing session_id, wrong message joining
    Evidence: .sisyphus/evidence/task-6-codex-success.txt

  Scenario: Empty output returns descriptive error
    Tool: Bash
    Preconditions: Empty fixture
    Steps:
      1. Run test that passes empty string as stdout
      2. Assert response == ""
      3. Assert status == :error
      4. Assert parse_error == "No output from codex"
    Expected Result: Graceful empty handling with descriptive parse_error
    Failure Indicators: Crash on empty input
    Evidence: .sisyphus/evidence/task-6-codex-empty.txt
  ```

  **Commit**: YES (groups with Task 5)
  - Message: `feat(cli): add Gemini JSON and Codex JSONL parsers`
  - Files: `lib/roundtable/cli/codex.ex`, `test/roundtable/cli/codex_test.exs`
  - Pre-commit: `mix test`

- [x] 7. Output — Result Builder + JSON Encoding

  **What to do**:
  - RED: Write `test/roundtable/output_test.exs` with tests for:
    - Build result for CLI not found → status "not_found", stderr message
    - Build result for probe failure → status "probe_failed", diagnostic stderr
    - Build result for successful execution → parse output, status from parser
    - Build result for timeout → status "timeout" overrides parser status
    - Build result for signal termination → status "terminated"
    - Build result for non-zero exit with parser "ok" → downgrade to "error"
    - Build meta object → total_elapsed_ms = max of both, role names, files list
    - Encode final output as pretty JSON (2-space indent) — match `JSON.stringify(results, null, 2)`
    - All status values are strings in JSON output (not atoms)
  - GREEN: Create `lib/roundtable/output.ex` with:
    - `build_result/5` — mirrors Node.js `buildResult(cliName, path, model, args, settledResult)`
      - If no path → `%{status: "not_found", stderr: "#{cli_name} CLI not found in PATH", ...}`
      - If probe failed → `%{status: "probe_failed", stderr: "#{cli_name} CLI probe failed: #{reason}...", ...}`
      - Otherwise: call the CLI module's `parse_output/2`, then apply status overrides:
        - `timed_out: true` → status = "timeout"
        - `exit_signal` present → status = "terminated"
        - `exit_code != 0 && parser_status == "ok"` → status = "error"
    - `build_meta/4` — `%{total_elapsed_ms: max(...), gemini_role: ..., codex_role: ..., files_referenced: ...}`
    - `encode/1` — `Jason.encode!(results, pretty: true)` with 2-space indent
    - All map keys in output must be strings (not atoms) to match JSON field names

  **Must NOT do**:
  - Add key ordering logic (structural parity, not byte-for-byte)
  - Add any output beyond the single JSON blob
  - Add Logger calls

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with Tasks 2, 3, 4, 5, 6)
  - **Blocks**: Task 11
  - **Blocked By**: Task 1

  **References**:

  **Pattern References**:
  - `roundtable.mjs:403-451` — buildResult: EXACT status override logic (timeout → terminated → exit-code downgrade). The ORDER of checks matters. Port this logic precisely.
  - `roundtable.mjs:507-513` — probeFailResult: exact stderr message format including "Run gemini --version to diagnose"
  - `roundtable.mjs:550-559` — buildMeta: total_elapsed_ms uses Math.max, field names are gemini_role/codex_role (not just "role")
  - `roundtable.mjs:526` — `JSON.stringify(results, null, 2)` — 2-space pretty print
  - `DESIGN.md:92-122` — Output contract JSON shape (but source code is authoritative, not DESIGN.md)

  **WHY Each Reference Matters**:
  - Status override order is the #1 correctness concern — timeout must be checked before signal, signal before exit code
  - buildMeta field names differ from DESIGN.md example (gemini_role/codex_role vs role) — source code is truth
  - Pretty-print format must match Node.js for output parity tests

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/output_test.exs`
  - [ ] `mix test test/roundtable/output_test.exs` → PASS (9+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Status override priority order
    Tool: Bash
    Preconditions: Tests cover all 6 status values
    Steps:
      1. Run `mix test test/roundtable/output_test.exs --trace`
      2. Assert "timeout overrides parser ok" test: timed_out=true + parser status :ok → "timeout"
      3. Assert "signal overrides parser ok" test: exit_signal="SIGTERM" → "terminated"
      4. Assert "non-zero exit downgrades ok" test: exit_code=1 + parser :ok → "error"
    Expected Result: Status priority: timeout > terminated > exit-code-downgrade > parser-status
    Failure Indicators: Wrong status for combined conditions
    Evidence: .sisyphus/evidence/task-7-status-override.txt

  Scenario: JSON output uses string keys, not atoms
    Tool: Bash
    Preconditions: Output module encode function exists
    Steps:
      1. Run test that encodes a result and checks for "response" not ":response" in output
      2. Assert `Jason.decode!(encoded)` produces map with string keys
    Expected Result: All keys are strings in JSON
    Failure Indicators: Atom keys in encoded JSON
    Evidence: .sisyphus/evidence/task-7-string-keys.txt
  ```

  **Commit**: YES
  - Message: `feat(output): add result builder and JSON encoding`
  - Files: `lib/roundtable/output.ex`, `test/roundtable/output_test.exs`
  - Pre-commit: `mix test`

- [x] 8. CLI.Runner — Port-Based CLI Execution with Shell Wrapper

  **What to do**:
  - RED: Write `test/roundtable/cli/runner_test.exs` with tests using fake CLI scripts:
    - Create `test/support/fake_cli_success.sh` — echoes known JSON to stdout, known text to stderr, exits 0
    - Create `test/support/fake_cli_timeout.sh` — sleeps forever (for timeout testing)
    - Create `test/support/fake_cli_error.sh` — exits non-zero with stderr output
    - Create `test/support/fake_cli_large.sh` — outputs >1MB to stdout
    - Tests:
      - Run CLI successfully → captures stdout, stderr (from temp file), exit_code 0, elapsed_ms > 0
      - Run CLI that times out → kills process group, timed_out: true, returns partial stdout
      - Run CLI that exits non-zero → captures output, exit_code set
      - Run CLI with >1MB stdout → truncated: true, output capped at 1MB
      - Probe a CLI (health check) → returns alive/not alive based on exit code
      - Run CLI sets ROUNDTABLE_ACTIVE=1 in child environment
      - Verify no orphan processes after timeout kill
  - GREEN: Create `lib/roundtable/cli/runner.ex` with:
    - **Shell wrapper pattern** (solves both stderr separation AND process group creation):
      ```
      /bin/sh -c 'exec 2>#{stderr_path}; exec setsid #{command} #{args_joined}'
      ```
      - `stderr_path` = `System.tmp_dir!/0 <> "/rt_stderr_#{System.unique_integer([:positive])}"
      - `setsid` creates new process group → PID = PGID
    - `run_cli/3` → `(command :: String.t(), cli_args :: [String.t()], timeout_ms :: non_neg_integer()) :: map()`
      - Open Port via `Port.open({:spawn_executable, "/bin/sh"}, [:binary, :exit_status, args: ["-c", wrapper_cmd]])`
      - Accumulate stdout chunks in receive loop, enforce 1MB cap (1_048_576 bytes)
      - On timeout: kill process group via `:os.cmd(~c"kill -TERM -#{os_pid}")`, wait 3s, `:os.cmd(~c"kill -KILL -#{os_pid}")`
      - After port closes: read stderr from temp file, delete temp file in `after` block
      - Return: `%{stdout: ..., stderr: ..., exit_code: ..., exit_signal: ..., elapsed_ms: ..., timed_out: ..., truncated: ...}`
    - `probe_cli/3` → `(executable :: String.t(), test_args :: [String.t()], probe_timeout_ms :: non_neg_integer()) :: map()`
      - Simpler version: `System.cmd(executable, test_args, ...)` with `:timeout` handling (spawn Task, yield with timeout, kill on timeout)
      - Sets `ROUNDTABLE_ACTIVE=1` in env
      - Return: `%{alive: boolean(), exit_code: ..., stdout: ..., reason: ...}`
    - `find_executable/1` → `System.find_executable(name)` — thin wrapper for testability

  **Must NOT do**:
  - Use erlexec or any external library for process management
  - Add retry logic
  - Stream output incrementally (buffer everything, return at end)
  - Leave temp stderr files behind on any code path (always cleanup in `after`)

  **Recommended Agent Profile**:
  - **Category**: `deep`
  - **Skills**: []
    - Most complex module: Port management, process group kill, timeout escalation, temp file management, OS-level concerns

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with Tasks 9, 10)
  - **Blocks**: Task 10
  - **Blocked By**: Tasks 1, 2

  **References**:

  **Pattern References**:
  - `roundtable.mjs:319-399` — runCli: EXACT behavior to replicate — spawn with detached process group, stdout/stderr buffering, 1MB cap, timeout with SIGTERM→SIGKILL escalation, elapsed_ms tracking. The shell wrapper replaces the `detached: true` + `process.kill(-pid)` pattern.
  - `roundtable.mjs:99-135` — probeCli: health probe with timeout, SIGTERM→SIGKILL fallback on probe timeout, return {alive, exit_code, stdout, reason}
  - `roundtable.mjs:315` — `activeChildren` Set — in Elixir, Task.Supervisor tracks this automatically
  - `roundtable.mjs:329-331` — `ROUNDTABLE_ACTIVE: '1'` in spawn env — must be replicated in Elixir child env

  **External References**:
  - Elixir Port.open docs: `{:spawn_executable, path}` with `:exit_status` option for exit code capture
  - `:os.cmd/1` for `kill -TERM -PGID` — note: takes charlist, not string

  **WHY Each Reference Matters**:
  - runCli is the highest-risk function — incorrect port management = orphan processes, zombie CLIs, or hung escript
  - The shell wrapper pattern is the KEY architectural decision — it replaces Node.js's `detached: true` + `process.kill(-pid)` with a single clean approach

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/cli/runner_test.exs`
  - [ ] `mix test test/roundtable/cli/runner_test.exs` → PASS (7+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: CLI execution captures stdout and stderr separately
    Tool: Bash
    Preconditions: test/support/fake_cli_success.sh exists, is executable
    Steps:
      1. Run `mix test test/roundtable/cli/runner_test.exs --trace`
      2. Assert "captures stdout and stderr" test verifies:
         - stdout contains expected output from fake CLI
         - stderr contains expected error text from fake CLI
         - exit_code == 0
         - elapsed_ms > 0
    Expected Result: Separate stdout and stderr capture via shell wrapper
    Failure Indicators: stderr empty, or stderr mixed into stdout
    Evidence: .sisyphus/evidence/task-8-runner-capture.txt

  Scenario: Timeout kills process group and returns partial output
    Tool: Bash
    Preconditions: test/support/fake_cli_timeout.sh (sleeps forever)
    Steps:
      1. Run test with 2-second timeout against sleep script
      2. Assert timed_out == true
      3. Assert elapsed_ms approximately 2000 (±500ms)
      4. Run `pgrep -f fake_cli_timeout` after test
      5. Assert no matching processes (clean kill)
    Expected Result: Process killed, no orphans, timed_out flag set
    Failure Indicators: Orphan process found, or test hangs
    Evidence: .sisyphus/evidence/task-8-runner-timeout.txt

  Scenario: Output >1MB is truncated
    Tool: Bash
    Preconditions: test/support/fake_cli_large.sh outputs 2MB
    Steps:
      1. Run test against large-output script
      2. Assert truncated == true
      3. Assert byte_size(stdout) <= 1_048_576
    Expected Result: Output capped, truncated flag set
    Failure Indicators: Full 2MB in output, or OOM
    Evidence: .sisyphus/evidence/task-8-runner-truncate.txt

  Scenario: No temp files left behind
    Tool: Bash
    Preconditions: Run any CLI execution test
    Steps:
      1. List files matching /tmp/rt_stderr_* before test
      2. Run test
      3. List files matching /tmp/rt_stderr_* after test
      4. Assert no new files remain
    Expected Result: Temp stderr files cleaned up
    Failure Indicators: Orphan temp files
    Evidence: .sisyphus/evidence/task-8-runner-cleanup.txt
  ```

  **Commit**: YES
  - Message: `feat(runner): add Port-based CLI runner with shell wrapper`
  - Files: `lib/roundtable/cli/runner.ex`, `test/roundtable/cli/runner_test.exs`, `test/support/fake_cli_*.sh`
  - Pre-commit: `mix test`

- [x] 9. Argument Parsing Module

  **What to do**:
  - RED: Write `test/roundtable/args_test.exs` with tests for:
    - Parse all flags: --prompt, --role, --gemini-role, --codex-role, --files, --gemini-model, --codex-model, --codex-reasoning, --timeout, --roles-dir, --project-roles-dir, --gemini-resume, --codex-resume
    - Default values: role="default", timeout=900, files=[], models=nil, etc.
    - Positional argument used as prompt when --prompt not given
    - --files splits on comma, trims whitespace, filters empty strings
    - Missing --prompt (and no positional) → error tuple
    - Invalid --timeout (non-integer, zero, negative) → error tuple
    - --timeout string like "abc" → error tuple
    - Flag requiring value but none given → error tuple
  - GREEN: Create `lib/roundtable/args.ex` with:
    - `parse/1` → `(argv :: [String.t()]) :: {:ok, map()} | {:error, String.t()}`
    - Use `OptionParser.parse/2` with `strict:` option for all flags
    - Handle the `{parsed, rest, invalid}` tuple:
      - Check `rest` for bare prompt (first non-flag arg)
      - Check `invalid` for errors
    - Post-validation: prompt required, timeout must be positive integer
    - --files handling: `String.split(value, ",") |> Enum.map(&String.trim/1) |> Enum.reject(&(&1 == ""))`
    - Default roles_dir: `:filename.dirname(:escript.script_name()) |> Path.join("roles")`
    - Return map with atom keys: `%{prompt: ..., role: ..., gemini_role: ..., ...}`
    - On error: `{:error, "Missing required --prompt argument"}` or `{:error, "--timeout must be a positive integer"}`

  **Must NOT do**:
  - Use optimus or any external arg parsing library
  - Add --help flag (not in Node.js version)
  - Add --verbose or --debug flags
  - Validate role names (role validation happens when loading files)

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with Tasks 8, 10)
  - **Blocks**: Task 11
  - **Blocked By**: Task 1

  **References**:

  **Pattern References**:
  - `roundtable.mjs:24-81` — parseArgs: EXACT flag names, default values, validation rules. The switch/case block defines every flag. Port defaults precisely (especially timeout=900, not 120).
  - `roundtable.mjs:66-68` — Positional arg fallback: `if (!arg.startsWith('--') && !args.prompt) args.prompt = arg`
  - `roundtable.mjs:579-584` — Error output format: `{error: msg, usage: "roundtable --prompt ..."}`

  **WHY Each Reference Matters**:
  - Flag names must match exactly (--codex-reasoning, not --reasoning)
  - Timeout default 900 from source, not 120 from DESIGN.md
  - Positional arg handling is a subtle feature users might depend on

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/args_test.exs`
  - [ ] `mix test test/roundtable/args_test.exs` → PASS (8+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: All defaults correct
    Tool: Bash
    Preconditions: Args module exists
    Steps:
      1. Run test that parses `["--prompt", "hello"]`
      2. Assert role == "default", timeout == 900, files == [], all models nil
    Expected Result: Defaults match Node.js exactly
    Failure Indicators: Wrong default timeout (120 vs 900)
    Evidence: .sisyphus/evidence/task-9-defaults.txt

  Scenario: Positional arg as prompt
    Tool: Bash
    Preconditions: Args module exists
    Steps:
      1. Run test that parses `["hello world"]` (no --prompt flag)
      2. Assert prompt == "hello world"
    Expected Result: Bare argument used as prompt
    Failure Indicators: Error about missing --prompt
    Evidence: .sisyphus/evidence/task-9-positional.txt

  Scenario: Invalid timeout rejected
    Tool: Bash
    Preconditions: Args module exists
    Steps:
      1. Run test that parses `["--prompt", "test", "--timeout", "abc"]`
      2. Assert {:error, _} returned
      3. Assert error message mentions "positive integer"
    Expected Result: Clear error message
    Failure Indicators: Crash or silent default
    Evidence: .sisyphus/evidence/task-9-invalid-timeout.txt
  ```

  **Commit**: YES
  - Message: `feat(args): add argument parsing with OptionParser`
  - Files: `lib/roundtable/args.ex`, `test/roundtable/args_test.exs`
  - Pre-commit: `mix test`

- [x] 10. Dispatcher — Parallel CLI Dispatch via Task.Supervisor

  **What to do**:
  - RED: Write `test/roundtable/dispatcher_test.exs` with tests using mock CLI modules:
    - Define `test/support/mock_cli.ex` implementing `Roundtable.CLI.Behaviour` with canned responses
    - Tests:
      - Dispatch to both CLIs in parallel → both results returned
      - One CLI not found → that result has status "not_found", other runs normally
      - One CLI probe fails → that result has status "probe_failed", other runs normally
      - One CLI times out → that result has status "timeout", other completes normally
      - Both CLIs not found → both results have "not_found"
      - Verify parallel execution (elapsed time ≈ max of both, not sum)
  - GREEN: Create `lib/roundtable/dispatcher.ex` with:
    - `dispatch/1` → `(config :: map()) :: map()`
    - Config includes: prompt, timeout_ms, cli_configs (list of %{module, name, path, model, args, prompt})
    - **CRITICAL: Runner owns ALL timeout/cleanup. Dispatcher does NOT add competing timeouts.**
    - Implementation:
      1. Start `Task.Supervisor` via `Task.Supervisor.start_link/1`
      2. For each CLI: check path (`Runner.find_executable`), if found probe (`Runner.probe_cli`)
      3. **Probes run in parallel** via `Task.async` + `Task.yield_many(probes, timeout: 6000)` (slightly above 5s probe timeout to allow Runner to finish cleanly)
      4. For healthy CLIs: dispatch main execution via `Task.Supervisor.async(sup, fn -> Runner.run_cli(cmd, args, timeout_ms) end)` — **Runner.run_cli handles its own timeout internally** (SIGTERM→3s→SIGKILL) and always returns a settled result map
      5. Collect results via `Task.await(task, timeout_ms + 10_000)` — generous outer timeout is a **safety net only**, not the primary timeout mechanism. Runner's internal timeout fires first and returns a `%{timed_out: true, ...}` result. The outer await timeout should never fire under normal operation.
      6. **DO NOT call Task.shutdown or Task.yield_many with a competing timeout.** Runner always returns a result (success, error, or timeout) — Dispatcher just awaits it.
      7. Build results using `Output.build_result/5` for each CLI
      8. Build meta using `Output.build_meta/4`
      9. Shut down Task.Supervisor
      10. Return `%{"gemini" => ..., "codex" => ..., "meta" => ...}`

  **Must NOT do**:
  - Use GenServer for orchestration — pure function with internal Task.Supervisor
  - **Add a competing timeout in Dispatcher** — Runner is the sole timeout/cleanup owner. Dispatcher's outer timeout is a safety net only, set well above Runner's timeout.
  - Call `Task.shutdown/2` or `Task.yield_many/2` with a timeout that races Runner's internal timeout
  - Add retry logic
  - Add streaming/progress callbacks
  - Hardcode CLI names — accept list of CLI configs (for Behaviour extensibility)

  **Recommended Agent Profile**:
  - **Category**: `deep`
  - **Skills**: []
    - Orchestration logic with parallel tasks, timeout handling, and proper cleanup — requires careful implementation

  **Parallelization**:
  - **Can Run In Parallel**: YES (partially — depends on Task 8)
  - **Parallel Group**: Wave 3 (with Tasks 8, 9)
  - **Blocks**: Task 11
  - **Blocked By**: Tasks 2, 8

  **References**:

  **Pattern References**:
  - `roundtable.mjs:455-527` — main() function: the orchestration flow — parse args → load roles → probe → dispatch → build results → output. The Dispatcher implements the probe → dispatch → build portion.
  - `roundtable.mjs:476-489` — Parallel probe execution: `probes.gemini = probeCli(...)` then `await probes.gemini`. Both probes run concurrently.
  - `roundtable.mjs:496-504` — Parallel CLI execution with Promise.allSettled: `const [geminiSettled, codexSettled] = await Promise.allSettled([geminiTask, codexTask])`. In Elixir: Runner.run_cli always returns a result map (never throws), so Task.async + Task.await achieves the same semantics.
  - `roundtable.mjs:515-526` — Result assembly with probe failure handling

  **WHY Each Reference Matters**:
  - main() is the orchestration source of truth — Dispatcher must replicate its control flow
  - Probe parallelism is explicitly noted by Metis — sequential probes add 10s worst case
  - Promise.allSettled semantics (independent failure): In Node.js, allSettled never rejects. In Elixir, Runner.run_cli never raises — it always returns a settled result map. Dispatcher just awaits both tasks.
  - **Timeout ownership is Runner-only**: Node.js has one timeout layer (in runCli). The Elixir port must also have one layer (in Runner.run_cli). Dispatcher does NOT add a competing timeout.

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable/dispatcher_test.exs`
  - [ ] `mix test test/roundtable/dispatcher_test.exs` → PASS (6+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Parallel execution verified by timing
    Tool: Bash
    Preconditions: Mock CLIs with 1-second delay each
    Steps:
      1. Run test that dispatches to two mock CLIs, each taking ~1s
      2. Assert total elapsed_ms < 2500ms (parallel, not sequential 2s+)
    Expected Result: Both CLIs ran concurrently
    Failure Indicators: elapsed_ms > 2500ms (sequential execution)
    Evidence: .sisyphus/evidence/task-10-parallel.txt

  Scenario: One CLI fails, other succeeds
    Tool: Bash
    Preconditions: Mock CLI A returns error, Mock CLI B returns success
    Steps:
      1. Run dispatcher with both mocks
      2. Assert CLI A result has status "error"
      3. Assert CLI B result has status "ok"
      4. Both results present in output
    Expected Result: Independent failure — one doesn't kill the other
    Failure Indicators: Both fail, or missing result
    Evidence: .sisyphus/evidence/task-10-partial-failure.txt
  ```

  **Commit**: YES
  - Message: `feat(dispatcher): add parallel CLI dispatch via Task.Supervisor`
  - Files: `lib/roundtable/dispatcher.ex`, `test/roundtable/dispatcher_test.exs`, `test/support/mock_cli.ex`
  - Pre-commit: `mix test`

- [x] 11. Main Entry Point + Escript Build

  **What to do**:
  - RED: Write `test/roundtable_test.exs` (top-level module test) with tests for:
    - Recursion guard: when ROUNDTABLE_ACTIVE env is set, outputs error JSON and returns exit code 1
    - Argument parse error: outputs `{"error": "...", "usage": "..."}` JSON
    - Role loading error: outputs `{"error": "Role prompt not found: ..."}` JSON
    - Full pipeline integration (with mock dispatcher): args → roles → assemble → dispatch → output
  - GREEN: Update `lib/roundtable.ex` to implement the full `main/1` pipeline:
    ```elixir
    def main(args) do
      # 1. Recursion guard
      if System.get_env("ROUNDTABLE_ACTIVE") do
        IO.puts(Jason.encode!(%{"error" => "Recursive invocation detected. Roundtable is already running in a parent process."}))
        System.halt(1)
      end
      
      # 2. Parse arguments
      case Args.parse(args) do
        {:error, msg} ->
          IO.puts(Jason.encode!(%{"error" => msg, "usage" => "roundtable --prompt \"...\" [--role default|planner|codereviewer] [--files a.ts,b.ts]"}))
          System.halt(1)
        {:ok, parsed_args} ->
          run(parsed_args)
      end
    end
    
    defp run(args) do
      # 3. Resolve roles
      gemini_role = args.gemini_role || args.role
      codex_role = args.codex_role || args.role
      
      # 4. Load role prompts (raises on failure)
      gemini_role_prompt = Roles.load_role_prompt(gemini_role, args.roles_dir, args.project_roles_dir)
      codex_role_prompt = Roles.load_role_prompt(codex_role, args.roles_dir, args.project_roles_dir)
      
      # 5. Assemble prompts
      file_refs = Assembler.format_file_references(args.files)
      gemini_prompt = Assembler.assemble(gemini_role_prompt, args.prompt, args.files)
      codex_prompt = Assembler.assemble(codex_role_prompt, args.prompt, args.files)
      
      # 6. Dispatch
      results = Dispatcher.dispatch(%{...config...})
      
      # 7. Output
      IO.puts(Output.encode(results))
      System.halt(0)
    end
    ```
  - Wrap role loading in try/rescue to convert exceptions to JSON error output
  - Update `mix.exs` escript config: `escript: [main_module: Roundtable]`
  - Verify escript build works: `mix escript.build` produces `./roundtable`

  **Must NOT do**:
  - Add Application module or application callback
  - Add --help handling
  - Add verbose/debug mode
  - Catch all exceptions broadly — only rescue known error types

  **Recommended Agent Profile**:
  - **Category**: `deep`
  - **Skills**: []
    - Wires everything together — requires understanding all modules and their contracts

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 4 (sequential — depends on all Wave 2+3 tasks)
  - **Blocks**: Tasks 12, 13
  - **Blocked By**: Tasks 3, 4, 7, 9, 10

  **References**:

  **Pattern References**:
  - `roundtable.mjs:455-527` — main() function: COMPLETE orchestration flow. This is the exact sequence to replicate.
  - `roundtable.mjs:10-18` — Recursion guard: check env var → output JSON error → exit 1
  - `roundtable.mjs:456-460` — Role resolution: `geminiRole = args.geminiRole || args.role`
  - `roundtable.mjs:579-584` — Error handling: ArgError vs generic, output format differs (ArgError includes usage field)

  **WHY Each Reference Matters**:
  - main() is the top-level orchestration — everything must be wired in the exact order
  - Recursion guard must be the FIRST check (before arg parsing)
  - Error output format must match: ArgError → `{error, usage}`, other errors → `{error}` only

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/roundtable_test.exs`
  - [ ] `mix test test/roundtable_test.exs` → PASS (4+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Escript builds and runs
    Tool: Bash
    Preconditions: All modules implemented
    Steps:
      1. Run `mix escript.build`
      2. Assert `./roundtable` file exists and is executable
      3. Run `./roundtable --prompt "test" --timeout 5` (may fail if CLIs not installed, that's ok)
      4. Assert stdout is valid JSON (`./roundtable --prompt "test" --timeout 5 | jq .`)
    Expected Result: Valid JSON output regardless of CLI availability
    Failure Indicators: Non-JSON output, crash, or hang
    Evidence: .sisyphus/evidence/task-11-escript-run.txt

  Scenario: Recursion guard works
    Tool: Bash
    Preconditions: Escript built
    Steps:
      1. Run `ROUNDTABLE_ACTIVE=1 ./roundtable --prompt "test" 2>/dev/null; echo "EXIT:$?"`
      2. Capture stdout
      3. Assert stdout contains "Recursive invocation detected"
      4. Assert exit code is 1
    Expected Result: Error JSON output, exit 1
    Failure Indicators: Exit 0, or proceeds to dispatch
    Evidence: .sisyphus/evidence/task-11-recursion-guard.txt

  Scenario: Missing prompt error
    Tool: Bash
    Preconditions: Escript built
    Steps:
      1. Run `./roundtable 2>/dev/null; echo "EXIT:$?"`
      2. Assert stdout is JSON with "error" and "usage" keys
      3. Assert exit code is 1
    Expected Result: `{"error":"Missing required --prompt argument","usage":"roundtable --prompt ..."}`
    Failure Indicators: Crash without JSON, or exit 0
    Evidence: .sisyphus/evidence/task-11-missing-prompt.txt
  ```

  **Commit**: YES
  - Message: `feat(main): wire up entry point and escript build`
  - Files: `lib/roundtable.ex`, `mix.exs`, `test/roundtable_test.exs`
  - Pre-commit: `mix escript.build && mix test`

- [x] 12. Integration Tests with Mock CLI Scripts

  **What to do**:
  - Create `test/integration_test.exs` with end-to-end tests that exercise the full escript:
    - Create `test/support/bin/gemini` — executable shell script (no `.sh` extension) that accepts `-p PROMPT -o json --yolo`, outputs fixture JSON to stdout. Named `gemini` so `System.find_executable("gemini")` finds it when `test/support/bin` is prepended to PATH.
    - Create `test/support/bin/codex` — executable shell script (no `.sh` extension) that accepts `exec --json ...`, outputs fixture JSONL to stdout. Named `codex` for same reason.
    - Create `test/support/bin/gemini_timeout` — sleeps forever (symlinked or copied as `gemini` for timeout tests)
    - Tests (all build escript, run it, parse JSON output):
      - Full success: both mock CLIs produce valid output → JSON has gemini.status="ok" + codex.status="ok"
      - One CLI missing: use PATH without mock dir → status="not_found" for that CLI
      - Timeout: use timeout mock as `gemini` → status="timeout"
      - Different roles: --role planner → meta.gemini_role="planner"
      - Per-CLI roles: --gemini-role planner --codex-role codereviewer → different roles in meta
      - File references: --files test/test_helper.exs → files_referenced includes path
      - Resume flags: --gemini-resume latest → passed through to mock CLI args
      - Recursion guard: ROUNDTABLE_ACTIVE=1 → error JSON, exit 1
      - Argument errors: no --prompt → error JSON with usage, exit 1
  - Tests use a modified PATH that PREPENDS `test/support/bin/` so mock scripts named `gemini` and `codex` are found by `System.find_executable/1` instead of real CLIs
  - Each test builds the escript if not already built, runs it as an OS process, and parses the JSON output

  **Must NOT do**:
  - Test against real Gemini/Codex CLIs (those are external, non-deterministic)
  - Name mock scripts `mock_gemini.sh` or `mock_codex.sh` — they MUST be named `gemini` and `codex` (no extension) so `System.find_executable/1` discovers them via PATH
  - Mock at the Elixir module level (integration tests exercise the FULL binary)
  - Skip error scenarios (these are the most important integration tests)

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 4 (with Task 13)
  - **Blocks**: F1-F4
  - **Blocked By**: Task 11

  **References**:

  **Pattern References**:
  - `roundtable.mjs:531-548` — buildGeminiArgs/buildCodexArgs: exact flags the mock scripts must accept
  - `SKILL.md:78-86` — Output format that integration tests must verify
  - `roundtable.mjs:403-451` — All status values and their trigger conditions

  **WHY Each Reference Matters**:
  - Mock scripts must accept the exact same args the real CLIs receive — derived from buildGeminiArgs/buildCodexArgs
  - Integration tests verify the ENTIRE pipeline end-to-end — the final acceptance gate

  **Acceptance Criteria**:

  **If TDD:**
  - [ ] Test file created: `test/integration_test.exs`
  - [ ] `mix test test/integration_test.exs` → PASS (9+ tests)

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Full success end-to-end
    Tool: Bash
    Preconditions: Escript built, mock CLIs in test/support/bin/
    Steps:
      1. Run `PATH=test/support/bin:$PATH ./roundtable --prompt "test question" --timeout 30`
      2. Parse output with jq
      3. Assert `.gemini.status` == "ok"
      4. Assert `.codex.status` == "ok"
      5. Assert `.gemini.response` is non-empty
      6. Assert `.codex.response` is non-empty
      7. Assert `.meta.gemini_role` == "default"
    Expected Result: Both CLIs succeed, valid JSON with all fields
    Failure Indicators: Missing fields, wrong status, non-JSON output
    Evidence: .sisyphus/evidence/task-12-e2e-success.txt

  Scenario: Output parity with Node.js (structural)
    Tool: Bash
    Preconditions: Both escript and roundtable.mjs available, mock CLIs named `gemini`/`codex` in test/support/bin/
    Steps:
      1. Run Node.js version: `PATH=test/support/bin:$PATH node roundtable.mjs --prompt "test" --timeout 30 | jq -S . > /tmp/node_out.json`
      2. Run Elixir version: `PATH=test/support/bin:$PATH ./roundtable --prompt "test" --timeout 30 | jq -S . > /tmp/elixir_out.json`
      3. diff the two files
      4. Assert identical (after jq -S key sorting)
    Expected Result: Structurally identical JSON output
    Failure Indicators: Key differences, missing fields, different values
    Evidence: .sisyphus/evidence/task-12-parity.txt
  ```

  **Commit**: YES
  - Message: `test(integration): add end-to-end tests with mock CLIs`
  - Files: `test/integration_test.exs`, `test/support/bin/gemini`, `test/support/bin/codex`, `test/support/bin/gemini_timeout`
  - Pre-commit: `mix test`

- [x] 13. SKILL.md Update + Documentation

  **What to do**:
  - Update `SKILL.md` to reference the Elixir escript instead of Node.js:
    - Change invocation from `node ~/.claude/skills/roundtable/roundtable.mjs` to `~/.claude/skills/roundtable/roundtable`
    - Update "Core Rule" section
    - Update all example invocations
    - Keep all flag documentation identical (same flags, same defaults)
    - Note that Erlang/OTP must be installed (`brew install elixir`)
  - Update `README.md` with:
    - Build instructions: `mix deps.get && mix escript.build`
    - Runtime requirement: Erlang/OTP + Elixir
    - Test instructions: `mix test`
  - Update `docs/elixir-port.md` status to "Complete" with actual date

  **Must NOT do**:
  - Change any CLI flag names or semantics
  - Remove Node.js documentation (keep roundtable.mjs reference in migration section)
  - Add Elixir-specific flags not in the Node.js version

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 4 (with Task 12)
  - **Blocks**: F1-F4
  - **Blocked By**: Task 11

  **References**:

  **Pattern References**:
  - `SKILL.md:1-165` — Current SKILL.md content — update invocation paths only, preserve all other content
  - `docs/elixir-port.md:109-115` — Migration path section — update status

  **WHY Each Reference Matters**:
  - SKILL.md is the interface contract for Claude Code — incorrect updates break the skill trigger

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: SKILL.md references correct binary
    Tool: Bash
    Preconditions: SKILL.md updated
    Steps:
      1. grep for "roundtable.mjs" in SKILL.md
      2. Assert it only appears in migration/historical context, not in invocation examples
      3. grep for "./roundtable" or "~/.claude/skills/roundtable/roundtable" in invocation section
      4. Assert present
    Expected Result: All invocation examples point to Elixir binary
    Failure Indicators: Old Node.js paths in active invocation examples
    Evidence: .sisyphus/evidence/task-13-skill-md.txt

  Scenario: README has build instructions
    Tool: Bash
    Preconditions: README.md updated
    Steps:
      1. grep for "mix escript.build" in README.md
      2. grep for "mix test" in README.md
      3. Assert both present
    Expected Result: Build and test instructions documented
    Failure Indicators: Missing build/test commands
    Evidence: .sisyphus/evidence/task-13-readme.txt
  ```

  **Commit**: YES
  - Message: `docs: update SKILL.md for Elixir escript`
  - Files: `SKILL.md`, `README.md`, `docs/elixir-port.md`
  - Pre-commit: —

---

## Final Verification Wave (MANDATORY — after ALL implementation tasks)

> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.

- [x] F1. **Plan Compliance Audit** — `oracle`
  Read the plan end-to-end. For each "Must Have": verify implementation exists (read file, run command). For each "Must NOT Have": search codebase for forbidden patterns — reject with file:line if found. Check evidence files exist in .sisyphus/evidence/. Compare deliverables against plan.
  Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT: APPROVE/REJECT`

- [x] F2. **Code Quality Review** — `unspecified-high`
  Run `mix compile --warnings-as-errors` + `mix test` + `mix format --check-formatted`. Review all `.ex` files for: `@spec` annotations on public functions, proper use of Behaviour callbacks, no `IO.inspect` in production code, no hardcoded paths, no `any()` typespecs. Check for AI slop: excessive comments, over-abstraction, generic variable names.
  Output: `Compile [PASS/FAIL] | Tests [N pass/N fail] | Format [PASS/FAIL] | Files [N clean/N issues] | VERDICT`

- [x] F3. **Real Manual QA** — `unspecified-high`
  Build escript. Execute EVERY QA scenario from EVERY task — follow exact steps, capture evidence. Test cross-task integration: full roundtable invocation with mock CLIs producing fixture output. Test edge cases: empty prompt, whitespace prompt, missing role, both CLIs missing, SIGTERM during execution, ROUNDTABLE_ACTIVE recursion guard. Save to `.sisyphus/evidence/final-qa/`.
  Output: `Scenarios [N/N pass] | Integration [N/N] | Edge Cases [N tested] | VERDICT`

- [x] F4. **Scope Fidelity Check** — `deep`
  For each task: read "What to do", read actual code. Verify 1:1 — everything in spec was built (no missing), nothing beyond spec was built (no creep). Check "Must NOT do" compliance: no Application module, no GenServer, no Logger, no extra deps. Detect cross-task contamination. Flag unaccounted files.
  Output: `Tasks [N/N compliant] | Contamination [CLEAN/N issues] | Unaccounted [CLEAN/N files] | VERDICT`

---

## Commit Strategy

| Commit | Message | Files | Pre-commit |
|-|-|-|-|
| 1 | `feat(elixir): scaffold Mix project with ExUnit and Jason` | mix.exs, lib/roundtable.ex (stub), test/test_helper.exs, .formatter.exs | `mix compile` |
| 2 | `feat(cli): define CLI behaviour callbacks` | lib/roundtable/cli/behaviour.ex, test/roundtable/cli/behaviour_test.exs | `mix test` |
| 3-4 | `feat(prompt): add role loading and prompt assembly` | lib/roundtable/prompt/*.ex, test/roundtable/prompt/*_test.exs | `mix test` |
| 5-6 | `feat(cli): add Gemini JSON and Codex JSONL parsers` | lib/roundtable/cli/{gemini,codex}.ex, test/fixtures/*, test/roundtable/cli/*_test.exs | `mix test` |
| 7 | `feat(output): add result builder and JSON encoding` | lib/roundtable/output.ex, test/roundtable/output_test.exs | `mix test` |
| 8 | `feat(runner): add Port-based CLI runner with shell wrapper` | lib/roundtable/cli/runner.ex, test/roundtable/cli/runner_test.exs, test/support/fake_cli.sh | `mix test` |
| 9 | `feat(args): add argument parsing with OptionParser` | lib/roundtable/args.ex, test/roundtable/args_test.exs | `mix test` |
| 10 | `feat(dispatcher): add parallel CLI dispatch via Task.Supervisor` | lib/roundtable/dispatcher.ex, test/roundtable/dispatcher_test.exs | `mix test` |
| 11 | `feat(main): wire up entry point and escript build` | lib/roundtable.ex, mix.exs (escript config) | `mix escript.build && mix test` |
| 12 | `test(integration): add end-to-end tests with mock CLIs` | test/integration_test.exs, test/support/*.sh | `mix test` |
| 13 | `docs: update SKILL.md for Elixir escript` | SKILL.md | — |

---

## Success Criteria

### Verification Commands
```bash
mix compile --warnings-as-errors  # Expected: zero warnings
mix test                          # Expected: all tests pass
mix format --check-formatted      # Expected: no formatting issues
mix escript.build                 # Expected: produces ./roundtable binary
./roundtable --prompt "hello" --timeout 5 | jq .  # Expected: valid JSON with gemini/codex/meta keys
ROUNDTABLE_ACTIVE=1 ./roundtable --prompt "test"; echo $?  # Expected: exit code 1, error JSON
```

### Final Checklist
- [ ] All "Must Have" items present and verified
- [ ] All "Must NOT Have" items absent (no Application, no GenServer, no Logger, no extra deps)
- [ ] All ExUnit tests pass
- [ ] Escript builds and produces valid JSON
- [ ] Output structurally matches Node.js version (`jq -S` comparison)
- [ ] Process cleanup works (no orphans after SIGTERM)
