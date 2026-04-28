package roundtable

// run_schema_test.go covers the schema-threading and validator-wiring
// inside Run (F04). These tests fail until the implementer adds
// ToolRequest.Schema, the BuildPromptSuffix append, and the post-drain
// Validate gate.
//
// Assertion traceability:
//   /features/3/oracle/assertions/1 → TestRunSchema_OmittedSchema_ByteEquivalence
//   /features/3/oracle/assertions/2 → TestRunSchema_StructuredPopulated,
//                                     TestRunSchema_StructuredError_MissingFence
//   /features/3/oracle/assertions/3 → TestRunSchema_OmittedSchema_NoStructuredKeys
//   C4 (separator)                  → TestRunSchema_SuffixSeparator_NonEmptyPromptSuffix,
//                                     TestRunSchema_SuffixSeparator_EmptyPromptSuffix
//   D4 (status gate)                → TestRunSchema_NonOkStatuses_ValidateSkipped
//   D2 (immutability / race)        → TestRunSchema_ConcurrentRaceDetector

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/TejGandham/roundtable/internal/roundtable/dispatchschema"
)

// --- stub backend ---

// stubBackend is a deterministic, in-process Backend for schema wiring tests.
// It captures the prompt it receives and returns a fixed response with the
// given status.
type stubBackend struct {
	mu           sync.Mutex
	prompts      []string // one entry per Run call
	responseText string
	status       string
}

func (s *stubBackend) Name() string                    { return "stub" }
func (s *stubBackend) Start(_ context.Context) error   { return nil }
func (s *stubBackend) Healthy(_ context.Context) error { return nil }

func (s *stubBackend) Run(_ context.Context, req Request) (*Result, error) {
	s.mu.Lock()
	s.prompts = append(s.prompts, req.Prompt)
	s.mu.Unlock()
	return &Result{
		Response: s.responseText,
		Model:    "stub-model",
		Status:   s.status,
	}, nil
}

func (s *stubBackend) Stop() error { return nil }

func (s *stubBackend) capturedPrompts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.prompts))
	copy(out, s.prompts)
	return out
}

// newSingleAgentBackends returns a backends map wired to a single "stub"
// provider using the supplied backend.
func newSingleAgentBackends(b Backend) map[string]Backend {
	return map[string]Backend{"stub": b}
}

// singleAgentReq builds a minimal ToolRequest that dispatches to the stub
// provider. promptSuffix may be empty.
func singleAgentReq(prompt, promptSuffix string) ToolRequest {
	return ToolRequest{
		Prompt:       prompt,
		PromptSuffix: promptSuffix,
		Agents:       []AgentSpec{{Name: "stub", Provider: "stub"}},
		Timeout:      10,
	}
}

// mustParseSchemaRun is a test helper that parses a JSON schema string and
// fatals if Parse returns an error.
func mustParseSchemaRun(t *testing.T, raw string) *dispatchschema.Schema {
	t.Helper()
	s, err := dispatchschema.Parse(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

// simpleSchema is the reusable schema for most Run-level tests.
func simpleSchema(t *testing.T) *dispatchschema.Schema {
	t.Helper()
	return mustParseSchemaRun(t, `{
		"type": "object",
		"properties": {
			"verdict": {"type": "string", "enum": ["approve", "reject"]},
			"score":   {"type": "number"}
		},
		"required": ["verdict", "score"]
	}`)
}

// --- /features/3/oracle/assertions/1 + 3 ---

// TestRunSchema_OmittedSchema_ByteEquivalence verifies that two Run calls on
// identical inputs produce byte-equal output when no schema is set.
// This is the byte-equivalence regression bar from assertions 1 and 3.
func TestRunSchema_OmittedSchema_ByteEquivalence(t *testing.T) {
	makeBackend := func() *stubBackend {
		return &stubBackend{responseText: "panelist text", status: "ok"}
	}

	req := singleAgentReq("hello", "")
	// req.Schema is nil — implementer must leave it nil.

	out1, err := Run(context.Background(), req, newSingleAgentBackends(makeBackend()))
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	out2, err := Run(context.Background(), req, newSingleAgentBackends(makeBackend()))
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}

	if !bytes.Equal(out1, out2) {
		t.Errorf("omitted-schema runs are not byte-equal:\nrun1: %s\nrun2: %s", out1, out2)
	}
}

// TestRunSchema_OmittedSchema_NoStructuredKeys guards the byte-equivalence bar
// by asserting neither "structured" nor "structured_error" key appears in the
// output when no schema is supplied (assertion 3 / researcher recommendation).
func TestRunSchema_OmittedSchema_NoStructuredKeys(t *testing.T) {
	backend := &stubBackend{responseText: "panelist text", status: "ok"}
	req := singleAgentReq("hello", "")

	out, err := Run(context.Background(), req, newSingleAgentBackends(backend))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if bytes.Contains(out, []byte(`"structured"`)) {
		t.Errorf("omitted-schema output contains %q key; must be absent.\nJSON: %s", "structured", out)
	}
	if bytes.Contains(out, []byte(`"structured_error"`)) {
		t.Errorf("omitted-schema output contains %q key; must be absent.\nJSON: %s", "structured_error", out)
	}
}

// --- /features/3/oracle/assertions/2 ---

// TestRunSchema_StructuredPopulated verifies that when a schema is set and the
// stub backend returns a conforming fenced JSON block, result.Structured is
// the parsed payload and result.StructuredError is nil.
func TestRunSchema_StructuredPopulated(t *testing.T) {
	schema := simpleSchema(t)
	fencedResponse := "Analysis:\n```json\n{\"verdict\":\"approve\",\"score\":9}\n```\n"

	backend := &stubBackend{responseText: fencedResponse, status: "ok"}
	req := singleAgentReq("evaluate this", "")
	req.Schema = schema // F04 field — undefined until implementer adds it

	out, err := Run(context.Background(), req, newSingleAgentBackends(backend))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Parse the top-level dispatch result to inspect the stub panelist result.
	var dr map[string]json.RawMessage
	if err := json.Unmarshal(out, &dr); err != nil {
		t.Fatalf("unmarshal dispatch result: %v", err)
	}
	stubRaw, ok := dr["stub"]
	if !ok {
		t.Fatal("dispatch result missing 'stub' key")
	}

	var result struct {
		Status          string              `json:"status"`
		Structured      json.RawMessage     `json:"structured"`
		StructuredError json.RawMessage     `json:"structured_error"`
	}
	if err := json.Unmarshal(stubRaw, &result); err != nil {
		t.Fatalf("unmarshal stub result: %v", err)
	}

	if result.Status != "ok" {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	if len(result.Structured) == 0 {
		t.Error("Structured is empty; expected populated JSON payload")
	}
	if len(result.StructuredError) != 0 {
		t.Errorf("StructuredError = %s; want absent on success", result.StructuredError)
	}

	// Verify the parsed content is the expected JSON.
	var got map[string]any
	if err := json.Unmarshal(result.Structured, &got); err != nil {
		t.Fatalf("unmarshal Structured: %v", err)
	}
	if got["verdict"] != "approve" {
		t.Errorf("Structured.verdict = %v, want approve", got["verdict"])
	}
}

// TestRunSchema_StructuredError_MissingFence verifies the negative case:
// when the stub backend returns a response with no fenced JSON block and a
// schema is set, result.Structured is nil and result.StructuredError.Kind
// is "missing_fence".
func TestRunSchema_StructuredError_MissingFence(t *testing.T) {
	schema := simpleSchema(t)
	noFenceResponse := "I forgot to add a code block. Just prose."

	backend := &stubBackend{responseText: noFenceResponse, status: "ok"}
	req := singleAgentReq("evaluate this", "")
	req.Schema = schema

	out, err := Run(context.Background(), req, newSingleAgentBackends(backend))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var dr map[string]json.RawMessage
	if err := json.Unmarshal(out, &dr); err != nil {
		t.Fatalf("unmarshal dispatch result: %v", err)
	}
	stubRaw, ok := dr["stub"]
	if !ok {
		t.Fatal("dispatch result missing 'stub' key")
	}

	var result struct {
		Status          string          `json:"status"`
		Structured      json.RawMessage `json:"structured"`
		StructuredError *struct {
			Kind string `json:"kind"`
		} `json:"structured_error"`
	}
	if err := json.Unmarshal(stubRaw, &result); err != nil {
		t.Fatalf("unmarshal stub result: %v", err)
	}

	if len(result.Structured) != 0 {
		t.Errorf("Structured = %s; want nil/absent on missing-fence failure", result.Structured)
	}
	if result.StructuredError == nil {
		t.Fatal("StructuredError is nil; want populated with Kind=missing_fence")
	}
	if result.StructuredError.Kind != dispatchschema.KindMissingFence {
		t.Errorf("StructuredError.Kind = %q, want %q", result.StructuredError.Kind, dispatchschema.KindMissingFence)
	}
}

// --- C3 / D7-a: empty-schema prompt non-degeneracy ---

// TestRunSchema_EmptyPropertiesSchema_PromptNonDegenerate verifies that a
// schema with empty properties still results in the schema suffix being
// appended to the prompt (non-degenerate). The backend must receive a prompt
// containing the fence instruction even when fields list is empty.
func TestRunSchema_EmptyPropertiesSchema_PromptNonDegenerate(t *testing.T) {
	emptySchema := mustParseSchemaRun(t, `{"type":"object","properties":{}}`)

	backend := &stubBackend{responseText: "```json\n{}\n```", status: "ok"}
	req := singleAgentReq("a question", "")
	req.Schema = emptySchema

	_, err := Run(context.Background(), req, newSingleAgentBackends(backend))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	prompts := backend.capturedPrompts()
	if len(prompts) == 0 {
		t.Fatal("stub backend received no prompts")
	}
	// The prompt must contain the fence instruction from BuildPromptSuffix.
	if !strings.Contains(prompts[0], "```json") {
		t.Errorf("prompt does not contain fence instruction; want schema suffix even for empty-properties schema.\nPrompt: %s", prompts[0])
	}
}

// --- C4: separator discipline ---

// TestRunSchema_SuffixSeparator_NonEmptyPromptSuffix verifies that when a
// ToolSpec PromptSuffix ends with non-newline prose (simulated via
// req.PromptSuffix = "\n\nFoo bar."), the BuildPromptSuffix output is
// separated from it by exactly "\n\n".
func TestRunSchema_SuffixSeparator_NonEmptyPromptSuffix(t *testing.T) {
	schema := simpleSchema(t)

	backend := &stubBackend{responseText: "```json\n{\"verdict\":\"approve\",\"score\":1}\n```", status: "ok"}
	req := singleAgentReq("main prompt", "\n\nFoo bar.")
	req.Schema = schema

	_, err := Run(context.Background(), req, newSingleAgentBackends(backend))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	prompts := backend.capturedPrompts()
	if len(prompts) == 0 {
		t.Fatal("stub backend received no prompts")
	}
	// The concatenated suffix prose must be separated from the schema suffix by "\n\n".
	if !strings.Contains(prompts[0], "Foo bar.\n\n") {
		t.Errorf("separator missing: prompt does not contain %q (trailing prose + \\n\\n before schema suffix).\nPrompt: %q", "Foo bar.\n\n", prompts[0])
	}
	// The schema suffix itself must follow.
	if !strings.Contains(prompts[0], "```json") {
		t.Errorf("prompt missing schema suffix fence instruction after separator.\nPrompt: %q", prompts[0])
	}
}

// TestRunSchema_SuffixSeparator_EmptyPromptSuffix verifies that when the
// PromptSuffix is empty, the schema suffix is still preceded by "\n\n" so
// it doesn't run on to the user prompt body.
func TestRunSchema_SuffixSeparator_EmptyPromptSuffix(t *testing.T) {
	schema := simpleSchema(t)

	backend := &stubBackend{responseText: "```json\n{\"verdict\":\"approve\",\"score\":1}\n```", status: "ok"}
	req := singleAgentReq("main prompt", "") // empty PromptSuffix
	req.Schema = schema

	_, err := Run(context.Background(), req, newSingleAgentBackends(backend))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	prompts := backend.capturedPrompts()
	if len(prompts) == 0 {
		t.Fatal("stub backend received no prompts")
	}
	// The user prompt ends right before \n\n + schema suffix.
	if !strings.Contains(prompts[0], "main prompt\n\n") {
		t.Errorf("separator missing: prompt does not contain %q.\nPrompt: %q", "main prompt\n\n", prompts[0])
	}
}

// --- D4: status gate ---

// TestRunSchema_NonOkStatuses_ValidateSkipped verifies that for each non-ok
// status, Validate is NOT invoked: result.Structured and result.StructuredError
// both remain nil.
func TestRunSchema_NonOkStatuses_ValidateSkipped(t *testing.T) {
	schema := simpleSchema(t)

	// These statuses must never invoke Validate even when schema != nil.
	nonOkStatuses := []string{"error", "rate_limited", "timeout", "terminated", "not_found", "probe_failed"}

	for _, status := range nonOkStatuses {
		status := status
		t.Run(status, func(t *testing.T) {
			// For not_found / probe_failed, the stub backend won't even be called
			// because Run handles them via NotFoundResult / ProbeFailedResult.
			// We test statuses that the backend returns directly ("error",
			// "rate_limited", "timeout", "terminated") by having the stub return
			// them. For not_found and probe_failed we use a missing/unhealthy backend.
			var backend Backend
			var backends map[string]Backend

			switch status {
			case "not_found":
				// No backend registered for "stub" provider → Run sets not_found.
				backends = map[string]Backend{}
			case "probe_failed":
				// Backend registered but Healthy returns error.
				backends = map[string]Backend{"stub": &unhealthyBackend{status: status}}
			default:
				b := &stubBackend{responseText: "prose only no fence", status: status}
				backend = b
				backends = newSingleAgentBackends(backend)
			}

			req := singleAgentReq("prompt", "")
			req.Schema = schema

			out, err := Run(context.Background(), req, backends)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			var dr map[string]json.RawMessage
			if err := json.Unmarshal(out, &dr); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			stubRaw, ok := dr["stub"]
			if !ok {
				t.Fatal("dispatch result missing 'stub' key")
			}

			var result struct {
				Status          string          `json:"status"`
				Structured      json.RawMessage `json:"structured"`
				StructuredError json.RawMessage `json:"structured_error"`
			}
			if err := json.Unmarshal(stubRaw, &result); err != nil {
				t.Fatalf("unmarshal stub result: %v", err)
			}

			if len(result.Structured) != 0 {
				t.Errorf("status=%q: Structured = %s; want nil/absent (Validate must be skipped)", status, result.Structured)
			}
			if len(result.StructuredError) != 0 {
				t.Errorf("status=%q: StructuredError = %s; want nil/absent (Validate must be skipped)", status, result.StructuredError)
			}
		})
	}
}

// unhealthyBackend is a Backend whose Healthy method returns an error, used to
// force probe_failed results without a real subprocess.
type unhealthyBackend struct {
	status string // unused, but documents intent
}

func (u *unhealthyBackend) Name() string                  { return "stub" }
func (u *unhealthyBackend) Start(_ context.Context) error { return nil }
func (u *unhealthyBackend) Healthy(_ context.Context) error {
	return &mockProbeError{}
}
func (u *unhealthyBackend) Run(_ context.Context, _ Request) (*Result, error) { return nil, nil }
func (u *unhealthyBackend) Stop() error                                        { return nil }

type mockProbeError struct{}

func (e *mockProbeError) Error() string { return "probe failed (test)" }

// --- D2: immutability / race detector ---

// TestRunSchema_ConcurrentRaceDetector runs N parallel dispatches sharing a
// single *dispatchschema.Schema to verify BuildPromptSuffix and Validate are
// read-only (no data race). Run with -race to catch any mutation.
func TestRunSchema_ConcurrentRaceDetector(t *testing.T) {
	const N = 6

	schema := simpleSchema(t)

	var wg sync.WaitGroup
	wg.Add(N)

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			backend := &stubBackend{
				responseText: "```json\n{\"verdict\":\"approve\",\"score\":7}\n```",
				status:       "ok",
			}
			req := singleAgentReq("concurrent prompt", "")
			req.Schema = schema // shared pointer

			_, _ = Run(context.Background(), req, newSingleAgentBackends(backend))
		}()
	}
	wg.Wait()
}
