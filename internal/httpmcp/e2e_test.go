package httpmcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestServer creates an httptest.Server backed by a fake backend script.
// Returns the server and its MCP endpoint URL.
func newTestServer(t *testing.T, script string) (*httptest.Server, string) {
	t.Helper()
	backend := writeExecutable(t, script)
	app := NewApp(Config{
		BackendPath:   backend,
		MCPPath:       "/mcp",
		ServerName:    "roundtable-http-mcp",
		ServerVersion: "test",
		ProbeTimeout:  1 * time.Second,
		RequestGrace:  200 * time.Millisecond,
	}, ExecRunner{})

	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	return ts, ts.URL + "/mcp"
}

func TestE2EHealthEndpoints(t *testing.T) {
	ts, _ := newTestServer(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument" }\n'
  exit 1
fi
printf '{ "gemini": { "status": "ok" }, "meta": { "total_elapsed_ms": 1 } }\n'
`)

	for _, tc := range []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/healthz", http.StatusOK, "ok"},
		{"/readyz", http.StatusOK, "ready"},
	} {
		resp, err := http.Get(ts.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != tc.wantStatus {
			t.Errorf("%s status = %d, want %d", tc.path, resp.StatusCode, tc.wantStatus)
		}
		if !strings.Contains(string(body), tc.wantBody) {
			t.Errorf("%s body = %q, want substring %q", tc.path, body, tc.wantBody)
		}
	}
}

func TestE2EToolsListOverHTTP(t *testing.T) {
	_, endpoint := newTestServer(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument" }\n'
  exit 1
fi
printf '{ "gemini": { "status": "ok" } }\n'
`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools list: %v", err)
		}
		names = append(names, tool.Name)
	}

	sort.Strings(names)
	want := []string{"architect", "challenge", "deepdive", "hivemind", "xray"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tool list = %v, want %v", names, want)
	}
}

func TestE2EToolCallSuccessOverHTTP(t *testing.T) {
	_, endpoint := newTestServer(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument" }\n'
  exit 1
fi
printf '{ "gemini": { "status": "ok" }, "codex": { "status": "ok" }, "meta": { "total_elapsed_ms": 42 } }\n'
`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "hivemind",
		Arguments: map[string]any{"prompt": "test prompt"},
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
	if !strings.Contains(text.Text, `"gemini"`) {
		t.Fatalf("unexpected response: %s", text.Text)
	}
}

func TestE2EBackendCrashReturnsError(t *testing.T) {
	_, endpoint := newTestServer(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument" }\n'
  exit 1
fi
printf '{ "error": "segfault in model dispatch" }\n'
exit 1
`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "hivemind",
		Arguments: map[string]any{"prompt": "test prompt"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got success")
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "segfault in model dispatch") {
		t.Fatalf("unexpected error text: %s", text.Text)
	}
}

func TestE2EBackendTimeoutReturnsError(t *testing.T) {
	_, endpoint := newTestServer(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument" }\n'
  exit 1
fi
sleep 30
printf '{ "gemini": { "status": "ok" } }\n'
`)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	timeout := 1
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "hivemind",
		Arguments: map[string]any{
			"prompt":  "test prompt",
			"timeout": timeout,
		},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got success")
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "timed out") {
		t.Fatalf("expected timeout error, got: %s", text.Text)
	}
}

func TestE2EMetricsEndpoint(t *testing.T) {
	ts, endpoint := newTestServer(t, `#!/bin/sh
if [ "$#" -eq 0 ]; then
  printf '{ "error": "Missing required --prompt argument" }\n'
  exit 1
fi
printf '{ "gemini": { "status": "ok" }, "meta": { "total_elapsed_ms": 1 } }\n'
`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// Make a successful tool call
	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "hivemind",
		Arguments: map[string]any{"prompt": "test"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}

	// Check metrics
	resp, err := http.Get(ts.URL + "/metricsz")
	if err != nil {
		t.Fatalf("GET /metricsz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metricsz status = %d, want 200", resp.StatusCode)
	}

	var m map[string]int64
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("parse metrics JSON: %v (body: %s)", err, body)
	}
	if m["total_requests"] < 1 {
		t.Errorf("total_requests = %d, want >= 1", m["total_requests"])
	}
	if m["backend_timeouts"] != 0 {
		t.Errorf("backend_timeouts = %d, want 0", m["backend_timeouts"])
	}
}

func TestE2ENotFoundForUnknownPaths(t *testing.T) {
	ts, _ := newTestServer(t, `#!/bin/sh
printf '{ "error": "noop" }\n'
exit 1
`)

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
