package stdiomcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func connectServer(t *testing.T, dispatch DispatchFunc) (*mcp.ClientSession, func()) {
	t.Helper()

	cfg := Config{
		ServerName:    "roundtable-test",
		ServerVersion: "v0.0.1",
	}
	srv := NewServer(cfg, dispatch, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		cancel()
		t.Fatalf("client connect: %v", err)
	}

	cleanup := func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
		cancel()
	}
	return clientSession, cleanup
}

func TestNewServerToolList(t *testing.T) {
	dispatch := func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	}
	session, cleanup := connectServer(t, dispatch)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	want := map[string]bool{
		"hivemind":  false,
		"deepdive":  false,
		"architect": false,
		"challenge": false,
		"xray":      false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		} else {
			t.Errorf("unexpected tool: %q", tool.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("missing tool: %q", name)
		}
	}
	if len(res.Tools) != 5 {
		t.Errorf("expected 5 tools, got %d", len(res.Tools))
	}
}

func TestNewServerCallTool(t *testing.T) {
	var capturedSpec ToolSpec
	var capturedInput ToolInput

	dispatch := func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error) {
		capturedSpec = spec
		capturedInput = input
		return json.Marshal(map[string]any{
			"gemini": map[string]any{"status": "ok", "response": "stdio-dispatched"},
			"meta":   map[string]any{"total_elapsed_ms": 7},
		})
	}
	session, cleanup := connectServer(t, dispatch)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "hivemind",
		Arguments: map[string]any{"prompt": "hello stdio"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %#v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatalf("expected content, got empty")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "stdio-dispatched") {
		t.Errorf("unexpected response text: %q", text.Text)
	}
	if capturedSpec.Name != "hivemind" {
		t.Errorf("spec name = %q, want hivemind", capturedSpec.Name)
	}
	if capturedInput.Prompt != "hello stdio" {
		t.Errorf("input prompt = %q, want 'hello stdio'", capturedInput.Prompt)
	}
}
