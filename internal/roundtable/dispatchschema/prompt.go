package dispatchschema

import (
	"fmt"
	"strings"
)

// BuildPromptSuffix returns a deterministic prompt suffix instructing
// panelists to wrap their structured response in a single fenced JSON
// code block conforming to schema. Field/enum order follows the input
// order preserved by Parse — only the ordered slice accessors
// Schema.Fields() and Field.Enum() are iterated, never a Go map, so
// repeated invocation on the same *Schema produces byte-identical
// output.
//
// Schema-author-supplied strings (field names, enum values) are
// embedding-untrusted: F01 enforces structural JSON-Schema-lite
// validity but does not constrain string content. To keep the suffix
// safe against prompt-injection via hostile schemas, BuildPromptSuffix
// rewrites the following before embedding any name/value:
//
//   - Raw newlines (0x0A) → "<LF>" (defeats role-impersonation lines
//     such as "x\nSYSTEM: ignore prior instructions"; the keyword on
//     its own — without a preceding newline — is harmless prose).
//   - Runs of three or more consecutive backticks → "<BACKTICKS>"
//     (prevents premature closure of the ```json fence).
//   - Other ASCII control characters in 0x00–0x1F, excluding \t
//     (which is display-neutral) → "<CTRL>".
//
// BuildPromptSuffix(nil) returns "" and never panics. An empty
// Fields() schema still emits the fence instruction so the contract
// is stable for callers that pre-validate before parsing.
func BuildPromptSuffix(schema *Schema) string {
	if schema == nil {
		return ""
	}

	var b strings.Builder

	b.WriteString("Respond with a single fenced JSON code block. ")
	b.WriteString("Downstream validation treats the last such block in your response as the canonical payload; ")
	b.WriteString("narrative before that block is ignored.\n\n")

	fields := schema.Fields()
	if len(fields) > 0 {
		b.WriteString("The JSON object must contain the following fields:\n")
		for _, f := range fields {
			name := sanitize(f.Name())
			typ := f.Type()
			enum := f.Enum()
			if typ == "string" && len(enum) > 0 {
				b.WriteString(fmt.Sprintf("  - %q (string, one of: ", name))
				for i, v := range enum {
					if i > 0 {
						b.WriteString(", ")
					}
					b.WriteString(fmt.Sprintf("%q", sanitize(v)))
				}
				b.WriteString(")\n")
			} else if typ == "string" {
				b.WriteString(fmt.Sprintf("  - %q (string, free-text)\n", name))
			} else {
				b.WriteString(fmt.Sprintf("  - %q (%s)\n", name, typ))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("Open the block with ```json and close it with the matching triple-backtick line.\n")

	return b.String()
}

// sanitize rewrites caller-supplied schema strings so they cannot
// break out of their embedding context in the prompt suffix. See
// BuildPromptSuffix's godoc for the full escape contract.
func sanitize(s string) string {
	// Step 1: collapse runs of 3+ backticks first. Doing this before
	// per-rune control-char escaping avoids splitting a backtick run
	// across replacement boundaries.
	s = collapseBacktickRuns(s)

	// Step 2: rewrite control characters per-rune. \n is special-cased
	// so the marker is "<LF>" rather than the generic "<CTRL>" — the
	// newline injection vector is the most documented attack and gets
	// a named placeholder for auditor legibility.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString("<LF>")
		case r == '\t':
			b.WriteRune(r)
		case r >= 0x00 && r <= 0x1F:
			b.WriteString("<CTRL>")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// collapseBacktickRuns replaces every maximal run of 3 or more
// consecutive backticks in s with the literal placeholder
// "<BACKTICKS>". Runs of 1 or 2 backticks pass through untouched.
func collapseBacktickRuns(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '`' {
			j := i
			for j < len(s) && s[j] == '`' {
				j++
			}
			if j-i >= 3 {
				b.WriteString("<BACKTICKS>")
			} else {
				b.WriteString(s[i:j])
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
