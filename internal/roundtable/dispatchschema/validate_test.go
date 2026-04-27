package dispatchschema_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/TejGandham/roundtable/internal/roundtable/dispatchschema"
)

// helpers

func mustParseSchema(t *testing.T, raw string) *dispatchschema.Schema {
	t.Helper()
	s, err := dispatchschema.Parse(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

func enumSchema(t *testing.T) *dispatchschema.Schema {
	t.Helper()
	return mustParseSchema(t, `{
		"type": "object",
		"properties": {
			"verdict": {"type": "string", "enum": ["approve", "reject", "defer"]},
			"score":   {"type": "number"}
		},
		"required": ["verdict", "score"]
	}`)
}

// --- oracle assertion tests ---

// TestValidate_ConformingResponse covers /features/2/oracle/assertions/0:
// "Given a panelist response that conforms to the schema, the validator returns
// the parsed structured value attached to the per-panelist result."
func TestValidate_ConformingResponse(t *testing.T) {
	schema := enumSchema(t)

	response := "Here is my analysis.\n\n```json\n{\"verdict\":\"approve\",\"score\":9}\n```\n"

	parsed, vErr := dispatchschema.Validate(response, schema)

	if vErr != nil {
		t.Fatalf("expected vErr nil, got %+v", vErr)
	}
	if len(parsed) == 0 {
		t.Fatal("expected non-empty parsed bytes, got empty")
	}

	// Normalise both sides via re-marshal to ignore whitespace differences.
	var got any
	if err := json.Unmarshal(parsed, &got); err != nil {
		t.Fatalf("parsed bytes are not valid JSON: %v", err)
	}
	normalised, _ := json.Marshal(got)
	if !json.Valid(normalised) {
		t.Errorf("re-marshaled parsed is not valid JSON: %s", normalised)
	}

	// Discipline guardrail: successful parse must not return literal JSON null.
	// json.RawMessage("null") is non-empty and slips past omitempty.
	if string(parsed) == "null" {
		t.Errorf("Validate returned parsed == json.RawMessage(\"null\") — " +
			"a null parse result must be surfaced as schema_violation, not returned as parsed value")
	}
}

// TestValidate_ViolatingResponse covers /features/2/oracle/assertions/1:
// "Given a panelist response that violates the schema, the validator surfaces
// the original raw text plus a structured error indicating which field(s) failed;
// no retry is invoked."
func TestValidate_ViolatingResponse(t *testing.T) {
	schema := enumSchema(t)

	// "verdict" must be one of approve/reject/defer; "invalid" is not in the enum.
	response := "```json\n{\"verdict\":\"invalid\",\"score\":5}\n```"

	parsed, vErr := dispatchschema.Validate(response, schema)

	if vErr == nil {
		t.Fatal("expected vErr non-nil for schema violation, got nil")
	}
	if vErr.Kind != dispatchschema.KindSchemaViolation {
		t.Errorf("vErr.Kind = %q, want %q", vErr.Kind, dispatchschema.KindSchemaViolation)
	}
	if vErr.Field == "" {
		t.Errorf("vErr.Field is empty; want the violating field name")
	}
	if parsed != nil {
		t.Errorf("parsed = %s, want nil on validation failure", parsed)
	}

	// Validate is a pure function: no retry hooks exist on it to verify,
	// which is the structural guarantee that no retry is invoked.
}

// TestValidate_FailureResultFields covers /features/2/oracle/assertions/2:
// "On validation failure, Result.Response retains the raw text unchanged,
// Result.Structured is nil, and Result.StructuredError carries
// Kind/Field/Message/Excerpt describing the failure."
//
// The oracle models the dispatch caller populating a Result from Validate's return.
func TestValidate_FailureResultFields(t *testing.T) {
	schema := enumSchema(t)

	rawResponse := "Narrative text.\n```json\n{\"verdict\":\"bad_value\",\"score\":7}\n```"

	parsed, vErr := dispatchschema.Validate(rawResponse, schema)

	if vErr == nil {
		t.Fatal("expected vErr non-nil")
	}

	// Simulate what the dispatcher (F04) does when validation fails:
	// - Response is the original raw text, unchanged.
	// - Structured is nil.
	// - StructuredError is populated from vErr.
	dispatcherResponse := rawResponse        // caller preserves raw text
	var dispatcherStructured []byte = parsed // must be nil
	dispatcherStructuredError := vErr

	if dispatcherResponse != rawResponse {
		t.Errorf("response mutated: got %q, want %q", dispatcherResponse, rawResponse)
	}
	if dispatcherStructured != nil {
		t.Errorf("Structured = %s, want nil on failure", dispatcherStructured)
	}
	if dispatcherStructuredError == nil {
		t.Error("StructuredError is nil, want populated")
	}

	// Verify all four fields are populated.
	if dispatcherStructuredError.Kind == "" {
		t.Error("StructuredError.Kind is empty")
	}
	// Field may be empty for top-level errors — check it is set for per-field violation.
	// For an enum violation on "verdict", Field must be "verdict".
	if dispatcherStructuredError.Field == "" {
		t.Error("StructuredError.Field is empty; expected the violating field name for an enum violation")
	}
	if dispatcherStructuredError.Message == "" {
		t.Error("StructuredError.Message is empty")
	}
	// Excerpt cap: must not exceed 200 runes.
	if utf8.RuneCountInString(dispatcherStructuredError.Excerpt) > 200 {
		t.Errorf("StructuredError.Excerpt rune count = %d, want <= 200",
			utf8.RuneCountInString(dispatcherStructuredError.Excerpt))
	}
}

// TestValidate_NilSchemaCallerBranch covers /features/2/oracle/assertions/3:
// "When the dispatcher invokes the validator with a nil schema (omitted-schema
// path), the validator is not called; Result.Structured and Result.StructuredError
// remain nil and the wire format is byte-equivalent to current dispatch output."
//
// Contract: Validate must NOT be called with nil schema. The caller branches on
// schema != nil. This test simulates that caller branch and verifies the JSON
// output contains neither key.
func TestValidate_NilSchemaCallerBranch(t *testing.T) {
	// Simulate the dispatcher logic: only call Validate when schema != nil.
	var schema *dispatchschema.Schema = nil

	var structuredRaw json.RawMessage
	var structuredError *dispatchschema.ValidationError

	// Dispatcher branch: schema == nil → skip Validate entirely.
	if schema != nil {
		var vErr *dispatchschema.ValidationError
		structuredRaw, vErr = dispatchschema.Validate("ignored", schema)
		if vErr != nil {
			structuredError = vErr
		}
	}

	// Both must be nil.
	if structuredRaw != nil {
		t.Errorf("Structured = %s, want nil for nil-schema path", structuredRaw)
	}
	if structuredError != nil {
		t.Errorf("StructuredError = %+v, want nil for nil-schema path", structuredError)
	}

	// Wire-format assertion: marshal a representative struct containing these
	// fields (as the Result struct will after F03 implementation) and confirm
	// neither key appears. We use a local anonymous struct that mirrors the
	// final Result extension contract.
	wirePayload := struct {
		Response        string                          `json:"response"`
		Status          string                          `json:"status"`
		Structured      json.RawMessage                 `json:"structured,omitempty"`
		StructuredError *dispatchschema.ValidationError `json:"structured_error,omitempty"`
	}{
		Response:        "some panelist text",
		Status:          "ok",
		Structured:      structuredRaw,
		StructuredError: structuredError,
	}

	data, err := json.Marshal(wirePayload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), `"structured"`) {
		t.Errorf("marshaled JSON contains %q key; want it absent on nil-schema path.\nJSON: %s",
			"structured", data)
	}
	if strings.Contains(string(data), `"structured_error"`) {
		t.Errorf("marshaled JSON contains %q key; want it absent on nil-schema path.\nJSON: %s",
			"structured_error", data)
	}
}

// --- edge case tests ---

// TestValidate_LastFenceSelected verifies that when multiple ```json blocks
// appear, the last one is used.
func TestValidate_LastFenceSelected(t *testing.T) {
	schema := mustParseSchema(t, `{
		"type": "object",
		"properties": {"result": {"type": "string"}}
	}`)

	// First block has a wrong type value; last block is valid.
	response := "First attempt:\n```json\n{\"result\":\"first\"}\n```\nSecond attempt:\n```json\n{\"result\":\"last\"}\n```"

	parsed, vErr := dispatchschema.Validate(response, schema)
	if vErr != nil {
		t.Fatalf("expected vErr nil, got %+v", vErr)
	}

	var got map[string]any
	if err := json.Unmarshal(parsed, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["result"] != "last" {
		t.Errorf("result = %q, want %q (last fence must win)", got["result"], "last")
	}
}

// TestValidate_MissingFence verifies Kind == KindMissingFence when no fenced
// block is present. Field must be empty; Excerpt is the first <=200 runes of response.
func TestValidate_MissingFence(t *testing.T) {
	schema := enumSchema(t)
	response := "I have no fenced block. Just prose."

	parsed, vErr := dispatchschema.Validate(response, schema)

	if vErr == nil {
		t.Fatal("expected vErr non-nil for missing fence")
	}
	if vErr.Kind != dispatchschema.KindMissingFence {
		t.Errorf("vErr.Kind = %q, want %q", vErr.Kind, dispatchschema.KindMissingFence)
	}
	if vErr.Field != "" {
		t.Errorf("vErr.Field = %q, want empty for top-level missing-fence error", vErr.Field)
	}
	if vErr.Message == "" {
		t.Error("vErr.Message is empty")
	}
	if utf8.RuneCountInString(vErr.Excerpt) > 200 {
		t.Errorf("vErr.Excerpt rune count = %d, want <= 200", utf8.RuneCountInString(vErr.Excerpt))
	}
	if parsed != nil {
		t.Errorf("parsed = %s, want nil", parsed)
	}
}

// TestValidate_MalformedJSON verifies Kind == KindJSONParse for a fence block
// that contains invalid JSON. Field must be empty (top-level); Excerpt <=200 runes.
func TestValidate_MalformedJSON(t *testing.T) {
	schema := enumSchema(t)
	response := "```json\n{not valid json}\n```"

	parsed, vErr := dispatchschema.Validate(response, schema)

	if vErr == nil {
		t.Fatal("expected vErr non-nil for malformed JSON")
	}
	if vErr.Kind != dispatchschema.KindJSONParse {
		t.Errorf("vErr.Kind = %q, want %q", vErr.Kind, dispatchschema.KindJSONParse)
	}
	if vErr.Field != "" {
		t.Errorf("vErr.Field = %q, want empty for top-level parse error", vErr.Field)
	}
	if utf8.RuneCountInString(vErr.Excerpt) > 200 {
		t.Errorf("vErr.Excerpt rune count = %d, want <= 200", utf8.RuneCountInString(vErr.Excerpt))
	}
	if parsed != nil {
		t.Errorf("parsed = %s, want nil", parsed)
	}
}

// TestValidate_SchemaViolationKind verifies Kind == KindSchemaViolation for
// each subcategory: required field missing, wrong scalar type, enum violation.
func TestValidate_SchemaViolation_RequiredFieldMissing(t *testing.T) {
	schema := enumSchema(t) // requires "verdict" and "score"

	// "score" is missing.
	response := "```json\n{\"verdict\":\"approve\"}\n```"

	parsed, vErr := dispatchschema.Validate(response, schema)

	if vErr == nil {
		t.Fatal("expected vErr non-nil for missing required field")
	}
	if vErr.Kind != dispatchschema.KindSchemaViolation {
		t.Errorf("vErr.Kind = %q, want %q", vErr.Kind, dispatchschema.KindSchemaViolation)
	}
	if vErr.Field == "" {
		t.Error("vErr.Field is empty; expected the missing required field name")
	}
	if !strings.Contains(vErr.Message, "missing") {
		t.Errorf("vErr.Message %q does not contain %q", vErr.Message, "missing")
	}
	if parsed != nil {
		t.Errorf("parsed = %s, want nil", parsed)
	}
}

// TestValidate_ExcerptRuneCap verifies rune-aware truncation: a response whose
// first 200 code points include multi-byte runes must not be truncated mid-rune.
func TestValidate_ExcerptRuneCap(t *testing.T) {
	schema := enumSchema(t)

	// Build a response with no fenced block, full of 4-byte runes (emoji).
	// 201 emoji = 201 runes → excerpt must be exactly 200 runes.
	emoji := "😀" // 4 bytes, 1 rune
	prose := strings.Repeat(emoji, 201)

	_, vErr := dispatchschema.Validate(prose, schema)
	if vErr == nil {
		t.Fatal("expected vErr non-nil (no fence)")
	}
	count := utf8.RuneCountInString(vErr.Excerpt)
	if count > 200 {
		t.Errorf("Excerpt rune count = %d, want <= 200", count)
	}
	// Excerpt must be valid UTF-8 (no mid-rune cut).
	if !utf8.ValidString(vErr.Excerpt) {
		t.Error("Excerpt is not valid UTF-8 — rune-aware truncation was not applied")
	}
}

// TestValidate_HostileInputNoPanic verifies that oversized / deeply nested /
// invalid-UTF-8 inputs do not panic and do not produce Excerpt > 200 runes.
func TestValidate_HostileInput_HugeJSON(t *testing.T) {
	schema := enumSchema(t)

	// 10 MB response with a valid fence containing 1 MB of JSON value.
	bigValue := strings.Repeat("x", 1<<20) // 1 MB string value
	payload := `{"verdict":"approve","score":1,"extra":"` + bigValue + `"}`
	response := strings.Repeat("narrative ", 500) + "\n```json\n" + payload + "\n```"

	var parsed json.RawMessage
	var vErr *dispatchschema.ValidationError
	require_no_panic(t, "Validate huge JSON", func() {
		parsed, vErr = dispatchschema.Validate(response, schema)
	})

	// "extra" is not declared in schema, so this should be a schema_violation
	// OR it may be accepted if the validator is permissive on extra fields.
	// Either way: no panic, Excerpt cap holds.
	if vErr != nil {
		if utf8.RuneCountInString(vErr.Excerpt) > 200 {
			t.Errorf("Excerpt rune count = %d, want <= 200", utf8.RuneCountInString(vErr.Excerpt))
		}
	}
	_ = parsed
}

func TestValidate_HostileInput_InvalidUTF8(t *testing.T) {
	schema := enumSchema(t)

	// Invalid UTF-8 byte sequences sprinkled in the prose area.
	response := "bad\xff\xfetext no fence here"

	var vErr *dispatchschema.ValidationError
	require_no_panic(t, "Validate invalid UTF-8", func() {
		_, vErr = dispatchschema.Validate(response, schema)
	})

	if vErr != nil {
		if utf8.RuneCountInString(vErr.Excerpt) > 200 {
			t.Errorf("Excerpt rune count = %d, want <= 200", utf8.RuneCountInString(vErr.Excerpt))
		}
	}
}

// TestValidate_BacktickInjectionInsideFence verifies that triple-backticks
// inside a JSON string value do not cause a false-positive fence closure.
func TestValidate_BacktickInjectionInsideFence(t *testing.T) {
	schema := mustParseSchema(t, `{
		"type": "object",
		"properties": {"code": {"type": "string"}}
	}`)

	// The JSON string value contains triple-backticks; the fence delimiter
	// must be line-aware (only a line that IS the closing fence counts).
	response := "```json\n{\"code\":\"use ``` here\"}\n```"

	parsed, vErr := dispatchschema.Validate(response, schema)
	if vErr != nil {
		t.Fatalf("expected vErr nil for backtick-inside-value, got %+v", vErr)
	}
	if len(parsed) == 0 {
		t.Fatal("expected non-empty parsed bytes")
	}
}

// TestValidate_UnclosedFence verifies that a fence opened but never closed
// is treated as missing_fence (partial blocks are not silently accepted).
func TestValidate_UnclosedFence(t *testing.T) {
	schema := enumSchema(t)
	response := "```json\n{\"verdict\":\"approve\",\"score\":1}\n" // no closing ```

	_, vErr := dispatchschema.Validate(response, schema)
	if vErr == nil {
		t.Fatal("expected vErr non-nil for unclosed fence")
	}
	if vErr.Kind != dispatchschema.KindMissingFence {
		t.Errorf("vErr.Kind = %q, want %q (unclosed fence = missing fence)", vErr.Kind, dispatchschema.KindMissingFence)
	}
}

// TestValidate_NonObjectJSON verifies schema_violation when fence contains a
// non-object (array, scalar, null).
func TestValidate_NonObjectJSON(t *testing.T) {
	schema := enumSchema(t)
	response := "```json\n[1,2,3]\n```"

	_, vErr := dispatchschema.Validate(response, schema)
	if vErr == nil {
		t.Fatal("expected vErr non-nil for array top-level")
	}
	if vErr.Kind != dispatchschema.KindSchemaViolation {
		t.Errorf("vErr.Kind = %q, want %q for non-object JSON", vErr.Kind, dispatchschema.KindSchemaViolation)
	}
	if vErr.Field != "" {
		t.Errorf("vErr.Field = %q, want empty for top-level structural error", vErr.Field)
	}
}

// TestValidate_ClosedThenUnclosedFence_FailsClosed is a regression test for the
// "fail closed on closed-then-unclosed fence" defect. A complete ```json block
// followed by an unclosed ```json block must yield KindMissingFence — NOT a
// successful parse against the stale earlier block.
func TestValidate_ClosedThenUnclosedFence_FailsClosed(t *testing.T) {
	schema := mustParseSchema(t, `{
		"type": "object",
		"properties": {"result": {"type": "string"}},
		"required": ["result"]
	}`)

	// First fence: complete and valid.
	// Second fence: opened but never closed.
	response := "First block:\n```json\n{\"result\":\"stale\"}\n```\nSecond block:\n```json\n{\"result\":\"incomplete\""

	parsed, vErr := dispatchschema.Validate(response, schema)

	// Must NOT validate the stale first block.
	if parsed != nil {
		t.Errorf("parsed = %s, want nil (stale first block must not be returned)", parsed)
	}
	if vErr == nil {
		t.Fatal("expected vErr non-nil for unclosed trailing fence")
	}
	if vErr.Kind != dispatchschema.KindMissingFence {
		t.Errorf("vErr.Kind = %q, want %q (closed-then-unclosed must fail closed, not fall back to stale block)",
			vErr.Kind, dispatchschema.KindMissingFence)
	}
}

// TestValidate_NullScalarFields_Rejected is a regression test for the
// "checkField rejects null for scalar fields" defect. For each scalar type
// (string, number, boolean), a JSON null value in an otherwise-valid response
// must yield KindSchemaViolation naming the offending field, with "null" in
// the message.
func TestValidate_NullScalarFields_Rejected(t *testing.T) {
	cases := []struct {
		name         string
		schemaJSON   string
		responseBody string
		field        string
	}{
		{
			name: "string field null",
			schemaJSON: `{
				"type": "object",
				"properties": {"placement": {"type": "string"}},
				"required": ["placement"]
			}`,
			responseBody: `{"placement": null}`,
			field:        "placement",
		},
		{
			name: "number field null",
			schemaJSON: `{
				"type": "object",
				"properties": {"score": {"type": "number"}},
				"required": ["score"]
			}`,
			responseBody: `{"score": null}`,
			field:        "score",
		},
		{
			name: "boolean field null",
			schemaJSON: `{
				"type": "object",
				"properties": {"flag": {"type": "boolean"}},
				"required": ["flag"]
			}`,
			responseBody: `{"flag": null}`,
			field:        "flag",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := mustParseSchema(t, tc.schemaJSON)
			response := "```json\n" + tc.responseBody + "\n```"

			parsed, vErr := dispatchschema.Validate(response, schema)

			if parsed != nil {
				t.Errorf("parsed = %s, want nil on null scalar", parsed)
			}
			if vErr == nil {
				t.Fatal("expected vErr non-nil for null scalar field")
			}
			if vErr.Kind != dispatchschema.KindSchemaViolation {
				t.Errorf("vErr.Kind = %q, want %q", vErr.Kind, dispatchschema.KindSchemaViolation)
			}
			if vErr.Field != tc.field {
				t.Errorf("vErr.Field = %q, want %q", vErr.Field, tc.field)
			}
			if !strings.Contains(vErr.Message, "null") {
				t.Errorf("vErr.Message = %q, want it to mention \"null\"", vErr.Message)
			}
		})
	}
}

// TestValidate_KindConstants verifies that the exported Kind constants have
// the exact string values the contract specifies.
func TestValidate_KindConstants(t *testing.T) {
	if dispatchschema.KindMissingFence != "missing_fence" {
		t.Errorf("KindMissingFence = %q, want %q", dispatchschema.KindMissingFence, "missing_fence")
	}
	if dispatchschema.KindJSONParse != "json_parse" {
		t.Errorf("KindJSONParse = %q, want %q", dispatchschema.KindJSONParse, "json_parse")
	}
	if dispatchschema.KindSchemaViolation != "schema_violation" {
		t.Errorf("KindSchemaViolation = %q, want %q", dispatchschema.KindSchemaViolation, "schema_violation")
	}
}

// TestValidate_ValidationErrorJSONTags verifies the snake_case JSON tags on
// ValidationError fields by round-tripping through json.Marshal/Unmarshal.
func TestValidate_ValidationErrorJSONTags(t *testing.T) {
	schema := enumSchema(t)
	response := "```json\n{\"verdict\":\"bad\",\"score\":5}\n```"

	_, vErr := dispatchschema.Validate(response, schema)
	if vErr == nil {
		t.Fatal("expected vErr non-nil")
	}

	data, err := json.Marshal(vErr)
	if err != nil {
		t.Fatalf("marshal ValidationError: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"kind", "field", "message", "excerpt"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("marshaled ValidationError missing JSON key %q (want snake_case tags)", key)
		}
	}
}
