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
// blob suitable for prepending to a user message. Format:
//
//	<file path="X">
//	<contents...>
//	</file>
//
// Oversized files are truncated with <truncated /> inside the block. Files
// beyond the aggregate budget are listed by path inside <skipped-files>.
// Unreadable files emit self-closing <file path="X" error="..." /> tags so
// the model sees failures rather than silent omission.
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
