package roundtable

import (
	"fmt"
	"os"
	"strings"
)

// FormatFileReferences builds the file reference section for a prompt.
// Returns empty string if filePaths is empty.
//
// For each path, os.Stat is called to get the file size. If the stat
// fails, the file is listed as "(unavailable)".
//
// Output format matches Elixir assembler.ex:
//
//	=== FILES ===
//	- path/to/file.go (1234 bytes)
//	- missing.go (unavailable)
//
//	Review the files listed above using your own tools to read their contents.
func FormatFileReferences(filePaths []string) string {
	if len(filePaths) == 0 {
		return ""
	}

	var refs []string
	for _, path := range filePaths {
		info, err := os.Stat(path)
		if err != nil {
			refs = append(refs, fmt.Sprintf("- %s (unavailable)", path))
		} else {
			refs = append(refs, fmt.Sprintf("- %s (%d bytes)", path, info.Size()))
		}
	}

	return "=== FILES ===\n" + strings.Join(refs, "\n") +
		"\n\nReview the files listed above using your own tools to read their contents."
}

// AssemblePrompt builds the full prompt string from a role prompt, user request,
// and optional file references. Matches the Elixir assembler.ex contract:
//
//	<trimmed role prompt>
//
//	=== REQUEST ===
//	<trimmed user request>
//
//	=== FILES ===
//	...
//
// Each section is separated by a blank line ("\n\n").
func AssemblePrompt(rolePrompt, userRequest string, filePaths []string) string {
	sections := []string{
		strings.TrimSpace(rolePrompt),
		"=== REQUEST ===\n" + strings.TrimSpace(userRequest),
	}

	fileRefs := FormatFileReferences(filePaths)
	if fileRefs != "" {
		sections = append(sections, fileRefs)
	}

	return strings.Join(sections, "\n\n")
}
