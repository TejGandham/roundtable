package roundtable

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// fakeCodexServer simulates a codex app-server on the other end of a pipe pair.
// It reads JSON-RPC requests from clientW (the "stdin" the client writes to)
// and writes responses/notifications to serverW (the "stdout" the client reads).
type fakeCodexServer struct {
	reader   *bufio.Reader // reads from clientR (client's stdin writes)
	writer   io.Writer     // writes to serverW (client's stdout reads)
	threadID string
	mu       sync.Mutex
}

func newFakeCodexServer(clientR io.Reader, serverW io.Writer) *fakeCodexServer {
	return &fakeCodexServer{
		reader:   bufio.NewReader(clientR),
		writer:   serverW,
		threadID: "thr_42",
	}
}

func (f *fakeCodexServer) serve(t *testing.T) {
	t.Helper()
	for {
		line, err := f.reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			f.respond(req.ID, map[string]any{
				"serverInfo": map[string]any{"name": "fake", "version": "0.0"},
			})

		case "thread/start":
			f.respond(req.ID, map[string]any{
				"thread": map[string]any{
					"id": f.threadID,
				},
			})

		case "turn/start":
			f.respond(req.ID, map[string]any{
				"turn": map[string]any{
					"id":     1,
					"status": "inProgress",
				},
			})

			// Simulate streaming: send item/completed then turn/completed
			f.notify("item/completed", map[string]any{
				"threadId": f.threadID,
				"item": map[string]any{
					"type": "agentMessage",
					"text": "Hello from Codex!",
				},
			})
			f.notify("turn/completed", map[string]any{
				"threadId": f.threadID,
				"turn": map[string]any{
					"status": "completed",
				},
			})

		case "turn/interrupt":
			f.respond(req.ID, map[string]any{"ok": true})
		}
	}
}

func (f *fakeCodexServer) respond(id int64, result any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	fmt.Fprintf(f.writer, "%s\n", data)
}

func (f *fakeCodexServer) notify(method string, params any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	fmt.Fprintf(f.writer, "%s\n", data)
}

// setupFakeCodex creates a CodexBackend wired to a fakeCodexServer via pipes,
// bypassing the real subprocess launch.
func setupFakeCodex(t *testing.T) (*CodexBackend, *fakeCodexServer) {
	t.Helper()

	// clientR/clientW: client writes to clientW, server reads from clientR
	clientR, clientW := io.Pipe()
	// serverR/serverW: server writes to serverW, client reads from serverR
	serverR, serverW := io.Pipe()

	cb := &CodexBackend{
		execPath: "fake-codex",
		model:    "o3-pro",
		notifs:   make(map[string]chan json.RawMessage),
		pending:  make(map[int64]chan json.RawMessage),
		done:     make(chan struct{}),
		stdin:    clientW,
		reader:   bufio.NewReaderSize(serverR, 256*1024),
	}

	server := newFakeCodexServer(clientR, serverW)

	go cb.readLoop()
	go server.serve(t)

	t.Cleanup(func() {
		clientW.Close()
		serverW.Close()
		clientR.Close()
		serverR.Close()
		<-cb.done
	})

	return cb, server
}

func TestCodexBackendName(t *testing.T) {
	cb := NewCodexBackend("/usr/bin/codex", "o3-pro", "test")
	if cb.Name() != "codex" {
		t.Errorf("Name() = %q, want codex", cb.Name())
	}
}

// TestCodexBackend_HealthyBeforeStart verifies the cheap Healthy path
// works without ever calling Start.
func TestCodexBackend_HealthyBeforeStart(t *testing.T) {
	// Use any existing file as the exec path — we're testing the
	// cheap check, not the real codex binary.
	f := t.TempDir() + "/fake-codex"
	if err := os.WriteFile(f, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cb := NewCodexBackend(f, "", "test")
	if err := cb.Healthy(context.Background()); err != nil {
		t.Errorf("Healthy before Start returned error: %v", err)
	}
}

// TestCodexBackend_HealthyMissingExecPath verifies Healthy fails loudly
// when the exec path no longer resolves.
func TestCodexBackend_HealthyMissingExecPath(t *testing.T) {
	cb := NewCodexBackend("/nonexistent/codex", "", "test")
	err := cb.Healthy(context.Background())
	if err == nil {
		t.Error("Healthy with missing exec path: expected error, got nil")
	}
}

func TestCodexRunSuccess(t *testing.T) {
	cb, _ := setupFakeCodex(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cb.Run(ctx, Request{
		Prompt:  "What is 2+2?",
		Timeout: 30,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if result.Status != "ok" {
		t.Errorf("status = %q, want ok", result.Status)
	}
	if result.Response != "Hello from Codex!" {
		t.Errorf("response = %q, want 'Hello from Codex!'", result.Response)
	}
	if result.Model != "o3-pro" {
		t.Errorf("model = %q, want o3-pro", result.Model)
	}
	if result.SessionID == nil || *result.SessionID != "thr_42" {
		t.Errorf("session_id = %v, want thr_42", result.SessionID)
	}
	if result.ElapsedMs < 0 {
		t.Errorf("elapsed_ms = %d, want >= 0", result.ElapsedMs)
	}
}

func TestCodexRunMultipleMessages(t *testing.T) {
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()

	cb := &CodexBackend{
		execPath: "fake-codex",
		model:    "o3-pro",
		notifs:   make(map[string]chan json.RawMessage),
		pending:  make(map[int64]chan json.RawMessage),
		done:     make(chan struct{}),
		stdin:    clientW,
		reader:   bufio.NewReaderSize(serverR, 256*1024),
	}

	// Custom server that sends two item/completed before turn/completed
	server := newFakeCodexServer(clientR, serverW)
	go func() {
		for {
			line, err := server.reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(line, &req) != nil {
				continue
			}

			switch req.Method {
			case "initialize":
				server.respond(req.ID, map[string]any{
					"serverInfo": map[string]any{"name": "fake", "version": "0.0"},
				})
			case "thread/start":
				server.respond(req.ID, map[string]any{
					"thread": map[string]any{"id": "thr_99"},
				})
			case "turn/start":
				server.respond(req.ID, map[string]any{
					"turn": map[string]any{"id": 1, "status": "inProgress"},
				})
				server.notify("item/completed", map[string]any{
					"threadId": "thr_99",
					"item":     map[string]any{"type": "agentMessage", "text": "First message"},
				})
				server.notify("item/completed", map[string]any{
					"threadId": "thr_99",
					"item":     map[string]any{"type": "agentMessage", "text": "Second message"},
				})
				server.notify("turn/completed", map[string]any{
					"threadId": "thr_99",
					"turn":     map[string]any{"status": "completed"},
				})
			}
		}
	}()

	go cb.readLoop()

	t.Cleanup(func() {
		clientW.Close()
		serverW.Close()
		<-cb.done
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cb.Run(ctx, Request{Prompt: "hello", Timeout: 30})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if result.Response != "First message\n\nSecond message" {
		t.Errorf("response = %q, want 'First message\\n\\nSecond message'", result.Response)
	}
}

func TestCodexRunTimeout(t *testing.T) {
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()

	cb := &CodexBackend{
		execPath: "fake-codex",
		model:    "o3-pro",
		notifs:   make(map[string]chan json.RawMessage),
		pending:  make(map[int64]chan json.RawMessage),
		done:     make(chan struct{}),
		stdin:    clientW,
		reader:   bufio.NewReaderSize(serverR, 256*1024),
	}

	// Server that responds to thread/start and turn/start but never sends
	// turn/completed — simulates a hung model.
	go func() {
		reader := bufio.NewReader(clientR)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(line, &req) != nil {
				continue
			}
			var resp []byte
			switch req.Method {
			case "initialize":
				resp, _ = json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"serverInfo": map[string]any{"name": "fake", "version": "0.0"}},
				})
			case "thread/start":
				resp, _ = json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"thread": map[string]any{"id": "thr_77"}},
				})
			case "turn/start":
				resp, _ = json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"turn": map[string]any{"id": 1, "status": "inProgress"}},
				})
			case "turn/interrupt":
				resp, _ = json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"ok": true},
				})
			default:
				continue
			}
			fmt.Fprintf(serverW, "%s\n", resp)
		}
	}()

	go cb.readLoop()

	t.Cleanup(func() {
		clientW.Close()
		serverW.Close()
		<-cb.done
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := cb.Run(ctx, Request{Prompt: "hello", Timeout: 1})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if result.Status != "timeout" {
		t.Errorf("status = %q, want timeout", result.Status)
	}
	if result.ElapsedMs <= 0 {
		t.Errorf("elapsed_ms = %d, want > 0", result.ElapsedMs)
	}
}

func TestCodexRunProcessExit(t *testing.T) {
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()

	cb := &CodexBackend{
		execPath: "fake-codex",
		model:    "o3-pro",
		notifs:   make(map[string]chan json.RawMessage),
		pending:  make(map[int64]chan json.RawMessage),
		done:     make(chan struct{}),
		stdin:    clientW,
		reader:   bufio.NewReaderSize(serverR, 256*1024),
	}

	// Server that responds to thread/start and turn/start, then closes the pipe
	// to simulate a crash.
	go func() {
		reader := bufio.NewReader(clientR)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(line, &req) != nil {
				continue
			}
			var resp []byte
			switch req.Method {
			case "initialize":
				resp, _ = json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"serverInfo": map[string]any{"name": "fake", "version": "0.0"}},
				})
				fmt.Fprintf(serverW, "%s\n", resp)
			case "thread/start":
				resp, _ = json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"thread": map[string]any{"id": "thr_55"}},
				})
				fmt.Fprintf(serverW, "%s\n", resp)
			case "turn/start":
				resp, _ = json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"turn": map[string]any{"id": 1, "status": "inProgress"}},
				})
				fmt.Fprintf(serverW, "%s\n", resp)
				// Simulate crash: close the pipe
				time.Sleep(50 * time.Millisecond)
				serverW.Close()
				return
			}
		}
	}()

	go cb.readLoop()

	t.Cleanup(func() {
		clientW.Close()
		clientR.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cb.Run(ctx, Request{Prompt: "hello", Timeout: 30})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if result.Status != "error" {
		t.Errorf("status = %q, want error", result.Status)
	}
}

func TestCodexRPCErrorResponse(t *testing.T) {
	clientR, clientW := io.Pipe()
	serverR, serverW := io.Pipe()

	cb := &CodexBackend{
		execPath: "fake-codex",
		model:    "o3-pro",
		notifs:   make(map[string]chan json.RawMessage),
		pending:  make(map[int64]chan json.RawMessage),
		done:     make(chan struct{}),
		stdin:    clientW,
		reader:   bufio.NewReaderSize(serverR, 256*1024),
	}

	// Server that returns a JSON-RPC error for thread/start
	go func() {
		reader := bufio.NewReader(clientR)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(line, &req) != nil {
				continue
			}
			resp, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error": map[string]any{
					"code":    -32600,
					"message": "invalid request",
				},
			})
			fmt.Fprintf(serverW, "%s\n", resp)
		}
	}()

	go cb.readLoop()

	t.Cleanup(func() {
		clientW.Close()
		serverW.Close()
		<-cb.done
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cb.Run(ctx, Request{Prompt: "hello", Timeout: 30})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if result.Status != "error" {
		t.Errorf("status = %q, want error", result.Status)
	}
	if result.ParseError == nil {
		t.Error("parse_error is nil, want non-nil")
	}
}
