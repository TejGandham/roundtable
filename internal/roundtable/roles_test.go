package roundtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRolePrompt_EmbeddedDefault(t *testing.T) {
	// No dirs specified — should fall back to embedded default
	content, err := LoadRolePrompt("default", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "senior software engineer") {
		t.Errorf("expected embedded default role content, got: %q", content[:min(len(content), 80)])
	}
}

func TestLoadRolePrompt_EmbeddedPlanner(t *testing.T) {
	content, err := LoadRolePrompt("planner", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "software architect") {
		t.Errorf("expected planner role content, got: %q", content[:min(len(content), 80)])
	}
}

func TestLoadRolePrompt_EmbeddedCodeReviewer(t *testing.T) {
	content, err := LoadRolePrompt("codereviewer", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "code reviewer") {
		t.Errorf("expected codereviewer role content, got: %q", content[:min(len(content), 80)])
	}
}

func TestLoadRolePrompt_GlobalDirOverride(t *testing.T) {
	dir := t.TempDir()
	rolePath := filepath.Join(dir, "default.txt")
	if err := os.WriteFile(rolePath, []byte("custom global role"), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := LoadRolePrompt("default", dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "custom global role" {
		t.Errorf("expected custom global role, got: %q", content)
	}
}

func TestLoadRolePrompt_ProjectDirOverride(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()

	// Write different content in both dirs
	if err := os.WriteFile(filepath.Join(globalDir, "default.txt"), []byte("global"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "default.txt"), []byte("project"), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := LoadRolePrompt("default", globalDir, projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "project" {
		t.Errorf("expected project dir to win, got: %q", content)
	}
}

func TestLoadRolePrompt_ProjectMissFallsToGlobal(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()

	// Only global has the file
	if err := os.WriteFile(filepath.Join(globalDir, "default.txt"), []byte("global"), 0644); err != nil {
		t.Fatal(err)
	}

	content, err := LoadRolePrompt("default", globalDir, projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "global" {
		t.Errorf("expected global fallback, got: %q", content)
	}
}

func TestLoadRolePrompt_DirsSetButMissing_ReturnsError(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	// Neither dir has the file — with dirs configured, this is an error
	// (matching Elixir semantics; embedded fallback is NOT used)
	_, err := LoadRolePrompt("default", globalDir, projectDir)
	if err == nil {
		t.Fatal("expected error when dirs configured but role not found")
	}
	if !strings.Contains(err.Error(), "role prompt not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoadRolePrompt_NoDirsConfigured_FallsToEmbedded(t *testing.T) {
	// globalDir empty = standalone binary mode — embedded fallback is used
	content, err := LoadRolePrompt("default", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "senior software engineer") {
		t.Errorf("expected embedded default, got: %q", content[:min(len(content), 80)])
	}
}

func TestLoadRolePrompt_MissingRoleError(t *testing.T) {
	_, err := LoadRolePrompt("nonexistent_role_xyz", "", "")
	if err == nil {
		t.Fatal("expected error for nonexistent role")
	}
	if !strings.Contains(err.Error(), "role prompt not found") {
		t.Errorf("unexpected error message: %v", err)
	}
	if !strings.Contains(err.Error(), "nonexistent_role_xyz") {
		t.Errorf("error should mention role name: %v", err)
	}
}

func TestLoadRolePrompt_UnreadableFileError(t *testing.T) {
	dir := t.TempDir()
	rolePath := filepath.Join(dir, "default.txt")

	// Create a directory where a file is expected — reading it returns a non-ENOENT error
	if err := os.Mkdir(rolePath, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRolePrompt("default", dir, "")
	if err == nil {
		t.Fatal("expected error for unreadable role file")
	}
	if !strings.Contains(err.Error(), "cannot read role file") {
		t.Errorf("unexpected error message: %v", err)
	}
}
