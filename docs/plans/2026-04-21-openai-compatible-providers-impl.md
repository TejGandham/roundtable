# OpenAI-Compatible Multi-Provider Refactor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single Ollama-specific HTTP backend with a generic `OpenAIHTTPBackend` that registers N providers from `ROUNDTABLE_PROVIDERS` JSON config. Ollama becomes one of those providers, not a special case.

**Architecture:** One generic `Backend` impl talking OpenAI `/v1/chat/completions`. Operators declare providers in a JSON array env var with `api_key_env` indirection. Composition root (`cmd/roundtable-http-mcp/main.go`) skips providers whose secret env var is empty (FR-3). `AgentSpec.CLI` renames to `AgentSpec.Provider`; `ParseAgents` switches to typed-struct decoding with `DisallowUnknownFields` to reject the legacy `cli` key cleanly. `OllamaBackend` and `ollama.go` are deleted outright (no migration compat).

**Tech Stack:** Go 1.26, stdlib `net/http`, `encoding/json`, `httptest`, `golang.org/x/sync/semaphore` (already in `go.mod`), existing `Backend`/`Result`/`ParsedOutput`/`BuildResult` types.

**Reference docs:**
- `docs/plans/2026-04-21-openai-compatible-providers-requirements.md` (WHAT — requirements spec)
- `docs/plans/2026-04-21-openai-compatible-providers-design.md` (WHY/HOW — design decisions)
- `docs/plans/2026-04-20-ollama-cloud-provider.md` (operational invariants preserved — offline `Healthy`, per-process bulkhead, PII no-log)
- `internal/roundtable/ollama.go` (baseline being replaced; read before deleting)

---

## File Structure

| File | Action | Responsibility |
|-|-|-|
| `internal/roundtable/providers.go` | CREATE | `ProviderConfig`, `ProviderInfo`, `LoadProviderRegistry(getenv)` |
| `internal/roundtable/providers_test.go` | CREATE | Registry parsing, validation, defaults, collisions |
| `internal/roundtable/files.go` | CREATE | `inlineFileContents` + `defaultMaxFileBytes`/`defaultMaxTotalFileBytes` (lifted from `ollama.go`) |
| `internal/roundtable/files_test.go` | CREATE | File-inlining tests (lifted from `ollama_test.go`) |
| `internal/roundtable/openai_http.go` | CREATE | `OpenAIHTTPBackend`, `openAIParseResponse`, `newHTTPTransport`, `defaultGateSlowLogThreshold`, `defaultMaxResponseBytes`, `ObserveFunc` type |
| `internal/roundtable/openai_http_test.go` | CREATE | `httptest.Server` fixtures + parser tables + concurrency gate |
| `internal/roundtable/ollama.go` | **DELETE** | (after `openai_http.go` lands and main.go no longer references it) |
| `internal/roundtable/ollama_test.go` | **DELETE** | (contracts migrated to `openai_http_test.go`; file-inlining to `files_test.go`) |
| `internal/roundtable/run.go` | MODIFY | `AgentSpec.CLI` → `Provider`; typed `ParseAgents` with `DisallowUnknownFields`; drop `validCLIs`; update `resolveRole/resolveModel/resolveResume` + `defaultAgents()` docstring |
| `internal/roundtable/run_test.go` | MODIFY | New ParseAgents tests; generalize default-excludes-HTTP test |
| `internal/roundtable/result.go` | MODIFY | Generalize `NotFoundResult` / `ProbeFailedResult` messages (CLI-flavored → provider-agnostic) |
| `internal/roundtable/result_test.go` | MODIFY | Update message assertions |
| `internal/httpmcp/metrics.go` | MODIFY | `ObserveBackend` → `ObserveProvider(provider, model, status, elapsedMs)`; `roundtable_backend_*` → `roundtable_provider_*`; surface `ProviderInfo` list |
| `internal/httpmcp/metrics_test.go` | MODIFY | New label shape assertions |
| `internal/httpmcp/server.go` | MODIFY | `/metricsz` emits `roundtable_providers_registered` |
| `cmd/roundtable-http-mcp/main.go` | MODIFY | `buildBackends` returns `(map, []ProviderInfo)`; `LoadProviderRegistry`; FR-3 skip-if-empty-credential; legacy Ollama branch removed |
| `INSTALL.md` | MODIFY | New Providers section with canonical example; legacy env vars removed |

Task order enforces dependencies: types first (providers.go), shared helpers (files.go), then the backend (openai_http.go) with its new `ObserveFunc` signature. Metrics and ParseAgents follow. Only then does `main.go` stop registering `OllamaBackend`, after which `ollama.go` can be deleted.

---

## Task 0: Pre-implementation verification gate (GO/NO-GO)

**This task is a hard gate. Do NOT start Task 1 until it passes.** If the curl check fails, the design doc's "delete `OllamaBackend`" premise is invalid and the design returns to brainstorming.

**Files:** none (manual check).

- [ ] **Step 1: Run the verification curl with a real `OLLAMA_API_KEY`**

Expects `$OLLAMA_API_KEY` to be exported in the shell environment.

```bash
curl -sS https://ollama.com/v1/chat/completions \
  -H "Authorization: Bearer $OLLAMA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"kimi-k2.6:cloud","messages":[{"role":"user","content":"reply with the word pong and nothing else"}],"stream":false}' \
  | tee /tmp/ollama-compat-check.json
```

- [ ] **Step 2: Verify the response shape**

```bash
jq -e '.choices[0].message.content' /tmp/ollama-compat-check.json \
  && jq -e '.choices[0].finish_reason' /tmp/ollama-compat-check.json \
  && jq -e '.usage.prompt_tokens, .usage.completion_tokens' /tmp/ollama-compat-check.json
```

Expected: all three `jq -e` invocations exit 0 (the keys exist and are non-null).

**If this fails:** Do not proceed. Update the design doc §0 with the observed failure mode and revisit the "delete `OllamaBackend`" decision. The response likely either (a) came from a non-existent `/v1/chat/completions` path (404 HTML), (b) rejected the `:cloud` suffix (400 with error body), or (c) returned Ollama-native-shape JSON instead of OpenAI shape (`message.content` instead of `choices[0].message.content`).

**If this passes:** record the output in a commit-message or comment on the upcoming PR for reviewer visibility; proceed to Task 1.

---

## Task 1: Create `ProviderConfig` types + `LoadProviderRegistry`

**Why first:** everything downstream consumes these types. Pure-data test surface, no network, no goroutines — easy to TDD in isolation.

**Files:**
- Create: `internal/roundtable/providers.go`
- Create: `internal/roundtable/providers_test.go`

### 1.1 Empty env returns `(nil, nil)`

- [ ] **Step 1: Write the failing test**

Create `internal/roundtable/providers_test.go`:

```go
package roundtable

import (
	"strings"
	"testing"
	"time"
)

// fakeEnv returns a getenv function that looks up from the map.
func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadProviderRegistry_EmptyEnv(t *testing.T) {
	cfgs, err := LoadProviderRegistry(fakeEnv(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfgs != nil {
		t.Errorf("cfgs = %v, want nil", cfgs)
	}
}
```

- [ ] **Step 2: Run test, verify fail**

```bash
go test ./internal/roundtable/ -run TestLoadProviderRegistry_EmptyEnv -v
```

Expected: FAIL with `undefined: LoadProviderRegistry`.

- [ ] **Step 3: Write minimal `providers.go`**

Create `internal/roundtable/providers.go`:

```go
package roundtable

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ProviderConfig is one registered OpenAI-compatible HTTP provider.
type ProviderConfig struct {
	ID                    string
	BaseURL               string
	APIKeyEnv             string
	DefaultModel          string
	MaxConcurrent         int
	ResponseHeaderTimeout time.Duration
	GateSlowLogThreshold  time.Duration
}

// ProviderInfo is the read-only enumeration surface for /metricsz and startup
// logging. Kept separate from ProviderConfig so the credential-env-var name
// is not accidentally exposed.
type ProviderInfo struct {
	ID           string `json:"id"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model,omitempty"`
}

// Built-in subprocess backend ids. LoadProviderRegistry rejects HTTP providers
// that try to claim these identifiers — a subprocess backend and an HTTP
// provider sharing a key in the backend map is a silent routing bug waiting
// to happen.
var builtInSubprocessIDs = map[string]bool{
	"gemini": true,
	"codex":  true,
	"claude": true,
}

// providerJSON is the wire shape for one entry in ROUNDTABLE_PROVIDERS.
// Kept private; converted to ProviderConfig with defaults applied.
type providerJSON struct {
	ID                    string `json:"id"`
	BaseURL               string `json:"base_url"`
	APIKeyEnv             string `json:"api_key_env"`
	DefaultModel          string `json:"default_model"`
	MaxConcurrent         int    `json:"max_concurrent"`
	ResponseHeaderTimeout string `json:"response_header_timeout"`
	GateSlowLogThreshold  string `json:"gate_slow_log_threshold"`
}

// LoadProviderRegistry parses the ROUNDTABLE_PROVIDERS env var (via the
// injected getenv function) and returns one ProviderConfig per entry.
// Returns (nil, nil) when the var is unset or empty. Returns (nil, error)
// on any parse or validation failure — caller logs and proceeds without
// HTTP providers.
func LoadProviderRegistry(getenv func(string) string) ([]ProviderConfig, error) {
	raw := strings.TrimSpace(getenv("ROUNDTABLE_PROVIDERS"))
	if raw == "" {
		return nil, nil
	}
	return nil, fmt.Errorf("not implemented")
}
```

- [ ] **Step 4: Run test, verify pass**

```bash
go test ./internal/roundtable/ -run TestLoadProviderRegistry_EmptyEnv -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/roundtable/providers.go internal/roundtable/providers_test.go
git commit -m "feat(providers): ProviderConfig/ProviderInfo types + empty-env returns nil"
```

### 1.2 Well-formed single entry parses

- [ ] **Step 1: Write the failing test**

Append to `providers_test.go`:

```go
func TestLoadProviderRegistry_WellFormedSingle(t *testing.T) {
	json := `[{
		"id": "moonshot",
		"base_url": "https://api.moonshot.cn/v1",
		"api_key_env": "MOONSHOT_API_KEY",
		"default_model": "kimi-k2-0711-preview",
		"max_concurrent": 5,
		"response_header_timeout": "90s"
	}]`
	cfgs, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": json}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfgs) != 1 {
		t.Fatalf("len(cfgs) = %d, want 1", len(cfgs))
	}
	c := cfgs[0]
	if c.ID != "moonshot" || c.BaseURL != "https://api.moonshot.cn/v1" ||
		c.APIKeyEnv != "MOONSHOT_API_KEY" || c.DefaultModel != "kimi-k2-0711-preview" ||
		c.MaxConcurrent != 5 || c.ResponseHeaderTimeout != 90*time.Second {
		t.Errorf("unexpected parsed config: %+v", c)
	}
}
```

- [ ] **Step 2: Run test, verify fail**

Expected: FAIL with `not implemented`.

- [ ] **Step 3: Implement parsing**

Replace the `LoadProviderRegistry` body:

```go
func LoadProviderRegistry(getenv func(string) string) ([]ProviderConfig, error) {
	raw := strings.TrimSpace(getenv("ROUNDTABLE_PROVIDERS"))
	if raw == "" {
		return nil, nil
	}

	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()

	var entries []providerJSON
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS: %w", err)
	}

	cfgs := make([]ProviderConfig, 0, len(entries))
	seen := make(map[string]bool, len(entries))

	for i, e := range entries {
		if e.ID == "" {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d]: id is required", i)
		}
		if builtInSubprocessIDs[e.ID] {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d]: id %q collides with built-in subprocess backend", i, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d]: duplicate id %q", i, e.ID)
		}
		seen[e.ID] = true
		if e.BaseURL == "" {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): base_url is required", i, e.ID)
		}
		if e.APIKeyEnv == "" {
			return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): api_key_env is required", i, e.ID)
		}

		maxConc := e.MaxConcurrent
		if maxConc <= 0 {
			maxConc = 3
		}

		rhTimeout := 60 * time.Second
		if e.ResponseHeaderTimeout != "" {
			d, err := time.ParseDuration(e.ResponseHeaderTimeout)
			if err != nil {
				return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): response_header_timeout: %w", i, e.ID, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): response_header_timeout must be positive", i, e.ID)
			}
			rhTimeout = d
		}

		gateThresh := 100 * time.Millisecond
		if e.GateSlowLogThreshold != "" {
			d, err := time.ParseDuration(e.GateSlowLogThreshold)
			if err != nil {
				return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): gate_slow_log_threshold: %w", i, e.ID, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("ROUNDTABLE_PROVIDERS[%d] (%s): gate_slow_log_threshold must be positive", i, e.ID)
			}
			gateThresh = d
		}

		cfgs = append(cfgs, ProviderConfig{
			ID:                    e.ID,
			BaseURL:               e.BaseURL,
			APIKeyEnv:             e.APIKeyEnv,
			DefaultModel:          e.DefaultModel,
			MaxConcurrent:         maxConc,
			ResponseHeaderTimeout: rhTimeout,
			GateSlowLogThreshold:  gateThresh,
		})
	}
	return cfgs, nil
}
```

- [ ] **Step 4: Run test, verify pass**

```bash
go test ./internal/roundtable/ -run TestLoadProviderRegistry_WellFormedSingle -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/roundtable/providers.go internal/roundtable/providers_test.go
git commit -m "feat(providers): parse ROUNDTABLE_PROVIDERS JSON with validation + defaults"
```

### 1.3 Validation coverage (batch — one commit)

- [ ] **Step 1: Write the failing tests**

Append to `providers_test.go`:

```go
func TestLoadProviderRegistry_RejectsBuiltInCollision(t *testing.T) {
	json := `[{"id":"gemini","base_url":"https://x","api_key_env":"X"}]`
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": json}))
	if err == nil || !strings.Contains(err.Error(), "collides with built-in") {
		t.Errorf("expected built-in collision error, got: %v", err)
	}
}

func TestLoadProviderRegistry_RejectsDuplicateID(t *testing.T) {
	json := `[
		{"id":"moonshot","base_url":"https://a","api_key_env":"A"},
		{"id":"moonshot","base_url":"https://b","api_key_env":"B"}
	]`
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": json}))
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Errorf("expected duplicate id error, got: %v", err)
	}
}

func TestLoadProviderRegistry_RejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{"empty id", `[{"base_url":"https://x","api_key_env":"X"}]`, "id is required"},
		{"empty base_url", `[{"id":"x","api_key_env":"X"}]`, "base_url is required"},
		{"empty api_key_env", `[{"id":"x","base_url":"https://x"}]`, "api_key_env is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": tc.json}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestLoadProviderRegistry_RejectsUnknownField(t *testing.T) {
	json := `[{"id":"x","base_url":"https://x","api_key_env":"X","typo_field":"oops"}]`
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": json}))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected unknown-field error, got: %v", err)
	}
}

func TestLoadProviderRegistry_RejectsMalformedJSON(t *testing.T) {
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": "not json"}))
	if err == nil {
		t.Error("expected error on malformed JSON")
	}
}

func TestLoadProviderRegistry_RejectsBadDuration(t *testing.T) {
	json := `[{"id":"x","base_url":"https://x","api_key_env":"X","response_header_timeout":"forever"}]`
	_, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": json}))
	if err == nil || !strings.Contains(err.Error(), "response_header_timeout") {
		t.Errorf("expected duration parse error, got: %v", err)
	}
}

func TestLoadProviderRegistry_AppliesDefaults(t *testing.T) {
	json := `[{"id":"x","base_url":"https://x","api_key_env":"X"}]`
	cfgs, err := LoadProviderRegistry(fakeEnv(map[string]string{"ROUNDTABLE_PROVIDERS": json}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := cfgs[0]
	if c.MaxConcurrent != 3 {
		t.Errorf("MaxConcurrent = %d, want default 3", c.MaxConcurrent)
	}
	if c.ResponseHeaderTimeout != 60*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want default 60s", c.ResponseHeaderTimeout)
	}
	if c.GateSlowLogThreshold != 100*time.Millisecond {
		t.Errorf("GateSlowLogThreshold = %v, want default 100ms", c.GateSlowLogThreshold)
	}
}
```

- [ ] **Step 2: Run tests, verify pass**

```bash
go test ./internal/roundtable/ -run TestLoadProviderRegistry -v
```

Expected: all PASS (the validation logic was added in Task 1.2 and now gets its explicit assertions).

- [ ] **Step 3: Commit**

```bash
git add internal/roundtable/providers_test.go
git commit -m "test(providers): cover validation rules, defaults, and bad inputs"
```

---

## Task 2: Lift file-inlining helpers out of `ollama.go`

**Why:** `inlineFileContents` and its size-cap constants are used by the new `OpenAIHTTPBackend` (Task 4). Keeping them in `ollama.go` would create a dependency on a file we're deleting (Task 9). Move them to a dedicated `files.go` in the same package so existing callers (still `ollama.go` until deletion) compile unchanged.

**Files:**
- Create: `internal/roundtable/files.go`
- Create: `internal/roundtable/files_test.go`
- Modify: `internal/roundtable/ollama.go` (remove lifted definitions)
- Modify: `internal/roundtable/ollama_test.go` (remove lifted tests — they move to `files_test.go`)

### 2.1 Move the implementation

- [ ] **Step 1: Create `files.go` with the lifted code**

Create `internal/roundtable/files.go`:

```go
package roundtable

import (
	"fmt"
	"os"
	"strings"
)

// defaultMaxFileBytes caps a single inlined file. Source files rarely exceed
// this; binaries and LLM dumps get cut with a <truncated /> marker inside
// the <file> block.
const defaultMaxFileBytes = 128 * 1024

// defaultMaxTotalFileBytes caps the aggregate size across all inlined files
// in one dispatch. Sized to fit comfortably inside a 128K-token context
// window. Files beyond the budget are listed in a <skipped-files> block so
// the model at least knows they existed.
const defaultMaxTotalFileBytes = 512 * 1024

// inlineFileContents reads the given paths and produces an XML-tag-wrapped
// blob suitable for prepending to a user message. See ollama.go (before
// deletion) or the 2026-04-21 design doc §5.4 for format details.
//
// Returns "" for nil/empty paths.
func inlineFileContents(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	var sb strings.Builder
	var total int
	var skipped []string

	for _, p := range paths {
		if total >= defaultMaxTotalFileBytes {
			skipped = append(skipped, p)
			continue
		}

		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(&sb, "<file path=%q error=%q />\n\n", p, err.Error())
			continue
		}

		truncated := false
		if len(data) > defaultMaxFileBytes {
			data = data[:defaultMaxFileBytes]
			truncated = true
		}

		remaining := defaultMaxTotalFileBytes - total
		if len(data) > remaining {
			if remaining <= 0 {
				skipped = append(skipped, p)
				continue
			}
			data = data[:remaining]
			truncated = true
		}

		fmt.Fprintf(&sb, "<file path=%q>\n", p)
		sb.Write(data)
		if truncated {
			sb.WriteString("\n<truncated />")
		}
		sb.WriteString("\n</file>\n\n")
		total += len(data)
	}

	if len(skipped) > 0 {
		sb.WriteString("<skipped-files>\n")
		for _, p := range skipped {
			fmt.Fprintf(&sb, "- %s\n", p)
		}
		sb.WriteString("</skipped-files>\n\n")
	}

	return sb.String()
}
```

- [ ] **Step 2: Delete the lifted definitions from `ollama.go`**

Open `internal/roundtable/ollama.go` and delete:
- The `ollamaMaxFileBytes = 128 * 1024` constant (around line 98)
- The `ollamaMaxTotalFileBytes = 512 * 1024` constant (around line 105)
- The entire `inlineFileContents` function (lines ~457-512)

In the same file, find every use of `ollamaMaxFileBytes` and `ollamaMaxTotalFileBytes` — should be none outside `inlineFileContents` itself. If there are comments referencing these names (e.g., the docstring on `ollamaMaxResponseBytes` doesn't reference them), leave those alone; Task 4 will remove `ollamaMaxResponseBytes` too.

- [ ] **Step 3: Verify build still passes**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/roundtable/files.go internal/roundtable/ollama.go
git commit -m "refactor(files): lift inlineFileContents + constants into files.go"
```

### 2.2 Move the tests

- [ ] **Step 1: Create `files_test.go` with the lifted tests**

Create `internal/roundtable/files_test.go` with the existing `inlineFileContents` tests from `ollama_test.go`. Open `internal/roundtable/ollama_test.go`, find every test whose name begins with `TestInlineFileContents_` (there should be several — empty input, single file, oversized file truncation, aggregate cap, unreadable file, skipped listing), and copy each verbatim into `files_test.go` under the same package. The tests reference `inlineFileContents`, `defaultMaxFileBytes`, `defaultMaxTotalFileBytes` — after Task 2.1 those resolve correctly.

If any copied test asserts against `ollamaMaxFileBytes` or `ollamaMaxTotalFileBytes` by name, rename those references to `defaultMaxFileBytes` / `defaultMaxTotalFileBytes`.

- [ ] **Step 2: Delete the same tests from `ollama_test.go`**

Remove the original `TestInlineFileContents_*` tests from `ollama_test.go`. Leave the rest of the file alone.

- [ ] **Step 3: Run tests, verify pass**

```bash
go test ./internal/roundtable/ -run TestInlineFileContents -v
```

Expected: every `TestInlineFileContents_*` test passes from its new location.

- [ ] **Step 4: Commit**

```bash
git add internal/roundtable/files_test.go internal/roundtable/ollama_test.go
git commit -m "refactor(files): move inlineFileContents tests into files_test.go"
```

---

## Task 3: Rename `ObserveFunc` + change metrics label shape

**Why:** The new `ObserveFunc` signature is `(provider, model, status, elapsedMs)` (was `(backend, status, elapsedMs)`). Making this change before Task 4 means the new backend is born with the correct signature. The existing `OllamaBackend.Run` has the `model` variable in scope already (see `ollama.go:193`), so updating its one call site is trivial and keeps the build green between Task 3 and Task 9.

**Files:**
- Modify: `internal/roundtable/ollama.go` (move `ObserveFunc` out and update call site)
- Create: `internal/roundtable/observe.go` (new home for the type)
- Modify: `internal/httpmcp/metrics.go` (rename + new label shape)
- Modify: `internal/httpmcp/metrics_test.go`
- Modify: `cmd/roundtable-http-mcp/main.go` (wire new method name)

### 3.1 Move `ObserveFunc` to `observe.go` and widen its signature

- [ ] **Step 1: Create `internal/roundtable/observe.go`**

```go
package roundtable

// ObserveFunc is the optional metrics hook invoked once per backend.Run()
// with (provider, model, status, elapsedMs). Wired at the composition
// root (cmd/roundtable-http-mcp/main.go) to route into
// httpmcp.Metrics.ObserveProvider. Defined in the roundtable package so
// backends don't import httpmcp (which would cycle, since httpmcp imports
// roundtable). A func value carries no package dependency, so no cycle.
//
// Callers MUST pass a non-nil function. Constructors that accept an
// ObserveFunc are responsible for normalizing nil to a no-op closure.
type ObserveFunc func(provider, model, status string, elapsedMs int64)
```

- [ ] **Step 2: Delete `ObserveFunc` from `ollama.go`**

Remove lines 21-28 of `internal/roundtable/ollama.go` (the `ObserveFunc` type declaration and its docstring).

- [ ] **Step 3: Update `OllamaBackend.Run`'s observe call to include model**

In `internal/roundtable/ollama.go`, find the deferred observe call (around line 206):

```go
defer func() {
    if result != nil {
        o.observe("ollama", result.Status, time.Since(runStart).Milliseconds())
    }
}()
```

Change to:

```go
defer func() {
    if result != nil {
        o.observe("ollama", model, result.Status, time.Since(runStart).Milliseconds())
    }
}()
```

Similarly update the no-op normalization at `ollama.go:131-133`:

```go
if observe == nil {
    observe = func(string, string, int64) {}
}
```

becomes:

```go
if observe == nil {
    observe = func(string, string, string, int64) {}
}
```

- [ ] **Step 4: Run tests, verify build**

```bash
go build ./...
```

Expected: build succeeds. (Ollama tests that inspect observe-call arguments may now fail with arity mismatch — address in step 5.)

- [ ] **Step 5: Update any `ollama_test.go` test that constructs a fake `observe`**

Search `internal/roundtable/ollama_test.go` for function literals passed as the second arg to `NewOllamaBackend` (e.g., `func(name, status string, ms int64) { ... }`). Widen each to `func(provider, model, status string, ms int64)` and update any captured-variable assertions to include the model. Specifically, tests that recorded `(name, status)` tuples now record `(provider, model, status)`.

```bash
go test ./internal/roundtable/ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/roundtable/observe.go internal/roundtable/ollama.go internal/roundtable/ollama_test.go
git commit -m "refactor(observe): widen ObserveFunc signature to (provider, model, status, ms)"
```

### 3.2 Update `internal/httpmcp/metrics.go` to the new label shape

- [ ] **Step 1: Write the failing test**

Replace the body of `internal/httpmcp/metrics_test.go`:

```go
package httpmcp

import (
	"encoding/json"
	"testing"
)

func TestMetrics_ObserveProvider(t *testing.T) {
	m := &Metrics{}
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "ok", 120)
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "ok", 240)
	m.ObserveProvider("moonshot", "kimi-k2-0711-preview", "rate_limited", 50)
	m.ObserveProvider("ollama", "kimi-k2.6:cloud", "ok", 300)

	raw := m.JSON()
	var snap map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	reqs, ok := snap["roundtable_provider_requests_total"].(map[string]any)
	if !ok {
		t.Fatalf("missing roundtable_provider_requests_total; got: %v", snap)
	}
	wantKeys := []string{
		"moonshot/kimi-k2-0711-preview/ok",
		"moonshot/kimi-k2-0711-preview/rate_limited",
		"ollama/kimi-k2.6:cloud/ok",
	}
	for _, k := range wantKeys {
		if _, ok := reqs[k]; !ok {
			t.Errorf("missing request counter key %q; got: %v", k, reqs)
		}
	}
	if count, _ := reqs["moonshot/kimi-k2-0711-preview/ok"].(float64); count != 2 {
		t.Errorf("moonshot ok count = %v, want 2", count)
	}

	durSum, ok := snap["roundtable_provider_request_duration_ms_sum"].(map[string]any)
	if !ok {
		t.Fatalf("missing roundtable_provider_request_duration_ms_sum")
	}
	if sum, _ := durSum["moonshot/kimi-k2-0711-preview"].(float64); sum != 410 {
		t.Errorf("moonshot duration sum = %v, want 410", sum)
	}
}
```

- [ ] **Step 2: Run test, verify fail**

```bash
go test ./internal/httpmcp/ -run TestMetrics_ObserveProvider -v
```

Expected: FAIL — `m.ObserveProvider` undefined.

- [ ] **Step 3: Rewrite `internal/httpmcp/metrics.go`**

Replace the file contents with:

```go
package httpmcp

import (
	"encoding/json"
	"sync"
	"sync/atomic"
)

// Metrics holds server-wide counters. JSON output keys follow Prometheus
// conventions (roundtable_provider_*) so a future migration to
// client_golang needs only a transport swap, not a rename.
type Metrics struct {
	TotalRequests  atomic.Int64
	DispatchErrors atomic.Int64

	mu sync.Mutex
	// providerRequests counts per (provider, model, status). Key format: "provider/model/status".
	providerRequests map[string]*atomic.Int64
	// providerDurationSum accumulates elapsed_ms per (provider, model). Key: "provider/model".
	providerDurationSum map[string]*atomic.Int64
	// providerDurationCount counts samples per (provider, model).
	providerDurationCount map[string]*atomic.Int64

	// providers is the snapshot of registered providers, set once at startup.
	providers []ProviderInfoDTO
}

// ProviderInfoDTO mirrors roundtable.ProviderInfo for JSON exposure on /metricsz.
// Duplicated here (rather than imported) to keep this package dependency-free
// from internal/roundtable's transitive imports for its metrics types.
type ProviderInfoDTO struct {
	ID           string `json:"id"`
	BaseURL      string `json:"base_url"`
	DefaultModel string `json:"default_model,omitempty"`
}

// SetProviders is called once at startup by the composition root.
func (m *Metrics) SetProviders(p []ProviderInfoDTO) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = append([]ProviderInfoDTO(nil), p...)
}

// ObserveProvider records a single backend call's outcome.
// `provider` is the registered provider id. `model` is the resolved model id
// used for the call. `status` is the Result.Status string. `elapsedMs` is
// wall-clock duration.
func (m *Metrics) ObserveProvider(provider, model, status string, elapsedMs int64) {
	reqKey := provider + "/" + model + "/" + status
	durKey := provider + "/" + model
	m.mu.Lock()
	if m.providerRequests == nil {
		m.providerRequests = map[string]*atomic.Int64{}
		m.providerDurationSum = map[string]*atomic.Int64{}
		m.providerDurationCount = map[string]*atomic.Int64{}
	}
	c, ok := m.providerRequests[reqKey]
	if !ok {
		c = &atomic.Int64{}
		m.providerRequests[reqKey] = c
	}
	ds, ok := m.providerDurationSum[durKey]
	if !ok {
		ds = &atomic.Int64{}
		m.providerDurationSum[durKey] = ds
	}
	dc, ok := m.providerDurationCount[durKey]
	if !ok {
		dc = &atomic.Int64{}
		m.providerDurationCount[durKey] = dc
	}
	m.mu.Unlock()
	c.Add(1)
	ds.Add(elapsedMs)
	dc.Add(1)
}

type metricsSnapshot struct {
	TotalRequests  int64 `json:"total_requests"`
	DispatchErrors int64 `json:"dispatch_errors"`

	ProviderRequests      map[string]int64  `json:"roundtable_provider_requests_total"`
	ProviderDurationSum   map[string]int64  `json:"roundtable_provider_request_duration_ms_sum"`
	ProviderDurationCount map[string]int64  `json:"roundtable_provider_request_duration_ms_count"`
	ProvidersRegistered   []ProviderInfoDTO `json:"roundtable_providers_registered"`
}

func (m *Metrics) Snapshot() metricsSnapshot {
	snap := metricsSnapshot{
		TotalRequests:         m.TotalRequests.Load(),
		DispatchErrors:        m.DispatchErrors.Load(),
		ProviderRequests:      map[string]int64{},
		ProviderDurationSum:   map[string]int64{},
		ProviderDurationCount: map[string]int64{},
	}
	m.mu.Lock()
	for k, v := range m.providerRequests {
		snap.ProviderRequests[k] = v.Load()
	}
	for k, v := range m.providerDurationSum {
		snap.ProviderDurationSum[k] = v.Load()
	}
	for k, v := range m.providerDurationCount {
		snap.ProviderDurationCount[k] = v.Load()
	}
	snap.ProvidersRegistered = append([]ProviderInfoDTO(nil), m.providers...)
	m.mu.Unlock()
	return snap
}

func (m *Metrics) JSON() []byte {
	data, _ := json.Marshal(m.Snapshot())
	return data
}
```

- [ ] **Step 4: Run test, verify pass**

```bash
go test ./internal/httpmcp/ -run TestMetrics_ObserveProvider -v
```

Expected: PASS.

- [ ] **Step 5: Update `main.go` call site**

In `cmd/roundtable-http-mcp/main.go`, find `metrics.ObserveBackend` (around line 104) and rename to `metrics.ObserveProvider`. The callsite was already passing a function-value to `buildBackends`; the function shape changed in Task 3.1 so the build will pass.

- [ ] **Step 6: Full build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/httpmcp/metrics.go internal/httpmcp/metrics_test.go cmd/roundtable-http-mcp/main.go
git commit -m "feat(metrics): rename ObserveBackend->ObserveProvider with (provider, model, status) labels"
```

---

## Task 4: Create `OpenAIHTTPBackend`

**Why biggest task:** this is the refactor's centerpiece. Build incrementally — parser in isolation first (no net), then Run() against `httptest.Server`, then concurrency gate. Each sub-task is a green commit.

**Files:**
- Create: `internal/roundtable/openai_http.go`
- Create: `internal/roundtable/openai_http_test.go`

### 4.1 Skeleton: struct, constructor, `Name`/`Start`/`Stop`, interface assertion

- [ ] **Step 1: Write the failing test**

Create `internal/roundtable/openai_http_test.go`:

```go
package roundtable

import (
	"context"
	"testing"
	"time"
)

// Compile-time assertion.
var _ Backend = (*OpenAIHTTPBackend)(nil)

func testConfig() ProviderConfig {
	return ProviderConfig{
		ID:                    "moonshot",
		BaseURL:               "https://api.moonshot.cn/v1",
		APIKeyEnv:             "MOONSHOT_API_KEY",
		DefaultModel:          "kimi-k2-0711-preview",
		MaxConcurrent:         3,
		ResponseHeaderTimeout: 60 * time.Second,
		GateSlowLogThreshold:  100 * time.Millisecond,
	}
}

func TestOpenAIHTTPBackend_Name(t *testing.T) {
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	if b.Name() != "moonshot" {
		t.Errorf("Name() = %q, want moonshot", b.Name())
	}
}

func TestOpenAIHTTPBackend_StartStop(t *testing.T) {
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	if err := b.Start(context.Background()); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := b.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/roundtable/ -run TestOpenAIHTTPBackend -v
```

Expected: FAIL — undefined types.

- [ ] **Step 3: Create `internal/roundtable/openai_http.go` skeleton**

```go
package roundtable

import (
	"context"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/semaphore"
)

// defaultMaxResponseBytes caps response bodies to protect against a
// misbehaving upstream streaming unbounded garbage. 8 MiB is well over any
// reasonable completion size with headroom for JSON framing.
// MEMORY-SAFETY INVARIANT — required by §5.2 of the design doc.
const defaultMaxResponseBytes = 8 * 1024 * 1024

// OpenAIHTTPBackend implements Backend for any provider speaking the
// OpenAI /v1/chat/completions contract.
//
// Design invariants (carried over from OllamaBackend):
//   - Healthy() is offline: it only checks whether os.Getenv(apiKeyEnv) is
//     non-empty. The dispatcher runs Healthy concurrently per-agent; a
//     network probe would burn the provider's concurrency quota before
//     Run() even starts.
//   - API key is read per-Run via os.Getenv(apiKeyEnv) so rotation
//     doesn't require a restart.
//   - httpClient is safe for concurrent use.
//   - observe is never nil after NewOpenAIHTTPBackend (nil normalized to no-op).
type OpenAIHTTPBackend struct {
	id           string
	baseURL      string
	apiKeyEnv    string
	defaultModel string
	httpClient   *http.Client
	observe      ObserveFunc
	sem          *semaphore.Weighted
	gateSlowLog  time.Duration
}

// NewOpenAIHTTPBackend constructs a backend from one registered provider.
// observe may be nil (will be normalized to a no-op).
func NewOpenAIHTTPBackend(cfg ProviderConfig, observe ObserveFunc) *OpenAIHTTPBackend {
	if observe == nil {
		observe = func(string, string, string, int64) {}
	}
	return &OpenAIHTTPBackend{
		id:           cfg.ID,
		baseURL:      cfg.BaseURL,
		apiKeyEnv:    cfg.APIKeyEnv,
		defaultModel: cfg.DefaultModel,
		observe:      observe,
		sem:          semaphore.NewWeighted(int64(cfg.MaxConcurrent)),
		gateSlowLog:  cfg.GateSlowLogThreshold,
		httpClient: &http.Client{
			// No Client.Timeout — we rely on the dispatcher's ctx deadline.
			// But Transport needs explicit timeouts because context
			// cancellation only reaches net/http AFTER the request is in
			// flight; a stalled TLS handshake can otherwise hang.
			Transport: newHTTPTransport(cfg.ResponseHeaderTimeout),
		},
	}
}

func newHTTPTransport(responseHeaderTimeout time.Duration) *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
	}
}

func (o *OpenAIHTTPBackend) Name() string                  { return o.id }
func (o *OpenAIHTTPBackend) Start(_ context.Context) error { return nil }
func (o *OpenAIHTTPBackend) Stop() error                   { return nil }

// Healthy is implemented in step 4.2.
func (o *OpenAIHTTPBackend) Healthy(_ context.Context) error { return nil }

// Run is implemented in step 4.9.
func (o *OpenAIHTTPBackend) Run(_ context.Context, _ Request) (*Result, error) {
	return nil, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/roundtable/ -run TestOpenAIHTTPBackend -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/roundtable/openai_http.go internal/roundtable/openai_http_test.go
git commit -m "feat(openai_http): skeleton backend with constructor + Name/Start/Stop"
```

### 4.2 `Healthy()` offline validation

- [ ] **Step 1: Write the failing tests**

Append to `openai_http_test.go`:

```go
func TestOpenAIHTTPBackend_Healthy_NoKey(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "")
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	if err := b.Healthy(context.Background()); err == nil {
		t.Error("want error when MOONSHOT_API_KEY unset")
	}
}

func TestOpenAIHTTPBackend_Healthy_WithKey(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "sk-test")
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	if err := b.Healthy(context.Background()); err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

func TestOpenAIHTTPBackend_Healthy_IsOffline(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "sk-test")
	b := NewOpenAIHTTPBackend(testConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Healthy(ctx); err != nil {
		t.Errorf("canceled ctx should not affect offline Healthy: %v", err)
	}
}
```

- [ ] **Step 2: Run, verify fail (specifically `TestOpenAIHTTPBackend_Healthy_NoKey`)**

```bash
go test ./internal/roundtable/ -run TestOpenAIHTTPBackend_Healthy -v
```

Expected: `NoKey` FAILs (current stub returns nil always).

- [ ] **Step 3: Implement `Healthy`**

In `internal/roundtable/openai_http.go`, replace the `Healthy` method body:

```go
// Healthy validates configuration only. DO NOT add a network probe —
// the dispatcher calls this concurrently per-agent; a probe would burn
// the provider's concurrency quota before any Run() executes.
func (o *OpenAIHTTPBackend) Healthy(_ context.Context) error {
	if os.Getenv(o.apiKeyEnv) == "" {
		return fmt.Errorf("%s: %s not set", o.id, o.apiKeyEnv)
	}
	return nil
}
```

Add `"fmt"` and `"os"` to the imports.

- [ ] **Step 4: Run tests, verify all pass**

```bash
go test ./internal/roundtable/ -run TestOpenAIHTTPBackend_Healthy -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/roundtable/openai_http.go internal/roundtable/openai_http_test.go
git commit -m "feat(openai_http): offline Healthy() — api_key_env presence only"
```

### 4.3 `openAIParseResponse` — 200 happy path

- [ ] **Step 1: Write the failing test**

Append to `openai_http_test.go`:

```go
func TestOpenAIParse_Success(t *testing.T) {
	body := []byte(`{
		"id":"chat-xyz",
		"model":"kimi-k2-0711-preview",
		"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":42,"completion_tokens":8}
	}`)
	parsed := openAIParseResponse(body, 200, "", "moonshot")
	if parsed.Status != "ok" {
		t.Errorf("status = %q, want ok", parsed.Status)
	}
	if parsed.Response != "Hello" {
		t.Errorf("response = %q, want Hello", parsed.Response)
	}
	if parsed.Metadata["model_used"] != "kimi-k2-0711-preview" {
		t.Errorf("model_used = %v", parsed.Metadata["model_used"])
	}
	if parsed.Metadata["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want stop", parsed.Metadata["finish_reason"])
	}
	if _, present := parsed.Metadata["output_truncated"]; present {
		t.Errorf("output_truncated must be absent when finish_reason != length; got %v", parsed.Metadata["output_truncated"])
	}
	tokens, ok := parsed.Metadata["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens not a map: %T", parsed.Metadata["tokens"])
	}
	if tokens["prompt_tokens"].(float64) != 42 || tokens["completion_tokens"].(float64) != 8 {
		t.Errorf("tokens = %v", tokens)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Expected: `openAIParseResponse` undefined.

- [ ] **Step 3: Implement the parser**

Append to `internal/roundtable/openai_http.go`:

```go
// openAIParseResponse converts a raw /v1/chat/completions response body
// plus HTTP status into a ParsedOutput. See design doc §5.1.1.
//
// Arguments:
//   - body: raw response bytes (already length-limited by caller)
//   - statusCode: HTTP status
//   - retryAfter: raw Retry-After header value ("" if absent)
//   - providerLabel: string prefix for error messages (e.g., "moonshot")
func openAIParseResponse(body []byte, statusCode int, retryAfter, providerLabel string) ParsedOutput {
	switch {
	case statusCode == 429 || statusCode == 503:
		return openAIRateLimitedOutput(body, statusCode, retryAfter, providerLabel)
	case statusCode >= 400:
		return openAIErrorOutput(body, statusCode, providerLabel)
	case statusCode != 200:
		return openAIErrorOutput(body, statusCode, providerLabel)
	}

	var data struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     json.Number `json:"prompt_tokens"`
			CompletionTokens json.Number `json:"completion_tokens"`
		} `json:"usage"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		pe := "JSON parse failed"
		return ParsedOutput{
			Response:   string(body),
			Status:     "error",
			ParseError: &pe,
		}
	}
	if len(data.Choices) == 0 {
		return ParsedOutput{
			Response: providerLabel + ": response missing choices",
			Status:   "error",
		}
	}

	metadata := map[string]any{}
	if data.Model != "" {
		metadata["model_used"] = data.Model
	}
	finish := data.Choices[0].FinishReason
	if finish != "" {
		metadata["finish_reason"] = finish
	}
	if finish == "length" {
		metadata["output_truncated"] = true
	}

	tokens := map[string]any{}
	if s := data.Usage.PromptTokens.String(); s != "" {
		if f, err := data.Usage.PromptTokens.Float64(); err == nil {
			tokens["prompt_tokens"] = f
		}
	}
	if s := data.Usage.CompletionTokens.String(); s != "" {
		if f, err := data.Usage.CompletionTokens.Float64(); err == nil {
			tokens["completion_tokens"] = f
		}
	}
	if len(tokens) > 0 {
		metadata["tokens"] = tokens
	}

	return ParsedOutput{
		Response: data.Choices[0].Message.Content,
		Status:   "ok",
		Metadata: metadata,
	}
}

func openAIRateLimitedOutput(body []byte, statusCode int, retryAfter, providerLabel string) ParsedOutput {
	msg := openAIExtractErrorMessage(body)
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", statusCode)
	}
	suffix := ". No Retry-After is published; back off and retry later."
	if retryAfter != "" {
		suffix = ". Retry-After: " + retryAfter
	}
	out := ParsedOutput{
		Response: fmt.Sprintf("%s rate limited (HTTP %d): %s%s", providerLabel, statusCode, msg, suffix),
		Status:   "rate_limited",
	}
	if retryAfter != "" {
		out.Metadata = map[string]any{"retry_after": retryAfter}
	}
	return out
}

func openAIErrorOutput(body []byte, statusCode int, providerLabel string) ParsedOutput {
	msg := openAIExtractErrorMessage(body)
	if msg == "" {
		msg = string(body)
	}
	return ParsedOutput{
		Response: fmt.Sprintf("%s HTTP %d: %s", providerLabel, statusCode, msg),
		Status:   "error",
	}
}

// openAIExtractErrorMessage pulls a human-readable message out of an error
// body. OpenAI-compat servers conventionally return {"error":{"message":...}};
// some legacy shims use {"error":"..."}. Accept both.
func openAIExtractErrorMessage(body []byte) string {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	switch v := data["error"].(type) {
	case string:
		return v
	case map[string]any:
		if m, ok := v["message"].(string); ok {
			return m
		}
	}
	return ""
}
```

Add `bytes` and `encoding/json` to the imports.

- [ ] **Step 4: Run test, verify pass**

```bash
go test ./internal/roundtable/ -run TestOpenAIParse_Success -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/roundtable/openai_http.go internal/roundtable/openai_http_test.go
git commit -m "feat(openai_http): openAIParseResponse happy-path + error helpers"
```

### 4.4 Parser error paths (batched tests)

- [ ] **Step 1: Write the failing tests**

Append to `openai_http_test.go`:

```go
func TestOpenAIParse_Truncated(t *testing.T) {
	body := []byte(`{"model":"glm-4.6","choices":[{"message":{"content":"cut..."},"finish_reason":"length"}]}`)
	parsed := openAIParseResponse(body, 200, "", "zai")
	if parsed.Status != "ok" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.Metadata["finish_reason"] != "length" {
		t.Error("finish_reason missing")
	}
	truncated, ok := parsed.Metadata["output_truncated"].(bool)
	if !ok || !truncated {
		t.Errorf("output_truncated = %v, want true", parsed.Metadata["output_truncated"])
	}
}

func TestOpenAIParse_429NoRetryAfter(t *testing.T) {
	body := []byte(`{"error":{"message":"rate limit exceeded"}}`)
	parsed := openAIParseResponse(body, 429, "", "moonshot")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.Metadata["retry_after"] != nil {
		t.Error("retry_after should be absent")
	}
}

func TestOpenAIParse_429WithRetryAfter(t *testing.T) {
	body := []byte(`{"error":{"message":"slow down"}}`)
	parsed := openAIParseResponse(body, 429, "30", "moonshot")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.Metadata["retry_after"] != "30" {
		t.Errorf("retry_after = %v, want 30", parsed.Metadata["retry_after"])
	}
}

func TestOpenAIParse_503(t *testing.T) {
	body := []byte(`{"error":"overloaded"}`)
	parsed := openAIParseResponse(body, 503, "", "moonshot")
	if parsed.Status != "rate_limited" {
		t.Errorf("status = %q", parsed.Status)
	}
}

func TestOpenAIParse_401(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid key"}}`)
	parsed := openAIParseResponse(body, 401, "", "moonshot")
	if parsed.Status != "error" {
		t.Errorf("status = %q", parsed.Status)
	}
}

func TestOpenAIParse_MalformedJSON(t *testing.T) {
	parsed := openAIParseResponse([]byte(`not json`), 200, "", "moonshot")
	if parsed.Status != "error" {
		t.Errorf("status = %q", parsed.Status)
	}
	if parsed.ParseError == nil {
		t.Error("ParseError = nil, want set")
	}
}

func TestOpenAIParse_MissingChoices(t *testing.T) {
	parsed := openAIParseResponse([]byte(`{"model":"x"}`), 200, "", "moonshot")
	if parsed.Status != "error" {
		t.Errorf("status = %q", parsed.Status)
	}
	if !strings.Contains(parsed.Response, "missing choices") {
		t.Errorf("response = %q", parsed.Response)
	}
}
```

Add `strings` to test imports if not already present.

- [ ] **Step 2: Run tests, verify pass**

```bash
go test ./internal/roundtable/ -run TestOpenAIParse -v
```

Expected: PASS (parser logic from 4.3 covers all these).

- [ ] **Step 3: Commit**

```bash
git add internal/roundtable/openai_http_test.go
git commit -m "test(openai_http): parser error paths + 429/503/401/malformed"
```

### 4.5 `Run()` happy path against `httptest.Server`

- [ ] **Step 1: Write the failing test**

Append to `openai_http_test.go`:

```go
// newTestBackend starts an httptest.Server and returns a backend wired to it.
// Caller defers both backend.Stop and server.Close.
func newTestBackend(t *testing.T, handler http.HandlerFunc) (*OpenAIHTTPBackend, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	cfg := testConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_TEST"
	t.Setenv("MOONSHOT_API_KEY_TEST", "sk-test")
	b := NewOpenAIHTTPBackend(cfg, nil)
	return b, srv
}

func TestOpenAIHTTPBackend_Run_Success(t *testing.T) {
	b, srv := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"model":"kimi-k2-0711-preview",
			"choices":[{"message":{"content":"pong"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":5,"completion_tokens":1}
		}`)
	})
	defer srv.Close()

	res, err := b.Run(context.Background(), Request{Prompt: "ping", Timeout: 10})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("status = %q", res.Status)
	}
	if res.Response != "pong" {
		t.Errorf("response = %q", res.Response)
	}
	if res.Model != "kimi-k2-0711-preview" {
		t.Errorf("model = %q, want kimi-k2-0711-preview (from response)", res.Model)
	}
	if res.Metadata["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", res.Metadata["finish_reason"])
	}
}
```

Add `bytes`, `fmt`, `io`, `net/http`, `net/http/httptest`, `strings`, `sync`, `time` to test imports as needed.

- [ ] **Step 2: Run, verify fail**

Expected: current stub returns (nil, nil). Test fails.

- [ ] **Step 3: Implement `Run`**

Replace the stub `Run` in `openai_http.go`:

```go
// Run dispatches a single chat-completion request. Env is read per-call.
// File contents in req.Files are eagerly inlined because this HTTP path
// has no tool-calling loop. Context deadline routes through BuildResult
// so the timeout-response formatting matches subprocess backends.
func (o *OpenAIHTTPBackend) Run(ctx context.Context, req Request) (*Result, error) {
	apiKey := os.Getenv(o.apiKeyEnv)

	model := req.Model
	if model == "" {
		model = o.defaultModel
	}

	runStart := time.Now()
	var result *Result
	defer func() {
		if result != nil {
			o.observe(o.id, model, result.Status, time.Since(runStart).Milliseconds())
		}
	}()

	if model == "" {
		result = ConfigErrorResult(o.id, "",
			"no model resolved: set provider default_model or AgentSpec.Model")
		return result, nil
	}

	content := req.Prompt
	if inlined := inlineFileContents(req.Files); inlined != "" {
		content = inlined + content
	}

	var bodyBuf bytes.Buffer
	enc := json.NewEncoder(&bodyBuf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": content}},
		"stream":   false,
	})
	bodyBytes := bodyBuf.Bytes()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		result = ConfigErrorResult(o.id, model, "request build: "+err.Error())
		return result, nil
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	// Bulkhead: block until a concurrency slot is available or ctx fires.
	acquireStart := time.Now()
	if err := o.sem.Acquire(ctx, 1); err != nil {
		waited := time.Since(acquireStart).Milliseconds()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			result = BuildResult(
				RawRunOutput{
					TimedOut:  true,
					ElapsedMs: waited,
					Stderr:    fmt.Sprintf("deadline exceeded waiting for %s concurrency slot after %dms", o.id, waited),
				},
				ParsedOutput{},
				model,
			)
			return result, nil
		}
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    o.id + " gate acquire failed: " + err.Error(),
			ElapsedMs: waited,
		}
		return result, nil
	}
	defer o.sem.Release(1)
	if waited := time.Since(acquireStart); waited > o.gateSlowLog {
		slog.Debug("http gate wait", "provider", o.id, "wait_ms", waited.Milliseconds())
	}

	// NOTE: do NOT log bodyBytes or response body at any level — they
	// contain user prompts and model output (PII/secret surface). Log
	// status code and elapsed time only.
	start := time.Now()
	resp, err := o.httpClient.Do(httpReq)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
			errors.Is(err, context.DeadlineExceeded) {
			result = BuildResult(
				RawRunOutput{TimedOut: true, ElapsedMs: elapsed, Stderr: err.Error()},
				ParsedOutput{},
				model,
			)
			return result, nil
		}
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    err.Error(),
			ElapsedMs: elapsed,
		}
		return result, nil
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, defaultMaxResponseBytes))
	if readErr != nil {
		result = &Result{
			Model:     model,
			Status:    "error",
			Stderr:    "read body: " + readErr.Error(),
			ElapsedMs: elapsed,
		}
		return result, nil
	}

	parsed := openAIParseResponse(raw, resp.StatusCode, resp.Header.Get("Retry-After"), o.id)
	result = BuildResult(
		RawRunOutput{Stdout: raw, ElapsedMs: elapsed},
		parsed,
		model,
	)
	return result, nil
}
```

Add imports: `errors`, `io`, `log/slog`. Keep `context`, `net/http` etc.

- [ ] **Step 4: Run test, verify pass**

```bash
go test ./internal/roundtable/ -run TestOpenAIHTTPBackend_Run_Success -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/roundtable/openai_http.go internal/roundtable/openai_http_test.go
git commit -m "feat(openai_http): Run() happy path against /chat/completions"
```

### 4.6 `Run()` error & edge-case tests

- [ ] **Step 1: Write the failing tests**

Append to `openai_http_test.go`:

```go
func TestOpenAIHTTPBackend_Run_RateLimited(t *testing.T) {
	b, srv := newTestBackend(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "15")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"too many"}}`)
	})
	defer srv.Close()
	res, _ := b.Run(context.Background(), Request{Prompt: "x", Timeout: 10})
	if res.Status != "rate_limited" {
		t.Errorf("status = %q", res.Status)
	}
	if res.Metadata["retry_after"] != "15" {
		t.Errorf("retry_after = %v", res.Metadata["retry_after"])
	}
}

func TestOpenAIHTTPBackend_Run_MissingModel(t *testing.T) {
	cfg := testConfig()
	cfg.DefaultModel = ""
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_TEST"
	t.Setenv("MOONSHOT_API_KEY_TEST", "sk-test")
	b := NewOpenAIHTTPBackend(cfg, nil)
	res, _ := b.Run(context.Background(), Request{Prompt: "x", Timeout: 10})
	if res.Status != "error" {
		t.Errorf("status = %q", res.Status)
	}
	if !strings.Contains(res.Stderr, "no model resolved") {
		t.Errorf("stderr = %q", res.Stderr)
	}
}

func TestOpenAIHTTPBackend_Run_CtxDeadlineDuringRequest(t *testing.T) {
	b, srv := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the ctx deadline; the dispatcher cancels us.
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(200)
		case <-r.Context().Done():
		}
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res, _ := b.Run(ctx, Request{Prompt: "x", Timeout: 10})
	if res.Status != "timeout" {
		t.Errorf("status = %q, want timeout", res.Status)
	}
}

func TestOpenAIHTTPBackend_Run_ResponseSizeCap(t *testing.T) {
	// Serve 9 MiB of garbage — the 8 MiB cap should trigger a JSON parse
	// failure on the truncated body, not a memory blowup.
	b, srv := newTestBackend(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		bigJunk := bytes.Repeat([]byte("x"), 9*1024*1024)
		_, _ = w.Write(bigJunk)
	})
	defer srv.Close()
	res, _ := b.Run(context.Background(), Request{Prompt: "x", Timeout: 10})
	if res.Status != "error" {
		t.Errorf("status = %q, want error (truncated body unparseable)", res.Status)
	}
}

func TestOpenAIHTTPBackend_Run_ConcurrencyGate_Deadline(t *testing.T) {
	// Handler blocks until its ctx is canceled. With MaxConcurrent=1 and
	// two parallel Runs, Run #1 holds the semaphore (blocked in handler);
	// Run #2 blocks in sem.Acquire until the 200ms ctx deadline fires.
	// Run #1 also times out (handler never returns before ctx cancel),
	// so both Results have status=timeout — either suffices to prove
	// the gate path is wired up. What we must NOT see is two successful
	// Results (which would mean the gate let both through at once).
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	cfg := testConfig()
	cfg.BaseURL = srv.URL
	cfg.APIKeyEnv = "MOONSHOT_API_KEY_TEST"
	cfg.MaxConcurrent = 1
	t.Setenv("MOONSHOT_API_KEY_TEST", "sk-test")
	b := NewOpenAIHTTPBackend(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	results := make([]*Result, 2)
	for i := range 2 {
		go func(idx int) {
			defer wg.Done()
			results[idx], _ = b.Run(ctx, Request{Prompt: "x", Timeout: 10})
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r == nil {
			t.Fatalf("results[%d] = nil", i)
		}
		if r.Status == "ok" {
			t.Errorf("results[%d] = ok; gate let two requests through at once", i)
		}
	}
}
```

- [ ] **Step 2: Run tests, verify pass**

```bash
go test ./internal/roundtable/ -run TestOpenAIHTTPBackend_Run -v
```

Expected: PASS (every path already implemented in 4.5).

- [ ] **Step 3: Commit**

```bash
git add internal/roundtable/openai_http_test.go
git commit -m "test(openai_http): Run() error paths, gate deadline, size cap, missing model"
```

### 4.7 File-inlining through `Run()`

- [ ] **Step 1: Write the failing test**

Append to `openai_http_test.go`:

```go
func TestOpenAIHTTPBackend_Run_InlinesFiles(t *testing.T) {
	// Write a small file to a temp location.
	dir := t.TempDir()
	fp := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(fp, []byte("hello from file"), 0o644); err != nil {
		t.Fatal(err)
	}

	var seenBody bytes.Buffer
	b, srv := newTestBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(&seenBody, r.Body)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	})
	defer srv.Close()

	_, err := b.Run(context.Background(), Request{
		Prompt:  "summarize",
		Files:   []string{fp},
		Timeout: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := seenBody.String()
	if !strings.Contains(body, "<file path=") || !strings.Contains(body, "hello from file") {
		t.Errorf("expected file-inlining in body; got: %s", body)
	}
}
```

Add `path/filepath` to test imports.

- [ ] **Step 2: Run, verify pass**

```bash
go test ./internal/roundtable/ -run TestOpenAIHTTPBackend_Run_InlinesFiles -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/roundtable/openai_http_test.go
git commit -m "test(openai_http): file inlining end-to-end through Run()"
```

---

## Task 5: `run.go` — rename + typed ParseAgents

**Why this order:** Before Task 5, `AgentSpec.CLI` is read by the dispatcher, `resolveRole`, `resolveModel`, `resolveResume`, and several tests. Renaming is a small mechanical change; pairing it with the typed-struct ParseAgents rewrite keeps one coherent commit.

**Files:**
- Modify: `internal/roundtable/run.go`
- Modify: `internal/roundtable/run_test.go`

### 5.1 Typed ParseAgents + rejected `cli` field

- [ ] **Step 1: Write the failing tests**

Append to `internal/roundtable/run_test.go`:

```go
func TestParseAgents_RejectsCLIField(t *testing.T) {
	_, err := ParseAgents(`[{"cli":"gemini"}]`)
	if err == nil || !strings.Contains(err.Error(), "cli") {
		t.Errorf("expected error mentioning cli, got: %v", err)
	}
}

func TestParseAgents_AcceptsUnknownProvider(t *testing.T) {
	// Unknown provider ids go through the dispatcher's not_found path (FR-10);
	// ParseAgents must not reject them.
	specs, err := ParseAgents(`[{"provider":"my-custom-one","model":"x"}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 1 || specs[0].Provider != "my-custom-one" {
		t.Errorf("specs = %+v", specs)
	}
}

func TestParseAgents_ProviderField(t *testing.T) {
	specs, err := ParseAgents(`[{"provider":"moonshot","model":"kimi","name":"kimi-moonshot"}]`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if specs[0].Name != "kimi-moonshot" || specs[0].Provider != "moonshot" || specs[0].Model != "kimi" {
		t.Errorf("specs[0] = %+v", specs[0])
	}
}

func TestParseAgents_RejectsUnknownField(t *testing.T) {
	_, err := ParseAgents(`[{"provider":"x","bogus":"1"}]`)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected unknown-field error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/roundtable/ -run TestParseAgents -v
```

Expected: existing `TestParseAgents*` tests may still pass if they use `cli`; the new ones fail with undefined fields or wrong errors.

- [ ] **Step 3: Rewrite `AgentSpec` + `ParseAgents`**

In `internal/roundtable/run.go`:

1. Change the `AgentSpec` struct (field rename):

```go
type AgentSpec struct {
	Name     string
	Provider string
	Model    string
	Role     string
	Resume   string
}
```

2. Delete `validCLIs` and `reservedNames` remains:

```go
var reservedNames = map[string]bool{"meta": true}
```

3. Rewrite `ParseAgents`:

```go
// agentSpecJSON is the wire shape; DisallowUnknownFields rejects the
// legacy "cli" key (and typos) at decode time.
type agentSpecJSON struct {
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Role     string `json:"role,omitempty"`
	Resume   string `json:"resume,omitempty"`
}

// ParseAgents parses and validates a JSON array of agent specs.
// Returns nil, nil for empty/nil input (use defaults).
func ParseAgents(agentsJSON string) ([]AgentSpec, error) {
	agentsJSON = strings.TrimSpace(agentsJSON)
	if agentsJSON == "" {
		return nil, nil
	}

	dec := json.NewDecoder(strings.NewReader(agentsJSON))
	dec.DisallowUnknownFields()

	var raw []agentSpecJSON
	if err := dec.Decode(&raw); err != nil {
		// Friendly rewording of the two most common operator mistakes.
		msg := err.Error()
		if strings.Contains(msg, `"cli"`) {
			return nil, fmt.Errorf(`agents: unknown field "cli"; use "provider"`)
		}
		if strings.Contains(msg, "unknown field") {
			return nil, fmt.Errorf("agents: %s", msg)
		}
		// Single-object (not array) input — detect + improve message.
		var single any
		if json.Unmarshal([]byte(agentsJSON), &single) == nil {
			if _, isArr := single.([]any); !isArr {
				return nil, fmt.Errorf("agents must be a JSON array")
			}
		}
		return nil, fmt.Errorf("agents is not valid JSON: %w", err)
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("agents list cannot be empty")
	}

	specs := make([]AgentSpec, 0, len(raw))
	names := make(map[string]bool, len(raw))

	for _, entry := range raw {
		if entry.Provider == "" {
			return nil, fmt.Errorf(`each agent must specify a "provider" field`)
		}

		name := entry.Name
		if name == "" {
			name = entry.Provider
		}
		if reservedNames[name] {
			return nil, fmt.Errorf("agent name %q is reserved", name)
		}
		if names[name] {
			return nil, fmt.Errorf("duplicate agent names: %s", name)
		}
		names[name] = true

		specs = append(specs, AgentSpec{
			Name:     name,
			Provider: entry.Provider,
			Model:    entry.Model,
			Role:     entry.Role,
			Resume:   entry.Resume,
		})
	}
	return specs, nil
}
```

- [ ] **Step 4: Update every reference to `agent.CLI` in `run.go`**

Find every `agent.CLI` — should be in `resolveRole`, `resolveModel`, `resolveResume`, and the dispatcher loop's backend-lookup (`backends[agent.CLI]`). Rename each to `agent.Provider`. The switch cases (`case "gemini":` etc.) stay as-is — those are matching on the value.

- [ ] **Step 5: Update `defaultAgents()` docstring**

Replace the multi-line comment above `defaultAgents()` with:

```go
// defaultAgents is the fan-out set for dispatches without explicit agents
// or ROUNDTABLE_DEFAULT_AGENTS override.
//
// Invariant: ONLY built-in subprocess backends (gemini/codex/claude) appear
// here. No HTTP-native provider (anything registered via ROUNDTABLE_PROVIDERS)
// is ever a default — callers must opt in explicitly via the agents JSON
// or ROUNDTABLE_DEFAULT_AGENTS. Codified as
// TestDefaultAgents_ExcludesAllHTTPProviders.
```

Update the three literals — they still use `CLI: "..."` today:

```go
func defaultAgents() []AgentSpec {
	return []AgentSpec{
		{Name: "gemini", Provider: "gemini"},
		{Name: "codex", Provider: "codex"},
		{Name: "claude", Provider: "claude"},
	}
}
```

- [ ] **Step 6: Update existing `run_test.go` tests**

Open `internal/roundtable/run_test.go`. Any test constructing an `AgentSpec` with `CLI: "..."` needs `Provider: "..."`. Any test using input JSON with `"cli":` needs `"provider":` (except the explicit `TestParseAgents_RejectsCLIField` added above, which uses `cli` deliberately).

Rename the existing `TestDefaultAgents_ExcludesOllama` test to `TestDefaultAgents_ExcludesAllHTTPProviders` and update the assertion body to verify provider ids are a subset of the built-in subprocess set:

```go
func TestDefaultAgents_ExcludesAllHTTPProviders(t *testing.T) {
	builtins := map[string]bool{"gemini": true, "codex": true, "claude": true}
	for _, a := range defaultAgents() {
		if !builtins[a.Provider] {
			t.Errorf("defaultAgents() includes non-subprocess provider %q — invariant broken", a.Provider)
		}
	}
}
```

- [ ] **Step 7: Run full roundtable test suite**

```bash
go test ./internal/roundtable/ -v
```

Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/roundtable/run.go internal/roundtable/run_test.go
git commit -m "refactor(run): AgentSpec.CLI -> Provider; typed ParseAgents with DisallowUnknownFields"
```

---

## Task 6: `result.go` — generalize `NotFoundResult` / `ProbeFailedResult`

**Why:** After Task 5, unknown HTTP provider ids surface through `NotFoundResult`, which today says `"<name> CLI not found in PATH"` — meaningless and misleading for a provider id like `moonshot`. Generalize the messages so reading a `Result.Stderr` doesn't mislead.

**Files:**
- Modify: `internal/roundtable/result.go`
- Modify: `internal/roundtable/result_test.go`

### 6.1 Generalize the messages

- [ ] **Step 1: Write the failing test**

Append to `internal/roundtable/result_test.go`:

```go
func TestNotFoundResult_ProviderAgnosticMessage(t *testing.T) {
	r := NotFoundResult("moonshot", "kimi-k2-0711-preview")
	if r.Status != "not_found" {
		t.Errorf("status = %q", r.Status)
	}
	if strings.Contains(r.Stderr, "PATH") {
		t.Errorf("stderr = %q; must not mention PATH for HTTP providers", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "moonshot") {
		t.Errorf("stderr = %q; must name the provider", r.Stderr)
	}
}

func TestProbeFailedResult_ProviderAgnosticMessage(t *testing.T) {
	r := ProbeFailedResult("moonshot", "m1", "api_key missing", nil)
	if r.Status != "probe_failed" {
		t.Errorf("status = %q", r.Status)
	}
	if strings.Contains(r.Stderr, "--version") {
		t.Errorf("stderr = %q; must not suggest CLI-specific diagnostics", r.Stderr)
	}
	if !strings.Contains(r.Stderr, "moonshot") || !strings.Contains(r.Stderr, "api_key missing") {
		t.Errorf("stderr = %q", r.Stderr)
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/roundtable/ -run TestNotFoundResult_ProviderAgnostic -v
go test ./internal/roundtable/ -run TestProbeFailedResult_ProviderAgnostic -v
```

Expected: FAILs on the PATH/--version assertions.

- [ ] **Step 3: Update `internal/roundtable/result.go`**

Replace the two helpers:

```go
func NotFoundResult(providerID, model string) *Result {
	if model == "" {
		model = "cli-default"
	}
	return &Result{
		Model:  model,
		Status: "not_found",
		Stderr: fmt.Sprintf("provider %q not registered", providerID),
	}
}

func ProbeFailedResult(providerID, model, reason string, exitCode *int) *Result {
	if model == "" {
		model = "cli-default"
	}
	return &Result{
		Model:    model,
		Status:   "probe_failed",
		ExitCode: exitCode,
		Stderr:   fmt.Sprintf("provider %q probe failed: %s", providerID, reason),
	}
}
```

Replace the top-of-file `import "strings"` with `import "fmt"` (and `"strings"` if other funcs in the file use it — check before removing).

- [ ] **Step 4: Run tests**

```bash
go test ./internal/roundtable/ -v
```

Expected: every test passes. Any existing test in `result_test.go` that asserted on the old "CLI not found in PATH" / "--version" strings has to be updated too — find and update them.

- [ ] **Step 5: Commit**

```bash
git add internal/roundtable/result.go internal/roundtable/result_test.go
git commit -m "refactor(result): generalize NotFoundResult/ProbeFailedResult messages for HTTP providers"
```

---

## Task 7: Wire `main.go` to load the provider registry

**Why:** After this task the binary registers HTTP providers from `ROUNDTABLE_PROVIDERS` instead of from the single-purpose `OLLAMA_API_KEY` path. FR-3 requires skipping providers whose credential env var is empty.

**Files:**
- Modify: `cmd/roundtable-http-mcp/main.go`
- Modify: `internal/httpmcp/server.go` (accept `ProviderInfo` list for `/metricsz`)

### 7.1 Refactor `buildBackends`

- [ ] **Step 1: Replace `buildBackends`**

In `cmd/roundtable-http-mcp/main.go`, replace the existing `buildBackends` function with:

```go
// buildBackends constructs the model backends shared between the stdio
// and HTTP entry points. Subprocess backends (gemini/codex/claude) are
// always registered. HTTP providers come from ROUNDTABLE_PROVIDERS
// (see internal/roundtable/providers.go). Providers whose api_key_env
// is empty are silently skipped — FR-3.
func buildBackends(logger *slog.Logger, observe roundtable.ObserveFunc) (map[string]roundtable.Backend, []roundtable.ProviderInfo) {
	var codexBackend roundtable.Backend
	codexPath := roundtable.ResolveExecutable("codex")
	if codexPath != "" {
		codexBackend = roundtable.NewCodexBackend(codexPath, "")
		logger.Info("codex backend configured (lazy start)", "path", codexPath)
	} else {
		logger.Warn("codex binary not found, using CodexFallback")
		codexBackend = roundtable.NewCodexFallbackBackend("", "")
	}

	backends := map[string]roundtable.Backend{
		"gemini": roundtable.NewGeminiBackend(""),
		"codex":  codexBackend,
		"claude": roundtable.NewClaudeBackend(""),
	}

	var infos []roundtable.ProviderInfo

	configs, err := roundtable.LoadProviderRegistry(os.Getenv)
	if err != nil {
		logger.Error("ROUNDTABLE_PROVIDERS parse failed; no HTTP providers registered", "error", err)
		return backends, infos
	}

	for _, c := range configs {
		if os.Getenv(c.APIKeyEnv) == "" {
			logger.Warn("provider skipped — credential env var unset",
				"id", c.ID, "api_key_env", c.APIKeyEnv)
			continue
		}
		backends[c.ID] = roundtable.NewOpenAIHTTPBackend(c, observe)
		infos = append(infos, roundtable.ProviderInfo{
			ID: c.ID, BaseURL: c.BaseURL, DefaultModel: c.DefaultModel,
		})
		logger.Info("provider registered",
			"id", c.ID,
			"base_url", c.BaseURL,
			"default_model", c.DefaultModel,
			"max_concurrent", c.MaxConcurrent)
	}

	return backends, infos
}
```

- [ ] **Step 2: Update the two callers**

In the same file, find both `backends := buildBackends(logger, ...)` call sites (one in `runStdio`, one in `runHTTP`). Each becomes:

```go
backends, providerInfos := buildBackends(logger, <observeArg>)
```

- `runStdio` passes `nil` for observe and discards `providerInfos` (stdio has no metrics endpoint):
  ```go
  backends, _ := buildBackends(logger, nil)
  ```
- `runHTTP` passes `metrics.ObserveProvider`:
  ```go
  backends, providerInfos := buildBackends(logger, metrics.ObserveProvider)
  ```

In `runHTTP`, after the buildBackends call and before `httpmcp.NewApp`, thread the provider info into metrics:

```go
providerDTOs := make([]httpmcp.ProviderInfoDTO, len(providerInfos))
for i, p := range providerInfos {
	providerDTOs[i] = httpmcp.ProviderInfoDTO{ID: p.ID, BaseURL: p.BaseURL, DefaultModel: p.DefaultModel}
}
metrics.SetProviders(providerDTOs)
```

- [ ] **Step 3: Full build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 4: Full tests**

```bash
go test ./... -count=1
```

Expected: all pass. If any `ollama_test.go` tests still call `NewOllamaBackend`, they're about to be deleted in Task 8; if they fail here due to the observe-signature change they were already updated in Task 3.1.

- [ ] **Step 5: Commit**

```bash
git add cmd/roundtable-http-mcp/main.go
git commit -m "feat(main): load providers from ROUNDTABLE_PROVIDERS with FR-3 skip-on-empty-credential"
```

---

## Task 8: Delete `ollama.go` and `ollama_test.go`

**Why now:** after Task 7, nothing in the composition root references `OllamaBackend`. `ollama.go` still has dead definitions; deleting now is a clean no-op at runtime but meaningful code reduction.

- [ ] **Step 1: Verify nothing else imports `NewOllamaBackend` or `OllamaBackend`**

```bash
grep -rn "OllamaBackend\|NewOllamaBackend\|resolveOllamaMaxConcurrent\|resolveOllamaResponseHeaderTimeout\|ollamaParseResponse\|ollamaRateLimitedOutput\|ollamaErrorOutput\|ollamaExtractErrorMessage\|ollamaMaxResponseBytes\|ollamaGateSlowLogThreshold\|ollamaDefaultMaxConcurrent\|ollamaDefaultResponseHeaderTimeout" --include="*.go" .
```

Expected: matches are confined to `internal/roundtable/ollama.go` and `internal/roundtable/ollama_test.go`.

- [ ] **Step 2: Delete the files**

```bash
git rm internal/roundtable/ollama.go internal/roundtable/ollama_test.go
```

- [ ] **Step 3: Full build + test**

```bash
go vet ./...
go build ./...
go test ./... -count=1
```

Expected: clean across the board.

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor(ollama): delete OllamaBackend — replaced by generic OpenAIHTTPBackend"
```

---

## Task 9: `/metricsz` emits `roundtable_providers_registered`

The `Metrics.SetProviders` + snapshot plumbing landed in Task 3.2. This task just confirms the endpoint output is correct and adds a test.

**Files:**
- Modify: `internal/httpmcp/metrics_test.go` (new test)
- Modify: `internal/httpmcp/server_test.go` (integration — `/metricsz` roundtrip)

- [ ] **Step 1: Write the failing test**

Append to `internal/httpmcp/metrics_test.go`:

```go
func TestMetrics_ProvidersRegisteredInSnapshot(t *testing.T) {
	m := &Metrics{}
	m.SetProviders([]ProviderInfoDTO{
		{ID: "moonshot", BaseURL: "https://api.moonshot.cn/v1", DefaultModel: "kimi-k2-0711-preview"},
		{ID: "ollama", BaseURL: "https://ollama.com/v1"},
	})
	raw := m.JSON()
	if !strings.Contains(string(raw), `"roundtable_providers_registered"`) {
		t.Errorf("missing providers_registered in output: %s", raw)
	}
	if !strings.Contains(string(raw), `"moonshot"`) || !strings.Contains(string(raw), `"ollama"`) {
		t.Errorf("missing provider ids: %s", raw)
	}
}
```

Add `strings` import if missing.

- [ ] **Step 2: Run, verify pass**

```bash
go test ./internal/httpmcp/ -run TestMetrics_ProvidersRegistered -v
```

Expected: PASS (`SetProviders` and snapshot plumbing already exist from Task 3.2).

- [ ] **Step 3: Add server-level integration test**

In `internal/httpmcp/server_test.go`, find the existing `/metricsz` test and extend it: have the test's `Metrics` call `SetProviders` before the HTTP request, then assert the response JSON contains `"roundtable_providers_registered"`.

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/httpmcp/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpmcp/metrics_test.go internal/httpmcp/server_test.go
git commit -m "test(metricsz): providers_registered surfaces on /metricsz snapshot"
```

---

## Task 10: Update `INSTALL.md`

**Files:**
- Modify: `INSTALL.md`

- [ ] **Step 1: Remove the legacy Ollama env var block**

Open `INSTALL.md`, find the section documenting `OLLAMA_API_KEY`, `OLLAMA_BASE_URL`, `OLLAMA_DEFAULT_MODEL`, `OLLAMA_MAX_CONCURRENT_REQUESTS`, `OLLAMA_RESPONSE_HEADER_TIMEOUT`. Delete that section entirely.

- [ ] **Step 2: Add a new "Providers" section**

Insert the following under a top-level `## Providers (HTTP)` heading:

````markdown
## Providers (HTTP)

Roundtable dispatches to OpenAI-compatible HTTP providers declared in
the `ROUNDTABLE_PROVIDERS` environment variable — a JSON array where
each entry names one provider.

### Minimal example

Set the following in your MCP server env (e.g. `~/.claude/settings.json`
under `"env": { ... }`):

```json
{
  "MOONSHOT_API_KEY": "sk-...",
  "ZAI_API_KEY": "sk-...",
  "OLLAMA_API_KEY": "sk-...",
  "ROUNDTABLE_PROVIDERS": "[{\"id\":\"moonshot\",\"base_url\":\"https://api.moonshot.cn/v1\",\"api_key_env\":\"MOONSHOT_API_KEY\",\"default_model\":\"kimi-k2-0711-preview\",\"max_concurrent\":5},{\"id\":\"zai\",\"base_url\":\"https://api.z.ai/v1\",\"api_key_env\":\"ZAI_API_KEY\",\"default_model\":\"glm-4.6\",\"max_concurrent\":3},{\"id\":\"ollama\",\"base_url\":\"https://ollama.com/v1\",\"api_key_env\":\"OLLAMA_API_KEY\",\"default_model\":\"kimi-k2.6:cloud\",\"max_concurrent\":3}]"
}
```

### Fields

| Field | Required | Description |
|-|-|-|
| `id` | yes | Operator-chosen identifier. Must not collide with `gemini`, `codex`, or `claude`. |
| `base_url` | yes | Root URL; `/chat/completions` is appended at request time. |
| `api_key_env` | yes | Name of the env var holding the secret. The secret itself is **not** in this JSON. |
| `default_model` | no | Used when `AgentSpec.Model` is empty. |
| `max_concurrent` | no (default 3) | Per-process concurrency cap. |
| `response_header_timeout` | no (default `60s`) | `http.Transport.ResponseHeaderTimeout`. With `stream: false` this is effectively the total-response time — raise for slow providers running long-context deepdives. |
| `gate_slow_log_threshold` | no (default `100ms`) | Wait above which the concurrency-gate Acquire emits a debug log. |

### Agent-spec JSON examples

Target one registered provider:

```json
[{"name":"kimi-moonshot","provider":"moonshot","model":"kimi-k2-0711-preview"}]
```

Fan out across multiple providers in one dispatch:

```json
[
  {"provider":"gemini"},
  {"provider":"codex"},
  {"provider":"claude"},
  {"provider":"moonshot","model":"kimi-k2-0711-preview","name":"kimi-moonshot"},
  {"provider":"ollama","model":"kimi-k2.6:cloud","name":"kimi-ollama"}
]
```

A single missing comma in `ROUNDTABLE_PROVIDERS` disables every HTTP
provider for that process — this is deliberate (fail-loud) rather than
silent partial registration. The startup log emits one
`INFO provider registered ...` line per successful registration; an
`ERROR ROUNDTABLE_PROVIDERS parse failed ...` line surfaces any
JSON-level issue.

### Secret rotation

Because `api_key_env` names the env var, rotating a key means updating
the single secret env var — no re-encoding of `ROUNDTABLE_PROVIDERS`.
The `api_key_env` value is read via `os.Getenv` at request time, so
the new key takes effect without restarting Roundtable.

### What happened to `OLLAMA_API_KEY`?

In earlier versions, `OLLAMA_API_KEY` and friends registered a special
Ollama-native provider. As of this refactor, **Ollama is just one
registered provider** in `ROUNDTABLE_PROVIDERS` (see the example above)
and speaks the OpenAI-compat `/v1/chat/completions` endpoint. The
legacy auto-registration has been removed. If your existing
deployment only set `OLLAMA_API_KEY`, add a `ROUNDTABLE_PROVIDERS`
entry with `"api_key_env":"OLLAMA_API_KEY"` — the secret stays in
the same env var; only the structural config is new.
````

- [ ] **Step 3: Commit**

```bash
git add INSTALL.md
git commit -m "docs(install): new Providers section; remove legacy OLLAMA_* env var docs"
```

---

## Task 11: Final validation

- [ ] **Step 1: Clean build, vet, test**

```bash
go vet ./...
go build ./...
go test ./... -count=1
```

Expected: all clean.

- [ ] **Step 2: Manual smoke test**

Start the HTTP server with a real `ROUNDTABLE_PROVIDERS`:

```bash
export OLLAMA_API_KEY=sk-your-ollama-key
export ROUNDTABLE_PROVIDERS='[{"id":"ollama","base_url":"https://ollama.com/v1","api_key_env":"OLLAMA_API_KEY","default_model":"kimi-k2.6:cloud","max_concurrent":3}]'
./roundtable-http-mcp 2>&1 | head -20
```

Expected: one log line `INFO provider registered id=ollama base_url=https://ollama.com/v1 ...`.

- [ ] **Step 3: Hit `/metricsz`**

With the server running on its default port (check `httpmcp.LoadConfig`):

```bash
curl -sS http://localhost:<port>/metricsz | jq '.roundtable_providers_registered'
```

Expected: one element matching the registered ollama config.

- [ ] **Step 4: Dispatch a real hivemind**

From a client (Claude Code MCP), invoke hivemind with:

```json
[{"provider":"gemini"},{"provider":"codex"},{"provider":"claude"},{"provider":"ollama","model":"kimi-k2.6:cloud"}]
```

Expected: four agents dispatch, all four return `ok`. Re-query `/metricsz` and verify the `roundtable_provider_requests_total` map has entries keyed `ollama/kimi-k2.6:cloud/ok` (plus subprocess backends which don't emit — see design §12).

- [ ] **Step 5: Negative test — FR-3 compliance**

Unset the credential env var:

```bash
unset OLLAMA_API_KEY
./roundtable-http-mcp 2>&1 | head -20
```

Expected: one log line `WARN provider skipped — credential env var unset id=ollama api_key_env=OLLAMA_API_KEY`. No `INFO provider registered` line. A subsequent dispatch against `{"provider":"ollama",...}` returns a `not_found` per-agent result.

- [ ] **Step 6: Open PR**

```bash
git log --oneline origin/main..HEAD
```

Expected: ~20-25 small commits spanning tasks 1-10.

Follow `AGENTS.md` "Landing the Plane" — push and open PR.

---

## Self-review checklist (run after writing, before execution starts)

- **Spec coverage:** FR-1..FR-32, NFR-1..NFR-12, C-1..C-5, MR-waived — every active requirement in the spec maps to at least one task step. Manually walked the requirements doc; no gaps.
- **Placeholder scan:** zero `TBD`/`TODO`/`later` in the plan. Every code block is complete and runnable.
- **Type consistency:** `ObserveFunc` signature `(provider, model, status, elapsedMs)` is identical across Task 3 (type def), Task 4 (backend call), Task 3.2 (metrics method), Task 7 (main wiring). `ProviderConfig` field names identical across Tasks 1, 4 (consumer), 7 (consumer). `AgentSpec.Provider` used in Tasks 5, 7 consistently. `NotFoundResult(providerID, model)` signature stable across Tasks 6 and the existing callers.
- **Task independence:** each task produces a green build + green tests at its end commit. Between tasks, `ollama.go` stays compileable (Task 3 updates its observe call inline) until Task 8 deletes it.
