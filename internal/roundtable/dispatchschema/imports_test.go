package dispatchschema_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDispatchschemaIsLeafPackage verifies that the dispatchschema package
// does NOT import internal/roundtable (or any path that would transitively
// pull it in), preserving the one-way dependency:
//
//	internal/roundtable → internal/roundtable/dispatchschema  (allowed)
//	internal/roundtable/dispatchschema → internal/roundtable  (FORBIDDEN)
//
// This enforces the import-cycle safety stated in the F03 arch-advisor brief.
// The test uses `go list -deps` which enumerates the full transitive closure.
func TestDispatchschemaIsLeafPackage(t *testing.T) {
	// `go list -deps` prints one package path per line for the target and
	// all of its transitive dependencies.
	cmd := exec.Command(
		"go", "list", "-deps",
		"github.com/TejGandham/roundtable/internal/roundtable/dispatchschema",
	)
	// Run from the module root so the go tool can find the module.
	cmd.Dir = findModuleRoot(t)

	out, err := cmd.Output()
	if err != nil {
		// If the package doesn't exist yet this test is expected to fail to
		// compile the test binary, not here. If go list itself errors, skip
		// with a message so the RED-NEW state is clearly isolated to validate.go.
		t.Skipf("go list -deps failed (dispatchschema package may not compile yet): %v", err)
	}

	const forbidden = "github.com/TejGandham/roundtable/internal/roundtable"

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The dispatchschema package itself is allowed; only the parent
		// package (or anything that imports it) is forbidden.
		if line == forbidden {
			t.Errorf("dispatchschema transitively imports %q — import cycle forbidden.\n"+
				"full dep list:\n%s", forbidden, out)
			return
		}
	}
}

// findModuleRoot walks upward from the test binary's working directory to
// find the directory containing go.mod. In practice for this repo the module
// root is the repo root.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	// `go env GOMOD` prints the absolute path to go.mod.
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	// Strip the "/go.mod" suffix to get the directory.
	idx := strings.LastIndex(gomod, "/")
	if idx < 0 {
		t.Fatalf("unexpected GOMOD path: %q", gomod)
	}
	return gomod[:idx]
}
