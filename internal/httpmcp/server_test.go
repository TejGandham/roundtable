package httpmcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeProbe struct {
	err error
}

func (f fakeProbe) Healthy(context.Context) error { return f.err }

func successDispatch(response string) DispatchFunc {
	return func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error) {
		return json.Marshal(map[string]any{
			"gemini": map[string]any{"status": "ok", "response": response},
			"meta":   map[string]any{"total_elapsed_ms": 42},
		})
	}
}

func newTestApp(t *testing.T, dispatch DispatchFunc, probes map[string]BackendProbe) (*httptest.Server, string) {
	t.Helper()
	app := NewApp(Config{
		MCPPath:       "/mcp",
		ServerName:    "test",
		ServerVersion: "v0.0.1",
	}, dispatch, probes)

	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	return ts, ts.URL + "/mcp"
}

func TestHealthzAlwaysOK(t *testing.T) {
	ts, _ := newTestApp(t, successDispatch("ok"), nil)
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestReadyzNoBackendsAssumesHealthy(t *testing.T) {
	ts, _ := newTestApp(t, successDispatch("ok"), nil)
	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestReadyzHealthyBackends(t *testing.T) {
	probes := map[string]BackendProbe{
		"gemini": fakeProbe{},
		"codex":  fakeProbe{},
		"claude": fakeProbe{},
	}
	ts, _ := newTestApp(t, successDispatch("ok"), probes)

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestReadyzUnhealthyBackendReturns503(t *testing.T) {
	probes := map[string]BackendProbe{
		"gemini": fakeProbe{err: errors.New("not found")},
	}
	ts, _ := newTestApp(t, successDispatch("ok"), probes)

	resp, err := http.Get(ts.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestToolsListOverHTTP(t *testing.T) {
	_, endpoint := newTestApp(t, successDispatch("ok"), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
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

func TestToolCallSuccess(t *testing.T) {
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

	_, endpoint := newTestApp(t, dispatch, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
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

func TestToolCallDispatchError(t *testing.T) {
	dispatch := func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error) {
		return nil, errors.New("backend exploded")
	}

	_, endpoint := newTestApp(t, dispatch, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "hivemind",
		Arguments: map[string]any{"prompt": "hello"},
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
	if !strings.Contains(text.Text, "backend exploded") {
		t.Errorf("expected error text, got: %s", text.Text)
	}
}

func TestToolCallPanicRecovery(t *testing.T) {
	dispatch := func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error) {
		panic("simulated panic")
	}

	_, endpoint := newTestApp(t, dispatch, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "hivemind",
		Arguments: map[string]any{"prompt": "hello"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result from panic, got success")
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "internal error") || !strings.Contains(text.Text, "simulated panic") {
		t.Errorf("expected panic recovery message, got: %s", text.Text)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	ts, endpoint := newTestApp(t, successDispatch("ok"), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "hivemind",
		Arguments: map[string]any{"prompt": "test"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}

	resp, err := http.Get(ts.URL + "/metricsz")
	if err != nil {
		t.Fatalf("GET /metricsz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var m map[string]int64
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("parse metrics JSON: %v (body: %s)", err, body)
	}
	if m["total_requests"] < 1 {
		t.Errorf("total_requests = %d, want >= 1", m["total_requests"])
	}
	if m["dispatch_errors"] != 0 {
		t.Errorf("dispatch_errors = %d, want 0", m["dispatch_errors"])
	}
}

func TestNotFoundForUnknownPaths(t *testing.T) {
	ts, _ := newTestApp(t, successDispatch("ok"), nil)

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
