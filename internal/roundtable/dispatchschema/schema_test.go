package dispatchschema_test

import (
	"encoding/json"
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
