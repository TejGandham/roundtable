// Package dispatchschema parses a JSON-Schema-lite subset used by the
// dispatch structured-output path. The accepted subset is:
//
//   - top-level object with "type":"object" and a "properties" map
//   - each property is a typed scalar: "string", "number", or "boolean"
//   - optional "enum":[...] of strings on string-typed properties only
//   - optional top-level "required":[...] referencing declared properties
//
// All other JSON Schema constructs ($ref, anyOf/oneOf/allOf, format,
// additionalProperties:true, nested objects, arrays, ...) are rejected
// with an error that names the offending construct so callers can route
// on it.
package dispatchschema

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Schema is the parsed representation of a JSON-Schema-lite document.
// Field order matches the property order in the input JSON.
type Schema struct {
	fields   []Field
	required []string
}

// Field is a single parsed property of a Schema.
type Field struct {
	name string
	typ  string
	enum []string
}

// Fields returns the parsed fields in input order.
func (s *Schema) Fields() []Field {
	if s == nil {
		return nil
	}
	return s.fields
}

// Required returns the names of properties listed in the top-level
// "required" array, in input order.
func (s *Schema) Required() []string {
	if s == nil {
		return nil
	}
	return s.required
}

// Name returns the property name.
func (f Field) Name() string { return f.name }

// Type returns the declared scalar type ("string", "number", "boolean").
func (f Field) Type() string { return f.typ }

// Enum returns the allowed values for a string-typed enum field, or nil
// if the field has no enum constraint.
func (f Field) Enum() []string { return f.enum }

// allowedTopLevel is the closed set of keywords recognized at the top
// level of a schema document. Any other key triggers a descriptive
// rejection.
var allowedTopLevel = map[string]struct{}{
	"type":       {},
	"properties": {},
	"required":   {},
}

// allowedFieldKeys is the closed set of keywords recognized inside a
// property descriptor.
var allowedFieldKeys = map[string]struct{}{
	"type": {},
	"enum": {},
}

// allowedScalarTypes enumerates the property "type" values accepted by
// the lite subset.
var allowedScalarTypes = map[string]struct{}{
	"string":  {},
	"number":  {},
	"boolean": {},
}

// Parse decodes raw into a *Schema, rejecting any construct outside the
// supported lite subset. The returned error names the offending
// construct (keyword, field, or type) so callers can route on it.
func Parse(raw json.RawMessage) (*Schema, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("dispatchschema: empty input")
	}

	// Two-stage decode (mirrors internal/roundtable/result.go:80-101):
	// stage 1 is a permissive map[string]json.RawMessage so unknown
	// keywords are visible by key without committing to a struct shape.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		// Top-level value is not a JSON object (could be array, scalar,
		// null, or malformed). Distinguish with a descriptive error.
		return nil, classifyTopLevelDecodeErr(raw, err)
	}
	if top == nil {
		// JSON `null` decodes to a nil map without error.
		return nil, fmt.Errorf("dispatchschema: top-level value must be an object, got null")
	}

	// Reject any unknown / unsupported top-level keyword by name. This
	// covers $ref, anyOf, oneOf, allOf, format, additionalProperties,
	// definitions, etc., per the rejection-mode contract.
	for kw := range top {
		if _, ok := allowedTopLevel[kw]; !ok {
			return nil, fmt.Errorf("dispatchschema: unsupported keyword %q at top level", kw)
		}
	}

	// "type" is required and must be "object".
	rawType, ok := top["type"]
	if !ok {
		return nil, fmt.Errorf("dispatchschema: missing top-level %q keyword", "type")
	}
	var topType string
	if err := json.Unmarshal(rawType, &topType); err != nil {
		return nil, fmt.Errorf("dispatchschema: top-level %q must be a string: %w", "type", err)
	}
	if topType != "object" {
		return nil, fmt.Errorf("dispatchschema: top-level type must be %q, got %q", "object", topType)
	}

	// "properties" is required (we accept empty {} but require the key).
	rawProps, ok := top["properties"]
	if !ok {
		return nil, fmt.Errorf("dispatchschema: missing top-level %q keyword", "properties")
	}

	fields, err := parseProperties(rawProps)
	if err != nil {
		return nil, err
	}

	schema := &Schema{fields: fields}

	if rawReq, ok := top["required"]; ok {
		req, err := parseRequired(rawReq, fields)
		if err != nil {
			return nil, err
		}
		schema.required = req
	}

	return schema, nil
}

// parseProperties decodes the top-level "properties" object and validates
// each declared field against the lite subset.
func parseProperties(raw json.RawMessage) ([]Field, error) {
	// Preserve declaration order by walking the raw bytes with a
	// streaming Decoder. encoding/json's map decoder discards order.
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("dispatchschema: %q must be an object: %w", "properties", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("dispatchschema: %q must be an object", "properties")
	}

	var fields []Field
	for dec.More() {
		nameTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("dispatchschema: malformed %q object: %w", "properties", err)
		}
		name, ok := nameTok.(string)
		if !ok {
			return nil, fmt.Errorf("dispatchschema: malformed %q object key", "properties")
		}

		var body json.RawMessage
		if err := dec.Decode(&body); err != nil {
			return nil, fmt.Errorf("dispatchschema: malformed value for field %q: %w", name, err)
		}

		field, err := parseField(name, body)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}

	// Consume closing '}'. Errors here only matter for malformed JSON,
	// which streaming decoder would have surfaced earlier.
	_, _ = dec.Token()
	return fields, nil
}

// parseField validates a single property descriptor.
func parseField(name string, raw json.RawMessage) (Field, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Field{}, fmt.Errorf("dispatchschema: field %q has null or empty descriptor", name)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return Field{}, fmt.Errorf("dispatchschema: field %q descriptor must be an object: %w", name, err)
	}
	if body == nil {
		return Field{}, fmt.Errorf("dispatchschema: field %q descriptor must be an object, got null", name)
	}

	// Ordering note: when "type" is present, validate it first so that
	// unsupported types ("object", "array") surface with the type name
	// in the error. Otherwise, type-specific follow-up keywords (e.g.
	// "properties" alongside "type":"object") would be reported as
	// "unsupported keyword" first and the test assertion that names the
	// type ("object", "array") would never be satisfied.
	if rawType, ok := body["type"]; ok {
		var typ string
		if err := json.Unmarshal(rawType, &typ); err != nil {
			return Field{}, fmt.Errorf("dispatchschema: field %q has non-string %q: %w", name, "type", err)
		}
		if _, ok := allowedScalarTypes[typ]; !ok {
			return Field{}, fmt.Errorf("dispatchschema: field %q has unsupported type %q", name, typ)
		}
	}

	// Reject any keyword inside the field that isn't in the lite subset.
	// This is where $ref, format, anyOf-on-property, etc. surface — and
	// for fields without "type" (e.g. {"$ref":"..."}), this is the only
	// signal we have, so the error names the offending keyword verbatim.
	for kw := range body {
		if _, ok := allowedFieldKeys[kw]; !ok {
			return Field{}, fmt.Errorf("dispatchschema: unsupported keyword %q at field %q", kw, name)
		}
	}

	// "type" is required for accepted fields.
	rawType, ok := body["type"]
	if !ok {
		return Field{}, fmt.Errorf("dispatchschema: field %q missing %q keyword", name, "type")
	}
	var typ string
	if err := json.Unmarshal(rawType, &typ); err != nil {
		return Field{}, fmt.Errorf("dispatchschema: field %q has non-string %q: %w", name, "type", err)
	}

	field := Field{name: name, typ: typ}

	if rawEnum, ok := body["enum"]; ok {
		if typ != "string" {
			return Field{}, fmt.Errorf("dispatchschema: field %q has enum on non-string type %q", name, typ)
		}
		var values []string
		if err := json.Unmarshal(rawEnum, &values); err != nil {
			return Field{}, fmt.Errorf("dispatchschema: field %q enum must be a string array: %w", name, err)
		}
		field.enum = values
	}

	return field, nil
}

// parseRequired decodes the top-level "required" array and confirms
// every named field is declared in properties.
func parseRequired(raw json.RawMessage, fields []Field) ([]string, error) {
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, fmt.Errorf("dispatchschema: %q must be a string array: %w", "required", err)
	}
	declared := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		declared[f.name] = struct{}{}
	}
	for _, n := range names {
		if _, ok := declared[n]; !ok {
			return nil, fmt.Errorf("dispatchschema: %q references field %q not declared in properties", "required", n)
		}
	}
	return names, nil
}

// classifyTopLevelDecodeErr returns a descriptive error for a top-level
// decode failure. encoding/json reports type mismatches via
// *json.UnmarshalTypeError; we use that to name the actual JSON kind
// (array, string, number, ...) so the error is useful to callers.
func classifyTopLevelDecodeErr(raw json.RawMessage, err error) error {
	var typeErr *json.UnmarshalTypeError
	if errAs(err, &typeErr) {
		return fmt.Errorf("dispatchschema: top-level value must be an object, got %s", typeErr.Value)
	}
	return fmt.Errorf("dispatchschema: malformed input: %w", err)
}

// errAs is a tiny wrapper so we don't import "errors" just for As; keeps
// the import set aligned with the brief (encoding/json, fmt, strings).
// We accept errors.As-style behaviour by attempting a single type
// assertion — sufficient because encoding/json returns the concrete
// *UnmarshalTypeError directly (no wrapping).
func errAs(err error, target **json.UnmarshalTypeError) bool {
	if e, ok := err.(*json.UnmarshalTypeError); ok {
		*target = e
		return true
	}
	return false
}
