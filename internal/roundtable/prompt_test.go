package roundtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatFileReferences_Empty(t *testing.T) {
	result := FormatFileReferences(nil)
	if result != "" {
		t.Errorf("expected empty string for nil paths, got: %q", result)
	}

	result = FormatFileReferences([]string{})
	if result != "" {
		t.Errorf("expected empty string for empty paths, got: %q", result)
	}
}

func TestFormatFileReferences_ExistingFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.go")
	f2 := filepath.Join(dir, "b.go")
	if err := os.WriteFile(f1, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("world!"), 0644); err != nil {
		t.Fatal(err)
	}

	result := FormatFileReferences([]string{f1, f2})

	if !strings.HasPrefix(result, "=== FILES ===\n") {
		t.Errorf("expected FILES header, got: %q", result[:min(len(result), 40)])
	}
	if !strings.Contains(result, f1+" (5 bytes)") {
		t.Errorf("expected file a.go with 5 bytes, got: %q", result)
	}
	if !strings.Contains(result, f2+" (6 bytes)") {
		t.Errorf("expected file b.go with 6 bytes, got: %q", result)
	}
	if !strings.Contains(result, "Review the files listed above") {
		t.Errorf("expected review instruction, got: %q", result)
	}
}

func TestFormatFileReferences_MissingFile(t *testing.T) {
	result := FormatFileReferences([]string{"/nonexistent/file.go"})
	if !strings.Contains(result, "(unavailable)") {
		t.Errorf("expected (unavailable) for missing file, got: %q", result)
	}
}

func TestFormatFileReferences_MixedExistAndMissing(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.go")
	if err := os.WriteFile(existing, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	result := FormatFileReferences([]string{existing, "/no/such/file.go"})
	if !strings.Contains(result, "(1 bytes)") {
		t.Errorf("expected size for existing file, got: %q", result)
	}
	if !strings.Contains(result, "(unavailable)") {
		t.Errorf("expected unavailable for missing file, got: %q", result)
	}
}

func TestAssemblePrompt_NoFiles(t *testing.T) {
	result := AssemblePrompt("  You are a helpful assistant.  ", "  Explain Go channels.  ", nil)

	expected := "You are a helpful assistant.\n\n=== REQUEST ===\nExplain Go channels."
	if result != expected {
		t.Errorf("got:\n%s\n\nwant:\n%s", result, expected)
	}
}

func TestAssemblePrompt_WithFiles(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	if err := os.WriteFile(f, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	result := AssemblePrompt("Role prompt.", "User request.", []string{f})

	if !strings.Contains(result, "Role prompt.") {
		t.Error("missing role prompt")
	}
	if !strings.Contains(result, "=== REQUEST ===\nUser request.") {
		t.Error("missing request section")
	}
	if !strings.Contains(result, "=== FILES ===") {
		t.Error("missing files section")
	}
	if !strings.Contains(result, "(12 bytes)") {
		t.Error("missing file size")
	}
}

func TestAssemblePrompt_Trimming(t *testing.T) {
	result := AssemblePrompt("\n\n  role  \n\n", "\n  request  \n", nil)

	if strings.Contains(result, "  role  ") {
		t.Error("role prompt not trimmed")
	}
	if strings.Contains(result, "  request  ") {
		t.Error("user request not trimmed")
	}
	if !strings.Contains(result, "role") {
		t.Error("role content missing")
	}
	if !strings.Contains(result, "request") {
		t.Error("request content missing")
	}
}

func TestAssemblePrompt_SectionSeparation(t *testing.T) {
	result := AssemblePrompt("role", "request", nil)
	parts := strings.Split(result, "\n\n")
	if len(parts) != 2 {
		t.Errorf("expected 2 sections separated by blank line, got %d: %q", len(parts), result)
	}
}

func TestAssemblePrompt_EmptyFilesSlice(t *testing.T) {
	result := AssemblePrompt("role", "request", []string{})
	if strings.Contains(result, "FILES") {
		t.Error("empty files slice should not produce a FILES section")
	}
}

func TestAssemblePrompt_EmptyRolePrompt(t *testing.T) {
	result := AssemblePrompt("", "request", nil)
	// Empty role should still produce a valid prompt with request section
	if !strings.Contains(result, "=== REQUEST ===\nrequest") {
		t.Errorf("expected request section, got: %q", result)
	}
	// First section is the trimmed (empty) role, so prompt starts with \n\n
	// or the empty string is joined — verify no crash
	if !strings.HasPrefix(result, "\n\n=== REQUEST") {
		t.Errorf("expected empty role followed by request, got: %q", result[:min(len(result), 40)])
	}
}

func TestFormatFileReferences_EmptyStringPath(t *testing.T) {
	result := FormatFileReferences([]string{""})
	if !strings.Contains(result, "=== FILES ===") {
		t.Error("expected FILES header even with empty path")
	}
	if !strings.Contains(result, "(unavailable)") {
		t.Errorf("empty string path should be unavailable, got: %q", result)
	}
}
