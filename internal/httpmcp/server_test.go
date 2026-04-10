package httpmcp

import (
	"context"
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

func connectInMemory(ctx context.Context, server *mcp.Server) (*mcp.ClientSession, error) {
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		return nil, err
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	return client.Connect(ctx, t2, nil)
}
