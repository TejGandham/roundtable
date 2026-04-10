package httpmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandlerReadyzAndHealthz(t *testing.T) {
	script := writeExecutable(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument", "usage": "roundtable --prompt ..." }\n'
  exit 1
fi
printf '{ "gemini": { "status": "ok" }, "meta": { "total_elapsed_ms": 1 } }\n'
`)

	app := NewApp(Config{
		BackendPath:   script,
		MCPPath:       "/mcp",
		ServerName:    "roundtable-http-mcp",
		ServerVersion: "0.6.0",
		ProbeTimeout:  1 * time.Second,
		RequestGrace:  100 * time.Millisecond,
	}, ExecRunner{})

	handler := app.Handler()

	for _, tc := range []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{path: "/healthz", wantStatus: http.StatusOK, wantBody: "ok"},
		{path: "/readyz", wantStatus: http.StatusOK, wantBody: "ready"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != tc.wantStatus {
			t.Fatalf("%s status = %d, want %d (%s)", tc.path, rec.Code, tc.wantStatus, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.wantBody) {
			t.Fatalf("%s body = %q, want substring %q", tc.path, rec.Body.String(), tc.wantBody)
		}
	}
}

func TestMCPToolsListAndCall(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args.log")
	script := writeExecutable(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument", "usage": "roundtable --prompt ..." }\n'
  exit 1
fi
printf '%s\n' "$@" > "`+logPath+`"
printf '{ "gemini": { "status": "ok" }, "meta": { "total_elapsed_ms": 1 } }\n'
`)

	app := NewApp(Config{
		BackendPath:   script,
		MCPPath:       "/mcp",
		ServerName:    "roundtable-http-mcp",
		ServerVersion: "0.6.0",
		ProbeTimeout:  1 * time.Second,
		RequestGrace:  100 * time.Millisecond,
	}, ExecRunner{})

	session, err := connectInMemory(context.Background(), app.server)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer session.Close()

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("tools list failed: %v", err)
		}
		names = append(names, tool.Name)
	}

	wantNames := []string{"architect", "challenge", "deepdive", "hivemind", "xray"}
	if strings.Join(names, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("tool list = %#v, want %#v", names, wantNames)
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "deepdive",
		Arguments: map[string]any{
			"prompt":  "Check this design",
			"timeout": 30,
		},
	})
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %#v", result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("unexpected tool content length: %d", len(result.Content))
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if !strings.Contains(text.Text, `"gemini"`) {
		t.Fatalf("unexpected response text: %s", text.Text)
	}

	loggedArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	logText := string(loggedArgs)
	if !strings.Contains(logText, "--role") || !strings.Contains(logText, "planner") {
		t.Fatalf("expected planner role in args, got: %s", logText)
	}
	if !strings.Contains(logText, "Provide conclusions, assumptions, alternatives, and confidence level.") {
		t.Fatalf("expected deepdive prompt suffix in args, got: %s", logText)
	}
}

func TestMCPToolCallReturnsIsErrorOnBackendFailure(t *testing.T) {
	script := writeExecutable(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument", "usage": "roundtable --prompt ..." }\n'
  exit 1
fi
printf '{ "error": "backend failed fast" }\n'
exit 1
`)

	app := NewApp(Config{
		BackendPath:   script,
		MCPPath:       "/mcp",
		ServerName:    "roundtable-http-mcp",
		ServerVersion: "0.6.0",
		ProbeTimeout:  1 * time.Second,
		RequestGrace:  100 * time.Millisecond,
	}, ExecRunner{})

	session, err := connectInMemory(context.Background(), app.server)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "hivemind",
		Arguments: map[string]any{"prompt": "hello"},
	})
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got success: %#v", result.Content)
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "backend failed fast") {
		t.Fatalf("unexpected error text: %s", text.Text)
	}
}

func TestNewAppWithDispatcherToolList(t *testing.T) {
	dispatch := func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error) {
		return json.Marshal(map[string]any{
			"gemini": map[string]any{"status": "ok"},
			"meta":   map[string]any{"total_elapsed_ms": 1},
		})
	}

	app := NewAppWithDispatcher(Config{
		MCPPath:       "/mcp",
		ServerName:    "test",
		ServerVersion: "v0.0.1",
	}, dispatch)

	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools: %v", err)
		}
		names = append(names, tool.Name)
	}

	if len(names) != 5 {
		t.Fatalf("expected 5 tools, got %d: %v", len(names), names)
	}
}

func TestNewAppWithDispatcherCallTool(t *testing.T) {
	var capturedSpec ToolSpec
	var capturedInput ToolInput

	dispatch := func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error) {
		capturedSpec = spec
		capturedInput = input
		return json.Marshal(map[string]any{
			"gemini": map[string]any{"status": "ok", "response": "dispatched"},
			"meta":   map[string]any{"total_elapsed_ms": 42},
		})
	}

	app := NewAppWithDispatcher(Config{
		MCPPath:       "/mcp",
		ServerName:    "test",
		ServerVersion: "v0.0.1",
	}, dispatch)

	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "deepdive",
		Arguments: map[string]any{"prompt": "test prompt", "timeout": 30},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %#v", result.Content)
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "dispatched") {
		t.Errorf("unexpected response: %s", text.Text)
	}

	if capturedSpec.Name != "deepdive" {
		t.Errorf("spec name = %q, want deepdive", capturedSpec.Name)
	}
	if capturedInput.Prompt != "test prompt" {
		t.Errorf("input prompt = %q, want 'test prompt'", capturedInput.Prompt)
	}
}

func TestNewAppWithDispatcherReadyz(t *testing.T) {
	dispatch := func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error) {
		return []byte("{}"), nil
	}

	app := NewAppWithDispatcher(Config{
		MCPPath:       "/mcp",
		ServerName:    "test",
		ServerVersion: "v0.0.1",
	}, dispatch)

	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("readyz status = %d, want 200", resp.StatusCode)
	}
}

func connectInMemory(ctx context.Context, server *mcp.Server) (*mcp.ClientSession, error) {
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		return nil, err
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	return client.Connect(ctx, t2, nil)
}
