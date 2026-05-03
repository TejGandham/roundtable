package main

// main_schema_test.go covers F12 byte-cap boundary behavior at the
// buildStdioDispatch call site (cmd/roundtable/main.go).
//
// buildStdioDispatch wraps dispatchschema.SafeParse and returns
// fmt.Errorf("invalid schema parameter: %w", err) on parse failure.
// These tests verify:
//   - The %w envelope preserves the *ParseError chain so errors.As succeeds.
//   - Over-cap input (65537 bytes) returns a typed bound error.
//   - At-cap input (65536 bytes) succeeds (nil error, non-nil schema path).
//
// Assertion traceability:
//   /features/0/oracle/assertions/3 → TestBuildStdioDispatch_OverByteCap_ReturnsTypedParseError,
//                                     TestBuildStdioDispatch_BoundaryByteCap_AtCap
//   /features/0/oracle/assertions/4 → TestBuildStdioDispatch_OverByteCap_ReturnsTypedParseError
//                                     (errors.As through %w envelope)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/TejGandham/roundtable/internal/roundtable/dispatchschema"
	"github.com/TejGandham/roundtable/internal/stdiomcp"
)

// mainByteCapSchemaRaw returns a json.RawMessage whose byte length is exactly
// targetBytes and represents a valid dispatchschema-lite JSON object.
// Schema structure: {"type":"object","properties":{"<name>":{"type":"string"}}}
// with <name> padded to fill the target length.
func mainByteCapSchemaRaw(targetBytes int) json.RawMessage {
	const prefix = `{"type":"object","properties":{"`
	const suffix = `":{"type":"string"}}}`
	overhead := len(prefix) + len(suffix)
	nameLen := targetBytes - overhead
	if nameLen < 1 {
		panic(fmt.Sprintf("mainByteCapSchemaRaw: targetBytes %d too small (overhead %d)", targetBytes, overhead))
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

// invokeDispatchWithSchema calls buildStdioDispatch using a minimal Config and
// no real backends, injecting the provided raw schema bytes as ToolInput.Schema.
// It returns the error from the dispatch closure (if any). The roundtable.Run
// call may fail for unrelated reasons (no backends configured), but schema
// parsing errors surface before roundtable.Run is invoked, so an "invalid
// schema parameter:" error unambiguously identifies the schema-parsing path.
func invokeDispatchWithSchema(t *testing.T, rawSchema json.RawMessage) error {
	t.Helper()

	cfg := stdiomcp.Config{
		ServerName:    "test",
		ServerVersion: "v0.0.0",
	}

	// No real backends — the dispatch closure returns errors before reaching
	// roundtable.Run when the schema is invalid. For valid schemas, Run will
	// fail because there are no backends; that error is NOT an "invalid schema
	// parameter:" error and is distinguishable.
	dispatch := buildStdioDispatch(nil, cfg)

	input := stdiomcp.ToolInput{
		Prompt: "test",
		Schema: rawSchema,
	}
	spec := stdiomcp.ToolSpec{Name: "roundtable-canvass"}

	_, err := dispatch(context.Background(), spec, input)
	return err
}

// TestBuildStdioDispatch_OverByteCap_ReturnsTypedParseError verifies that
// buildStdioDispatch returns an error wrapping *ParseError{KindBoundExceeded}
// when given a schema of MaxSchemaBytes+1 (65537) bytes. The error must be
// recoverable via errors.As through the fmt.Errorf("%w") envelope at main.go,
// which is the contract for /features/0/oracle/assertions/4.
// Covers /features/0/oracle/assertions/3 and /features/0/oracle/assertions/4.
func TestBuildStdioDispatch_OverByteCap_ReturnsTypedParseError(t *testing.T) {
	overCapRaw := mainByteCapSchemaRaw(dispatchschema.MaxSchemaBytes + 1)
	if len(overCapRaw) != dispatchschema.MaxSchemaBytes+1 {
		t.Fatalf("test fixture has wrong length: got %d, want %d", len(overCapRaw), dispatchschema.MaxSchemaBytes+1)
	}

	err := invokeDispatchWithSchema(t, overCapRaw)
	if err == nil {
		t.Fatalf("buildStdioDispatch returned nil error for over-cap schema; want typed bound error")
	}

	// The error must be "invalid schema parameter: ..." (not some other error).
	if !strings.HasPrefix(err.Error(), "invalid schema parameter:") {
		t.Errorf("error does not start with %q: %v", "invalid schema parameter:", err)
	}

	// The *ParseError must be recoverable through the %w chain.
	var pErr *dispatchschema.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("errors.As(*ParseError) failed through %%w envelope; got error type %T: %v", err, err)
	}
	if pErr.Kind != dispatchschema.KindBoundExceeded {
		t.Errorf("pErr.Kind = %q, want %q", pErr.Kind, dispatchschema.KindBoundExceeded)
	}
}

// TestBuildStdioDispatch_BoundaryByteCap_AtCap verifies that buildStdioDispatch
// does NOT return an "invalid schema parameter:" error for a schema of exactly
// MaxSchemaBytes (65536) bytes (the at-cap case must pass the byte cap).
// The dispatch may return a different error (no backends configured), but that
// error must not be a schema-parameter error.
// Covers /features/0/oracle/assertions/3.
func TestBuildStdioDispatch_BoundaryByteCap_AtCap(t *testing.T) {
	atCapRaw := mainByteCapSchemaRaw(dispatchschema.MaxSchemaBytes)
	if len(atCapRaw) != dispatchschema.MaxSchemaBytes {
		t.Fatalf("test fixture has wrong length: got %d, want %d", len(atCapRaw), dispatchschema.MaxSchemaBytes)
	}

	err := invokeDispatchWithSchema(t, atCapRaw)

	// The schema parse must succeed (no "invalid schema parameter:" error).
	// The dispatch may still fail for unrelated reasons (no backends).
	if err != nil && strings.HasPrefix(err.Error(), "invalid schema parameter:") {
		t.Errorf("buildStdioDispatch returned schema-parameter error for at-cap input (%d bytes): %v", dispatchschema.MaxSchemaBytes, err)
	}
}
