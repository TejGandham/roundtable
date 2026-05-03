package dispatchschema_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/TejGandham/roundtable/internal/roundtable/dispatchschema"
)

// TestParseScalarFields covers /features/0/oracle/assertions/0:
// "Parser accepts a schema object with typed scalar fields and returns a parsed schema value without error."
func TestParseScalarFields(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"deliverable":{"type":"string"},"score":{"type":"number"}}}`)

	schema, err := dispatchschema.Parse(raw)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if schema == nil {
		t.Fatal("Parse returned nil schema")
	}

	fields := schema.Fields()
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}

	byName := make(map[string]dispatchschema.Field)
	for _, f := range fields {
		byName[f.Name()] = f
	}

	deliverable, ok := byName["deliverable"]
	if !ok {
		t.Fatal("field 'deliverable' missing from parsed schema")
	}
	if deliverable.Type() != "string" {
		t.Errorf("deliverable.Type() = %q, want %q", deliverable.Type(), "string")
	}

	score, ok := byName["score"]
	if !ok {
		t.Fatal("field 'score' missing from parsed schema")
	}
	if score.Type() != "number" {
		t.Errorf("score.Type() = %q, want %q", score.Type(), "number")
	}
}

// TestParseEnumConstraints covers /features/0/oracle/assertions/1:
// "Parser accepts enum constraints on string fields per the feedback example shape and exposes the allowed values for downstream validation."
func TestParseEnumConstraints(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"placement":{"type":"string","enum":["a","b","c","d"]},"confidence":{"type":"string","enum":["low","med","high"]}}}`)

	schema, err := dispatchschema.Parse(raw)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if schema == nil {
		t.Fatal("Parse returned nil schema")
	}

	byName := make(map[string]dispatchschema.Field)
	for _, f := range schema.Fields() {
		byName[f.Name()] = f
	}

	placement, ok := byName["placement"]
	if !ok {
		t.Fatal("field 'placement' missing from parsed schema")
	}
	wantPlacement := []string{"a", "b", "c", "d"}
	if !stringSliceEqual(placement.Enum(), wantPlacement) {
		t.Errorf("placement.Enum() = %v, want %v", placement.Enum(), wantPlacement)
	}

	confidence, ok := byName["confidence"]
	if !ok {
		t.Fatal("field 'confidence' missing from parsed schema")
	}
	wantConfidence := []string{"low", "med", "high"}
	if !stringSliceEqual(confidence.Enum(), wantConfidence) {
		t.Errorf("confidence.Enum() = %v, want %v", confidence.Enum(), wantConfidence)
	}
}

// TestParseRequiredField verifies that the required array is accessible.
func TestParseRequiredField(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"number"}},"required":["name"]}`)

	schema, err := dispatchschema.Parse(raw)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	req := schema.Required()
	if len(req) != 1 || req[0] != "name" {
		t.Errorf("Required() = %v, want [name]", req)
	}
}

// TestParseEmptyProperties verifies that an empty properties object is accepted.
func TestParseEmptyProperties(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{}}`)
	schema, err := dispatchschema.Parse(raw)
	if err != nil {
		t.Fatalf("Parse returned unexpected error for empty properties: %v", err)
	}
	if schema == nil {
		t.Fatal("Parse returned nil schema")
	}
	if len(schema.Fields()) != 0 {
		t.Errorf("expected 0 fields, got %d", len(schema.Fields()))
	}
}

// TestParseRejectsUnsupportedSubset covers /features/0/oracle/assertions/2:
// "Parser rejects schema constructs outside the supported subset with a descriptive error."
func TestParseRejectsUnsupportedSubset(t *testing.T) {
	// format is a supported-subset violation; additionalProperties:true is another.
	cases := []struct {
		name    string
		raw     string
		wantErr string // substring expected in error message
	}{
		{
			name:    "top-level format keyword",
			raw:     `{"type":"object","properties":{"x":{"type":"string"}},"format":"email"}`,
			wantErr: "format",
		},
		{
			name:    "additionalProperties true",
			raw:     `{"type":"object","properties":{"x":{"type":"string"}},"additionalProperties":true}`,
			wantErr: "additionalProperties",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := dispatchschema.Parse(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestParseRejectionModes covers /features/0/oracle/assertions/3:
// "Parser rejects nested objects, arrays, $ref, anyOf/oneOf/allOf, format, and additionalProperties:true
// with errors that name the offending construct."
func TestParseRejectionModes(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string // substring expected in error message
	}{
		{
			name:    "nested-properties-object",
			raw:     `{"type":"object","properties":{"child":{"type":"object","properties":{"x":{"type":"string"}}}}}`,
			wantErr: "object",
		},
		{
			name:    "type-array",
			raw:     `{"type":"object","properties":{"items":{"type":"array"}}}`,
			wantErr: "array",
		},
		{
			name:    "$ref",
			raw:     `{"type":"object","properties":{"x":{"$ref":"#/definitions/Foo"}}}`,
			wantErr: "$ref",
		},
		{
			name:    "anyOf",
			raw:     `{"type":"object","anyOf":[{"properties":{"x":{"type":"string"}}}]}`,
			wantErr: "anyOf",
		},
		{
			name:    "oneOf",
			raw:     `{"type":"object","oneOf":[{"properties":{"x":{"type":"string"}}}]}`,
			wantErr: "oneOf",
		},
		{
			name:    "allOf",
			raw:     `{"type":"object","allOf":[{"properties":{"x":{"type":"string"}}}]}`,
			wantErr: "allOf",
		},
		{
			name:    "format",
			raw:     `{"type":"object","properties":{"email":{"type":"string","format":"email"}}}`,
			wantErr: "format",
		},
		{
			name:    "additionalProperties-true",
			raw:     `{"type":"object","properties":{"x":{"type":"string"}},"additionalProperties":true}`,
			wantErr: "additionalProperties",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := dispatchschema.Parse(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("case %q: expected error containing %q, got nil", tc.name, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("case %q: error = %q, want it to contain %q", tc.name, err.Error(), tc.wantErr)
			}
		})
	}
}

// TestParseEdgeCases covers the edge cases from the execution brief.
func TestParseEdgeCases(t *testing.T) {
	t.Run("missing-top-level-type", func(t *testing.T) {
		raw := json.RawMessage(`{"properties":{"x":{"type":"string"}}}`)
		_, err := dispatchschema.Parse(raw)
		if err == nil {
			t.Fatal("expected error for missing top-level type, got nil")
		}
	})

	t.Run("required-references-nonexistent-field", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["nonexistent"]}`)
		_, err := dispatchschema.Parse(raw)
		if err == nil {
			t.Fatal("expected error when required references field not in properties, got nil")
		}
		if !strings.Contains(err.Error(), "nonexistent") {
			t.Errorf("error = %q, want it to name the offending field %q", err.Error(), "nonexistent")
		}
	})

	t.Run("enum-on-non-string-field", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"object","properties":{"score":{"type":"number","enum":[1,2,3]}}}`)
		_, err := dispatchschema.Parse(raw)
		if err == nil {
			t.Fatal("expected error for enum on non-string field, got nil")
		}
	})

	t.Run("top-level-non-object-null", func(t *testing.T) {
		_, err := dispatchschema.Parse(json.RawMessage(`null`))
		if err == nil {
			t.Fatal("expected error for top-level null, got nil")
		}
	})

	t.Run("top-level-non-object-array", func(t *testing.T) {
		_, err := dispatchschema.Parse(json.RawMessage(`[{"type":"string"}]`))
		if err == nil {
			t.Fatal("expected error for top-level array, got nil")
		}
	})

	t.Run("top-level-non-object-scalar", func(t *testing.T) {
		_, err := dispatchschema.Parse(json.RawMessage(`"hello"`))
		if err == nil {
			t.Fatal("expected error for top-level string scalar, got nil")
		}
	})
}

// TestParseRobustness covers the safety-auditor concern:
// malformed/adversarial input must not panic; it must return an error.
func TestParseRobustness(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"truncated-json", `{"type":"object","properties":{"x":`},
		{"empty-input", ``},
		{"deeply-nested-array-in-enum", `{"type":"object","properties":{"x":{"type":"string","enum":[[[[[[[[[[[[[[[[[[[[1]]]]]]]]]]]]]]]]]]]]}}}`},
		{"null-property-value", `{"type":"object","properties":{"x":null}}`},
		{"number-as-property-value", `{"type":"object","properties":{"x":42}}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Must not panic. Error is expected but not required for all cases;
			// the critical invariant is no panic.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse panicked on malformed input %q: %v", tc.name, r)
				}
			}()
			dispatchschema.Parse(json.RawMessage(tc.raw)) //nolint:errcheck // panic guard only
		})
	}

	// Truncated JSON MUST return an error (not just not panic).
	t.Run("truncated-must-error", func(t *testing.T) {
		_, err := dispatchschema.Parse(json.RawMessage(`{"type":"object","properties":{"x":`))
		if err == nil {
			t.Fatal("expected error for truncated JSON, got nil")
		}
	})
}

// stringSliceEqual compares two string slices for equality (order-sensitive).
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- F12: count-cap boundary tests ---
// Assertion traceability:
//   /features/0/oracle/assertions/2 → TestParse_PropertiesCap_BoundaryAtCap,
//                                     TestParse_PropertiesCap_OverCapFails
//   /features/0/oracle/assertions/0 → TestParse_EnumCap_BoundaryAtCap,
//                                     TestParse_EnumCap_OverCapFails
//   /features/0/oracle/assertions/1 → TestParse_RequiredCap_BoundaryAtCap,
//                                     TestParse_RequiredCap_OverCapFails
//   /features/0/oracle/assertions/4 → all cap tests (errors.As discrimination,
//                                     no strings.Contains for Kind discrimination)
//   /features/0/oracle/assertions/4 → TestParse_KindMalformedPreservesCause
//                                     (Cause chain via errors.As)

// buildPropertiesSchema returns a JSON schema with n string-typed properties.
// Property names are "p0", "p1", ..., "p<n-1>".
func buildPropertiesSchema(n int) json.RawMessage {
	buf := []byte(`{"type":"object","properties":{`)
	for i := 0; i < n; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, fmt.Sprintf("%q:{\"type\":\"string\"}", fmt.Sprintf("p%d", i))...)
	}
	buf = append(buf, '}', '}')
	return json.RawMessage(buf)
}

// buildEnumSchema returns a JSON schema with a single string field "x" whose
// enum has n entries (values "v0", "v1", ..., "v<n-1>").
func buildEnumSchema(n int) json.RawMessage {
	buf := []byte(`{"type":"object","properties":{"x":{"type":"string","enum":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, fmt.Sprintf("%q", fmt.Sprintf("v%d", i))...)
	}
	buf = append(buf, ']', '}', '}', '}')
	return json.RawMessage(buf)
}

// buildRequiredSchema returns a JSON schema with n string-typed properties and
// a "required" array referencing all n of them. n must be ≤ MaxProperties so
// the properties cap does not fire before the required cap is tested.
func buildRequiredSchema(n int) json.RawMessage {
	buf := []byte(`{"type":"object","properties":{`)
	for i := 0; i < n; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, fmt.Sprintf("%q:{\"type\":\"string\"}", fmt.Sprintf("p%d", i))...)
	}
	buf = append(buf, '}', ',', '"', 'r', 'e', 'q', 'u', 'i', 'r', 'e', 'd', '"', ':')
	buf = append(buf, '[')
	for i := 0; i < n; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, fmt.Sprintf("%q", fmt.Sprintf("p%d", i))...)
	}
	buf = append(buf, ']', '}')
	return json.RawMessage(buf)
}

// TestParse_PropertiesCap_BoundaryAtCap verifies that a schema with exactly
// MaxProperties (256) properties is accepted without error.
// Covers /features/0/oracle/assertions/2.
func TestParse_PropertiesCap_BoundaryAtCap(t *testing.T) {
	raw := buildPropertiesSchema(dispatchschema.MaxProperties)
	schema, err := dispatchschema.Parse(raw)
	if err != nil {
		t.Fatalf("Parse with %d properties returned unexpected error: %v", dispatchschema.MaxProperties, err)
	}
	if schema == nil {
		t.Fatal("Parse returned nil schema")
	}
	if len(schema.Fields()) != dispatchschema.MaxProperties {
		t.Errorf("Fields() = %d, want %d", len(schema.Fields()), dispatchschema.MaxProperties)
	}
}

// TestParse_PropertiesCap_OverCapFails verifies that a schema with MaxProperties+1
// (257) properties returns a *ParseError with Kind == KindBoundExceeded and a
// message containing "257 (max 256)".
// Covers /features/0/oracle/assertions/2 and /features/0/oracle/assertions/4.
func TestParse_PropertiesCap_OverCapFails(t *testing.T) {
	raw := buildPropertiesSchema(dispatchschema.MaxProperties + 1)
	_, err := dispatchschema.Parse(raw)
	if err == nil {
		t.Fatalf("Parse with %d properties returned nil error; want *ParseError{KindBoundExceeded}", dispatchschema.MaxProperties+1)
	}

	var pErr *dispatchschema.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("errors.As(*ParseError) failed; got error type %T: %v", err, err)
	}
	if pErr.Kind != dispatchschema.KindBoundExceeded {
		t.Errorf("pErr.Kind = %q, want %q", pErr.Kind, dispatchschema.KindBoundExceeded)
	}
	if !strings.Contains(pErr.Message, "257 (max 256)") {
		t.Errorf("pErr.Message = %q, want it to contain %q", pErr.Message, "257 (max 256)")
	}
}

// TestParse_EnumCap_BoundaryAtCap verifies that a schema with exactly
// MaxEnumEntries (256) enum values on a string field is accepted.
// Covers /features/0/oracle/assertions/0.
func TestParse_EnumCap_BoundaryAtCap(t *testing.T) {
	raw := buildEnumSchema(dispatchschema.MaxEnumEntries)
	schema, err := dispatchschema.Parse(raw)
	if err != nil {
		t.Fatalf("Parse with %d enum entries returned unexpected error: %v", dispatchschema.MaxEnumEntries, err)
	}
	if schema == nil {
		t.Fatal("Parse returned nil schema")
	}
}

// TestParse_EnumCap_OverCapFails verifies that a schema with MaxEnumEntries+1
// (257) enum values on a string field returns a *ParseError with Kind ==
// KindBoundExceeded and a message containing "257 (max 256)".
// Covers /features/0/oracle/assertions/0 and /features/0/oracle/assertions/4.
func TestParse_EnumCap_OverCapFails(t *testing.T) {
	raw := buildEnumSchema(dispatchschema.MaxEnumEntries + 1)
	_, err := dispatchschema.Parse(raw)
	if err == nil {
		t.Fatalf("Parse with %d enum entries returned nil error; want *ParseError{KindBoundExceeded}", dispatchschema.MaxEnumEntries+1)
	}

	var pErr *dispatchschema.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("errors.As(*ParseError) failed; got error type %T: %v", err, err)
	}
	if pErr.Kind != dispatchschema.KindBoundExceeded {
		t.Errorf("pErr.Kind = %q, want %q", pErr.Kind, dispatchschema.KindBoundExceeded)
	}
	if !strings.Contains(pErr.Message, "257 (max 256)") {
		t.Errorf("pErr.Message = %q, want it to contain %q", pErr.Message, "257 (max 256)")
	}
}

// TestParse_RequiredCap_BoundaryAtCap verifies that a schema with exactly
// MaxRequiredEntries (256) required fields (and 256 matching properties) is
// accepted.
// Covers /features/0/oracle/assertions/1.
func TestParse_RequiredCap_BoundaryAtCap(t *testing.T) {
	// buildRequiredSchema uses n ≤ MaxProperties so the properties cap does
	// not fire before the required cap is tested.
	raw := buildRequiredSchema(dispatchschema.MaxRequiredEntries)
	schema, err := dispatchschema.Parse(raw)
	if err != nil {
		t.Fatalf("Parse with %d required entries returned unexpected error: %v", dispatchschema.MaxRequiredEntries, err)
	}
	if schema == nil {
		t.Fatal("Parse returned nil schema")
	}
	if len(schema.Required()) != dispatchschema.MaxRequiredEntries {
		t.Errorf("Required() = %d, want %d", len(schema.Required()), dispatchschema.MaxRequiredEntries)
	}
}

// TestParse_RequiredCap_OverCapFails verifies that a schema with
// MaxRequiredEntries+1 (257) required entries returns a *ParseError with Kind
// == KindBoundExceeded and a message containing "257 (max 256)".
// The schema has MaxRequiredEntries+1 properties so the properties cap
// (also 256) would fire first; we therefore use exactly 257 properties to
// trigger properties cap — but the required cap test must isolate the required
// cap, so we use 256 properties + a "required" array of 257 names where one
// name is fabricated to bypass the cross-ref check. Wait: the cross-ref check
// fires after the cap check, per the execution brief ("BEFORE cross-ref
// loop"). So we can use 257 names against the cap and it will fire before
// referential validation.
// NOTE: Because MaxRequiredEntries == MaxProperties == 256, and we need 257
// required entries, we must have ≥257 properties to satisfy cross-ref — which
// itself triggers the properties cap. So we build 256 properties but 257
// required names (one non-existent property name): the required cap fires
// before cross-ref validation per the design constraint.
// Covers /features/0/oracle/assertions/1 and /features/0/oracle/assertions/4.
func TestParse_RequiredCap_OverCapFails(t *testing.T) {
	// 256 properties + required array of 257 names (includes one non-existent
	// "overflow" name). Required cap fires before the cross-ref check.
	buf := []byte(`{"type":"object","properties":{`)
	for i := 0; i < dispatchschema.MaxRequiredEntries; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, fmt.Sprintf("%q:{\"type\":\"string\"}", fmt.Sprintf("p%d", i))...)
	}
	buf = append(buf, '}', ',', '"', 'r', 'e', 'q', 'u', 'i', 'r', 'e', 'd', '"', ':')
	buf = append(buf, '[')
	for i := 0; i <= dispatchschema.MaxRequiredEntries; i++ { // 257 entries: p0..p255 + "overflow"
		if i > 0 {
			buf = append(buf, ',')
		}
		if i < dispatchschema.MaxRequiredEntries {
			buf = append(buf, fmt.Sprintf("%q", fmt.Sprintf("p%d", i))...)
		} else {
			buf = append(buf, '"', 'o', 'v', 'e', 'r', 'f', 'l', 'o', 'w', '"')
		}
	}
	buf = append(buf, ']', '}')
	raw := json.RawMessage(buf)

	_, err := dispatchschema.Parse(raw)
	if err == nil {
		t.Fatalf("Parse with %d required entries returned nil error; want *ParseError{KindBoundExceeded}", dispatchschema.MaxRequiredEntries+1)
	}

	var pErr *dispatchschema.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("errors.As(*ParseError) failed; got error type %T: %v", err, err)
	}
	if pErr.Kind != dispatchschema.KindBoundExceeded {
		t.Errorf("pErr.Kind = %q, want %q", pErr.Kind, dispatchschema.KindBoundExceeded)
	}
	if !strings.Contains(pErr.Message, "257 (max 256)") {
		t.Errorf("pErr.Message = %q, want it to contain %q", pErr.Message, "257 (max 256)")
	}
}

// TestParse_KindMalformedPreservesCause verifies that Parse on truncated JSON
// returns a *ParseError with Kind == KindMalformed and that Cause contains the
// underlying json error recoverable via errors.As.
// Covers /features/0/oracle/assertions/4 (Unwrap chain / Cause field).
func TestParse_KindMalformedPreservesCause(t *testing.T) {
	_, err := dispatchschema.Parse(json.RawMessage(`{`))
	if err == nil {
		t.Fatal("Parse returned nil error for truncated JSON; want *ParseError{KindMalformed}")
	}

	var pErr *dispatchschema.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("errors.As(*ParseError) failed; got error type %T: %v", err, err)
	}
	if pErr.Kind != dispatchschema.KindMalformed {
		t.Errorf("pErr.Kind = %q, want %q", pErr.Kind, dispatchschema.KindMalformed)
	}

	// Cause must be non-nil and must wrap a json syntax/IO error.
	if pErr.Cause == nil {
		t.Fatal("pErr.Cause is nil; want inner json error preserved via Unwrap()")
	}

	// Verify errors.As can reach the inner json.SyntaxError through the
	// ParseError.Unwrap() chain (proves the %w wrapping is wired through).
	var syntaxErr *json.SyntaxError
	if !errors.As(pErr.Cause, &syntaxErr) {
		// Acceptable alternative: errors.As on the outer error also works.
		if !errors.As(err, &syntaxErr) {
			t.Errorf("errors.As(*json.SyntaxError) failed on both pErr.Cause and outer err; Cause type = %T, Cause = %v", pErr.Cause, pErr.Cause)
		}
	}
}

// --- F12: SafeParse direct tests ---
// Assertion traceability:
//   /features/0/oracle/assertions/3 → TestSafeParse_BytecapAtBoundary,
//                                     TestSafeParse_BytecapOverCapFails,
//                                     TestSafeParse_WhitespaceFloodDoS
//   /features/0/oracle/assertions/4 → TestSafeParse_BytecapOverCapFails (errors.As)

// safeParseBoundarySchema returns a valid schema JSON padded to exactly
// targetBytes bytes by using a property name long enough to reach the target.
// The schema is {"type":"object","properties":{"<name>":{"type":"string"}}}.
// prefix+suffix account for all JSON scaffolding around the property name.
func safeParseBoundarySchema(targetBytes int) json.RawMessage {
	// Scaffolding: {"type":"object","properties":{"<name>":{"type":"string"}}}
	// prefix before name: {"type":"object","properties":{"   = 32 bytes
	// suffix after name:  ":{"type":"string"}}}              = 21 bytes
	// total overhead: 53 bytes
	const prefix = `{"type":"object","properties":{"`
	const suffix = `":{"type":"string"}}}`
	overhead := len(prefix) + len(suffix)
	nameLen := targetBytes - overhead
	if nameLen < 1 {
		panic(fmt.Sprintf("safeParseBoundarySchema: targetBytes %d too small (overhead %d)", targetBytes, overhead))
	}
	name := make([]byte, nameLen)
	for i := range name {
		name[i] = 'a'
	}
	buf := make([]byte, 0, targetBytes)
	buf = append(buf, prefix...)
	buf = append(buf, name...)
	buf = append(buf, suffix...)
	return json.RawMessage(buf)
}

// TestSafeParse_BytecapAtBoundary verifies that SafeParse accepts a valid
// schema of exactly MaxSchemaBytes (65536) bytes.
// Covers /features/0/oracle/assertions/3.
func TestSafeParse_BytecapAtBoundary(t *testing.T) {
	raw := safeParseBoundarySchema(dispatchschema.MaxSchemaBytes)
	if len(raw) != dispatchschema.MaxSchemaBytes {
		t.Fatalf("test fixture has wrong length: got %d, want %d", len(raw), dispatchschema.MaxSchemaBytes)
	}
	schema, err := dispatchschema.SafeParse(raw)
	if err != nil {
		t.Fatalf("SafeParse with exactly %d bytes returned unexpected error: %v", dispatchschema.MaxSchemaBytes, err)
	}
	if schema == nil {
		t.Fatal("SafeParse returned nil schema for valid boundary input")
	}
}

// TestSafeParse_BytecapOverCapFails verifies that SafeParse rejects input of
// MaxSchemaBytes+1 (65537) bytes with a *ParseError{Kind: KindBoundExceeded}.
// Covers /features/0/oracle/assertions/3 and /features/0/oracle/assertions/4.
func TestSafeParse_BytecapOverCapFails(t *testing.T) {
	raw := safeParseBoundarySchema(dispatchschema.MaxSchemaBytes + 1)
	if len(raw) != dispatchschema.MaxSchemaBytes+1 {
		t.Fatalf("test fixture has wrong length: got %d, want %d", len(raw), dispatchschema.MaxSchemaBytes+1)
	}
	_, err := dispatchschema.SafeParse(raw)
	if err == nil {
		t.Fatalf("SafeParse with %d bytes returned nil error; want *ParseError{KindBoundExceeded}", dispatchschema.MaxSchemaBytes+1)
	}

	var pErr *dispatchschema.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("errors.As(*ParseError) failed; got error type %T: %v", err, err)
	}
	if pErr.Kind != dispatchschema.KindBoundExceeded {
		t.Errorf("pErr.Kind = %q, want %q", pErr.Kind, dispatchschema.KindBoundExceeded)
	}
}

// TestSafeParse_WhitespaceFloodDoS verifies that SafeParse measures the byte
// cap on the RAW (pre-trim) length. 65537 bytes of pure whitespace must return
// KindBoundExceeded and must never reach bytes.TrimSpace or Parse.
// This is the Gemini pre-trim DoS regression witness.
// Covers /features/0/oracle/assertions/3.
func TestSafeParse_WhitespaceFloodDoS(t *testing.T) {
	// MaxSchemaBytes+1 bytes of pure whitespace. Post-trim this is empty
	// (would be "no schema" = nil,nil). Pre-trim cap must fire first.
	flood := make([]byte, dispatchschema.MaxSchemaBytes+1)
	for i := range flood {
		flood[i] = ' '
	}
	_, err := dispatchschema.SafeParse(json.RawMessage(flood))
	if err == nil {
		t.Fatalf("SafeParse with %d whitespace bytes returned nil error; want *ParseError{KindBoundExceeded} (pre-trim cap must fire)", dispatchschema.MaxSchemaBytes+1)
	}

	var pErr *dispatchschema.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("errors.As(*ParseError) failed; got error type %T: %v", err, err)
	}
	if pErr.Kind != dispatchschema.KindBoundExceeded {
		t.Errorf("pErr.Kind = %q, want %q (whitespace flood must trigger pre-trim cap, not reach Parse)", pErr.Kind, dispatchschema.KindBoundExceeded)
	}
}

// TestSafeParse_WhitespaceUnderCapReturnsNilNil verifies that whitespace
// input under the byte cap is treated as "no schema" (nil, nil).
// Covers the null/empty/whitespace short-circuit in SafeParse.
func TestSafeParse_WhitespaceUnderCapReturnsNilNil(t *testing.T) {
	ws := make([]byte, 100)
	for i := range ws {
		ws[i] = ' '
	}
	schema, err := dispatchschema.SafeParse(json.RawMessage(ws))
	if err != nil {
		t.Fatalf("SafeParse(100 spaces) returned unexpected error: %v", err)
	}
	if schema != nil {
		t.Errorf("SafeParse(100 spaces) returned non-nil schema; want nil (no schema)")
	}
}

// TestSafeParse_NullReturnsNilNil verifies that []byte("null") is treated as
// "no schema" and returns (nil, nil).
func TestSafeParse_NullReturnsNilNil(t *testing.T) {
	schema, err := dispatchschema.SafeParse(json.RawMessage("null"))
	if err != nil {
		t.Fatalf("SafeParse(null) returned unexpected error: %v", err)
	}
	if schema != nil {
		t.Errorf("SafeParse(null) returned non-nil schema; want nil")
	}
}

// TestSafeParse_EmptyReturnsNilNil verifies that empty bytes are treated as
// "no schema" and return (nil, nil).
func TestSafeParse_EmptyReturnsNilNil(t *testing.T) {
	schema, err := dispatchschema.SafeParse(json.RawMessage(nil))
	if err != nil {
		t.Fatalf("SafeParse(nil) returned unexpected error: %v", err)
	}
	if schema != nil {
		t.Errorf("SafeParse(nil) returned non-nil schema; want nil")
	}
}
