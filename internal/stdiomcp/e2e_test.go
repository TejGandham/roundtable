package stdiomcp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioE2E builds the roundtable binary, spawns it with the `stdio`
// subcommand, runs through initialize + tools/list, and asserts all five
// tools are advertised. It does NOT actually call a tool — that would
// require real gemini/codex/claude binaries on PATH. Tool dispatch is
// covered by internal/roundtable/run_test.go with mock backends.
//
// The critical invariant this catches: if something starts writing to
// stdout (fmt.Println, a panic stack, a dep that logs to stdout on
// import), the MCP framing is corrupted and this test fails at Connect()
// or ListTools().
func TestStdioE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	binPath := buildRoundtableBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "0"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command(binPath, "stdio")}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect to spawned binary: %v", err)
	}
	defer session.Close()

	listResp, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := map[string]bool{
		"hivemind":  false,
		"deepdive":  false,
		"architect": false,
		"challenge": false,
		"xray":      false,
	}
	for _, tool := range listResp.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q not advertised", name)
		}
	}
}

// buildRoundtableBinary compiles the roundtable binary into a temporary
// directory and returns its path. Uses `go build` directly; if `go` is
// not on PATH (mise-managed environment), honors GO_BIN.
func buildRoundtableBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "roundtable")

	goBin := "go"
	if env := os.Getenv("GO_BIN"); env != "" {
		goBin = env
	} else if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go not on PATH and GO_BIN not set: %v", err)
	}

	// Build from the test's working directory (internal/stdiomcp).
	cmd := exec.Command(goBin, "build", "-o", bin, "../../cmd/roundtable")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}
