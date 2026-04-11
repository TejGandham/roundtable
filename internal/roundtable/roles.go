package roundtable

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed roles/*.txt
var embeddedRoles embed.FS

// LoadRolePrompt loads a role prompt by name. Fallback semantics match Elixir:
//
//  1. projectDir/<name>.txt  (if projectDir is non-empty)
//  2. globalDir/<name>.txt   (if globalDir is non-empty)
//  3. If BOTH dirs were set and the file wasn't found → error (matching Elixir)
//  4. If globalDir was empty/unset (standalone binary mode) → embedded defaults
//  5. If embedded doesn't have it either → error
//
// Returns the file contents as a string. Returns an error if the role
// is not found or if a file exists but cannot be read.
func LoadRolePrompt(roleName, globalDir, projectDir string) (string, error) {
	filename := roleName + ".txt"

	// Level 1: project directory
	if projectDir != "" {
		content, err := os.ReadFile(filepath.Join(projectDir, filename))
		if err == nil {
			return string(content), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot read role file %s in project dir: %w", roleName, err)
		}
	}

	// Level 2: global directory
	if globalDir != "" {
		content, err := os.ReadFile(filepath.Join(globalDir, filename))
		if err == nil {
			return string(content), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot read role file %s in global dir: %w", roleName, err)
		}
	}

	// If any directory was configured, disk lookup is authoritative — do NOT
	// silently fall back to embedded defaults. This matches Elixir, which
	// raises when role is not found in either directory.
	if globalDir != "" || projectDir != "" {
		searched := []string{}
		if projectDir != "" {
			searched = append(searched, projectDir)
		}
		if globalDir != "" {
			searched = append(searched, globalDir)
		}
		return "", fmt.Errorf("role prompt not found: %s (searched %v)", roleName, searched)
	}

	// Level 3: embedded defaults — only when globalDir is empty/unset
	// (standalone binary mode, no roles directory configured).
	content, err := embeddedRoles.ReadFile("roles/" + filename)
	if err == nil {
		return string(content), nil
	}

	return "", fmt.Errorf("role prompt not found: %s (searched [embedded])", roleName)
}
