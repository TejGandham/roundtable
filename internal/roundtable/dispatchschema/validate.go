// This file is part of package dispatchschema (see schema.go for the package
// godoc). Validate enforces the lite-subset contract on a panelist response.
//
// Contract summary:
//
//   - Validate(response, schema) extracts the LAST fenced JSON block in
//     response, JSON-decodes it, and validates it against schema.
//   - Returns either (parsed, nil) on success or (nil, *ValidationError) on
//     any failure. Never panics on hostile input.
//   - Stdlib-only: encoding/json, fmt, strings, unicode/utf8.
//   - No retry, fallback, or auto-repair logic. The PRD's no_retry clause is
//     explicit — a single failure is terminal for the validation step.
//   - The caller MUST branch on schema != nil before calling. Validate is not
//     a nil-schema short-circuit; passing a nil schema is undefined.
//   - Discipline guardrail: never returns json.RawMessage("null") as parsed.
//     A literal-null parse result is surfaced as KindSchemaViolation with
//     parsed == nil, so omitempty on Result.Structured stays meaningful.
package dispatchschema

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Kind constants identify the failure category of a ValidationError. They are
// untyped string constants so callers can compare ValidationError.Kind ==
// dispatchschema.KindMissingFence without a typed-constant indirection.
const (
	// KindMissingFence — no closed ```json fenced block was found in the
	// response. An opened-but-unclosed fence falls into this category too:
	// partial blocks are not silently accepted.
	KindMissingFence = "missing_fence"

	// KindJSONParse — a fenced block was found but its body is not valid
	// JSON. Field is "" (top-level). Excerpt is the offending body, capped
	// at 200 runes.
	KindJSONParse = "json_parse"

	// KindSchemaViolation — the fenced block parsed as JSON but does not
	// conform to schema (wrong top-level kind, missing required field,
	// wrong scalar type, or enum violation). Field names the violating
	// path; Excerpt is the offending value.
	KindSchemaViolation = "schema_violation"
)

// ValidationError describes a single per-panelist validation failure. It is
// surfaced verbatim on Result.StructuredError (see internal/roundtable/result.go).
//
// Field-path grammar (forward-compatible with F04+ extensions):
//
//   - Empty string ("") denotes a top-level structural error (e.g. missing
//     fence, JSON parse failure, or non-object payload).
//   - A bare segment names a top-level property (e.g. "verdict").
//   - Nested objects use a literal '.' separator: "outer.inner".
//   - Array indices use bracket notation with integer indices:
//     "items[0]" or "items[3].name".
//   - Literal '.', '[', or ']' inside a property name is backslash-escaped:
//     "weird\.key" denotes a top-level property whose name contains a dot.
//
// F03 only emits "" or a bare top-level field name (the F01 schema lite
// subset has no nested objects or arrays). The grammar above is documented
// here for forward-compat so F04+ can extend without re-litigating syntax.
//
// Excerpt MAY contain sensitive panelist content (the offending substring is
// passed through, capped at 200 runes; no automated redaction is performed).
// The same egress policy that applies to Result.Response applies to Excerpt.
type ValidationError struct {
	// Kind is one of KindMissingFence, KindJSONParse, KindSchemaViolation.
	// Bare string (not a typed alias) so callers can compare against the
	// exported untyped constants without a cast.
	Kind string `json:"kind"`

	// Field is the dotted path identifying the violation. Empty for
	// top-level errors. See the type-level godoc for grammar details.
	Field string `json:"field"`

	// Message is a human-readable description of the failure.
	Message string `json:"message"`

	// Excerpt is up to 200 runes (utf8-aware) of the offending text.
	// May contain sensitive content — apply the same egress policy as
	// Result.Response.
	Excerpt string `json:"excerpt"`
}

// excerptRuneCap is the maximum rune length of ValidationError.Excerpt.
const excerptRuneCap = 200

// Validate locates the last ```json fenced block in response, JSON-decodes it,
// and validates the decoded value against schema. On success it returns the
// parsed JSON bytes (the canonical block contents) and a nil error. On any
// failure it returns nil bytes and a non-nil *ValidationError describing the
// failure category, field path, message, and a capped excerpt of the
// offending substring.
//
// Callers MUST branch on schema != nil before calling. Passing a nil schema
// is undefined; the omitted-schema dispatch path skips Validate entirely so
// Result.Structured and Result.StructuredError remain nil and elide via
// omitempty.
//
// Validate is a pure function — no retry, no fallback prompt, no auto-repair.
// A single failure is terminal for the validation step.
func Validate(response string, schema *Schema) (json.RawMessage, *ValidationError) {
	body, ok := lastFencedJSON(response)
	if !ok {
		return nil, &ValidationError{
			Kind:    KindMissingFence,
			Field:   "",
			Message: "no fenced JSON block found in response",
			Excerpt: capRunes(response, excerptRuneCap),
		}
	}

	// Stage-1 decode: try to JSON-parse the body. We use json.Valid first
	// to avoid building an interface-tree for malformed input, but still
	// fall through to Unmarshal for the actual parsed shape.
	if !json.Valid([]byte(body)) {
		return nil, &ValidationError{
			Kind:    KindJSONParse,
			Field:   "",
			Message: "fenced block body is not valid JSON",
			Excerpt: capRunes(body, excerptRuneCap),
		}
	}

	// The body must be a JSON object — F01 schema subset is type:"object".
	// Use a permissive map[string]json.RawMessage decode so non-object
	// top-levels can be classified by JSON kind.
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		// Body parsed as JSON but not as a map — array, scalar, or null.
		// Distinguish kind for the error message.
		kind := topLevelKind([]byte(body))
		return nil, &ValidationError{
			Kind:    KindSchemaViolation,
			Field:   "",
			Message: fmt.Sprintf("top-level JSON value must be an object, got %s", kind),
			Excerpt: capRunes(body, excerptRuneCap),
		}
	}
	// JSON `null` decodes to a nil map without error. Treat as schema
	// violation — discipline guardrail prevents returning literal null
	// as parsed.
	if top == nil {
		return nil, &ValidationError{
			Kind:    KindSchemaViolation,
			Field:   "",
			Message: "top-level JSON value must be an object, got null",
			Excerpt: capRunes(body, excerptRuneCap),
		}
	}

	// Required-field check: every name in schema.Required() must be a key
	// in the parsed object.
	for _, name := range schema.Required() {
		if _, present := top[name]; !present {
			return nil, &ValidationError{
				Kind:    KindSchemaViolation,
				Field:   name,
				Message: fmt.Sprintf("required field %q missing", name),
				Excerpt: capRunes(body, excerptRuneCap),
			}
		}
	}

	// Per-field type and enum checks. Iterate fields in declaration order
	// so error reporting is deterministic.
	for _, field := range schema.Fields() {
		raw, present := top[field.Name()]
		if !present {
			// Optional field absent — that's fine; required-field check
			// above caught the cases that matter.
			continue
		}
		if vErr := checkField(field, raw); vErr != nil {
			return nil, vErr
		}
	}

	// Success path: return canonical bytes from the body. Use a re-marshal
	// of the top map to guarantee consistent encoding, OR just return the
	// trimmed body bytes. Returning the original body bytes preserves the
	// authored shape (whitespace inside string values, key order). Tests
	// re-marshal both sides before comparing, so we return body verbatim.
	return json.RawMessage([]byte(body)), nil
}

// checkField validates a single property's raw bytes against its declared
// type and (optional) enum constraint.
func checkField(field Field, raw json.RawMessage) *ValidationError {
	// Reject JSON null (and empty raw) for any scalar type before the type
	// switch. json.Unmarshal happily accepts "null" into string/float64/bool
	// receivers as the zero value, which would silently coerce {"x": null}
	// into legitimate-looking ""/0/false. Surface as schema violation so
	// the caller distinguishes "field present, wrong shape" from "field
	// present, correct type."
	if len(raw) == 0 || string(raw) == "null" {
		return &ValidationError{
			Kind:    KindSchemaViolation,
			Field:   field.Name(),
			Message: fmt.Sprintf("expected %s, got null", field.Type()),
			Excerpt: capRunes(string(raw), excerptRuneCap),
		}
	}
	switch field.Type() {
	case "string":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return &ValidationError{
				Kind:    KindSchemaViolation,
				Field:   field.Name(),
				Message: fmt.Sprintf("expected string, got %s", jsonKind(raw)),
				Excerpt: capRunes(string(raw), excerptRuneCap),
			}
		}
		if enum := field.Enum(); len(enum) > 0 {
			for _, allowed := range enum {
				if s == allowed {
					return nil
				}
			}
			return &ValidationError{
				Kind:    KindSchemaViolation,
				Field:   field.Name(),
				Message: fmt.Sprintf("value not in enum for field %q", field.Name()),
				Excerpt: capRunes(s, excerptRuneCap),
			}
		}
	case "number":
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil {
			return &ValidationError{
				Kind:    KindSchemaViolation,
				Field:   field.Name(),
				Message: fmt.Sprintf("expected number, got %s", jsonKind(raw)),
				Excerpt: capRunes(string(raw), excerptRuneCap),
			}
		}
	case "boolean":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return &ValidationError{
				Kind:    KindSchemaViolation,
				Field:   field.Name(),
				Message: fmt.Sprintf("expected boolean, got %s", jsonKind(raw)),
				Excerpt: capRunes(string(raw), excerptRuneCap),
			}
		}
	}
	return nil
}

// lastFencedJSON returns the body of the LAST ```json fenced block in
// response. The fence delimiters must be whole lines (optionally surrounded
// by whitespace) — a backtick sequence inside a JSON string value does not
// false-positive as a fence closure.
//
// A line that, after trimming leading/trailing whitespace, equals "```json"
// opens a block. The next line that, after trimming whitespace, equals "```"
// closes it. Anything else is body. If the last opening fence has no
// matching close, ok is false (treat as missing fence).
func lastFencedJSON(response string) (body string, ok bool) {
	lines := strings.Split(response, "\n")

	type span struct {
		start, end int // line indices into lines (start = first body line, end = exclusive)
	}
	var blocks []span
	openIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if openIdx < 0 {
			if trimmed == "```json" {
				openIdx = i + 1 // body starts on the next line
			}
			continue
		}
		// Inside an open block: only a bare ``` line closes it.
		if trimmed == "```" {
			blocks = append(blocks, span{start: openIdx, end: i})
			openIdx = -1
		}
	}
	// If a fence opened but never closed before EOF, fail closed — do NOT
	// fall back to the previous closed block. An attacker could emit a
	// stale, complete answer followed by an unclosed fence containing their
	// real (but truncated) final answer; falling back would validate the
	// stale block. Per the package contract, partial blocks are not
	// silently accepted.
	if openIdx >= 0 {
		return "", false
	}
	if len(blocks) == 0 {
		return "", false
	}
	last := blocks[len(blocks)-1]
	// Reassemble body lines with newline separators (preserve original
	// authored shape). Trailing newline omitted.
	body = strings.Join(lines[last.start:last.end], "\n")
	return body, true
}

// capRunes returns the first n runes of s, decoding rune-by-rune so the
// result is always valid UTF-8 even when s contains invalid byte sequences.
// If s has fewer than n runes, s is returned (potentially with invalid bytes
// repaired to U+FFFD via the standard rune-decoding rules — actually no,
// utf8 decoding does not auto-repair; we re-encode via runes).
func capRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	// Fast path: if s is short enough by bytes (each rune is ≥1 byte),
	// rune count cannot exceed n.
	if len(s) <= n {
		// Ensure valid UTF-8 — invalid sequences are replaced with the
		// replacement rune so Excerpt is always valid UTF-8 per contract.
		if utf8.ValidString(s) {
			return s
		}
		return sanitizeUTF8(s)
	}
	// Walk runes up to n; bail when we hit the cap.
	count := 0
	end := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid byte — skip it (it won't be in the output) but
			// don't count it toward the rune cap.
			i++
			continue
		}
		count++
		end = i + size
		if count >= n {
			i = end
			// We've consumed n valid runes; check if there's more.
			if end >= len(s) {
				break
			}
			// Cap reached — return prefix of s up to end, but only if
			// it's all-valid UTF-8. If not, sanitize.
			prefix := s[:end]
			if utf8.ValidString(prefix) {
				return prefix
			}
			return sanitizeUTF8(prefix)
		}
		i += size
	}
	// Loop finished — we counted < n valid runes, but s had invalid bytes
	// we skipped. Return only the valid-decoded portion.
	if end == len(s) && utf8.ValidString(s) {
		return s
	}
	return sanitizeUTF8(s)
}

// sanitizeUTF8 returns a copy of s with invalid byte sequences replaced by
// U+FFFD. Keeps Excerpt always-valid-UTF-8 even when input has stray bytes.
func sanitizeUTF8(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
			i++
			continue
		}
		b.WriteRune(r)
		i += size
	}
	// Final rune count may exceed cap if input had many invalid bytes
	// converted to RuneError; tighten by re-walking with rune cap.
	if utf8.RuneCountInString(b.String()) <= excerptRuneCap {
		return b.String()
	}
	// Re-cap by runes.
	out := b.String()
	count := 0
	for i := range out {
		count++
		if count > excerptRuneCap {
			return out[:i]
		}
	}
	return out
}

// topLevelKind returns the JSON kind name of a top-level value: "array",
// "string", "number", "boolean", or "null". Used in error messages for
// non-object payloads.
func topLevelKind(raw []byte) string {
	// Skip leading whitespace.
	i := 0
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) {
		return "empty"
	}
	switch raw[i] {
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	case '{':
		return "object"
	default:
		// '-' or digit → number; anything else falls through.
		return "number"
	}
}

// jsonKind returns the kind name of a non-empty raw JSON value. Same value
// set as topLevelKind but tolerant of leading whitespace inside object
// values (json.Unmarshal already consumed whitespace).
func jsonKind(raw json.RawMessage) string {
	return topLevelKind([]byte(raw))
}
