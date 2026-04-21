package roundtable

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// CodexBackend speaks JSON-RPC over stdio to `codex app-server --listen stdio://`.
type CodexBackend struct {
	execPath      string
	model         string
	clientVersion string // advertised to codex in the initialize handshake

	startOnce sync.Once
	startErr  error

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID atomic.Int64

	// notifications is keyed by turn ID; each entry receives all
	// JSON-RPC notifications for that turn until turn/completed.
	notifMu sync.Mutex
	notifs  map[string]chan json.RawMessage

	// pending is keyed by request ID; each entry receives the
	// JSON-RPC response for that request.
	pendingMu sync.Mutex
	pending   map[int64]chan json.RawMessage

	done chan struct{} // closed when readLoop exits
}

// NewCodexBackend creates a CodexBackend that will launch the given executable.
// clientVersion is advertised as clientInfo.version in the initialize handshake;
// the composition root passes the binary's build-time version string.
func NewCodexBackend(execPath, model, clientVersion string) *CodexBackend {
	if clientVersion == "" {
		clientVersion = "dev"
	}
	return &CodexBackend{
		execPath:      execPath,
		model:         model,
		clientVersion: clientVersion,
		notifs:        make(map[string]chan json.RawMessage),
		pending:       make(map[int64]chan json.RawMessage),
		done:          make(chan struct{}),
	}
}

func (c *CodexBackend) Name() string { return "codex" }

// doStart launches the codex app-server subprocess and completes the
// JSON-RPC initialize handshake. This is the body of the old Start(),
// called exactly once via startOnce from ensureStarted.
//
// The subprocess is NOT bound to any caller context. Its lifetime is
// tied to the CodexBackend itself and is terminated only by Stop(),
// which kills the process group explicitly. Binding the subprocess to
// a per-request context would cause the process to be killed when the
// first caller's context expires, latching a permanent error in
// startOnce for all subsequent callers.
func (c *CodexBackend) doStart(_ context.Context) error {
	c.mu.Lock()
	// If stdin is already wired (e.g., in tests that inject fake pipes),
	// skip exec and handshake — the caller has already set things up.
	if c.stdin != nil {
		c.mu.Unlock()
		return nil
	}

	// Use exec.Command (not exec.CommandContext) so the subprocess is not
	// tied to any individual caller's context. Cleanup is via Stop().
	cmd := exec.Command(c.execPath, "app-server", "--listen", "stdio://")
	applyPdeathsig(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("codex stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		c.mu.Unlock()
		return fmt.Errorf("codex stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		c.mu.Unlock()
		return fmt.Errorf("codex start: %w", err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.reader = bufio.NewReaderSize(stdout, 256*1024)
	c.done = make(chan struct{})

	go c.readLoop()
	c.mu.Unlock()

	// Send initialize outside the lock — c.call acquires c.mu for the
	// stdin write, so we must not hold it here. Derive from Background so
	// the handshake is not cut short by a short-lived caller context.
	initCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.call(initCtx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "roundtable",
			"version": c.clientVersion,
		},
	})
	if err != nil {
		return fmt.Errorf("codex initialize: %w", err)
	}
	return nil
}

// ensureStarted runs doStart under sync.Once. Subsequent callers block
// on the Once until the first caller finishes, then see the same startErr.
//
// Note: ctx is currently not used for subprocess creation — the codex
// app-server lifecycle is bound to the CodexBackend, not the caller's
// request context. ctx is retained for API symmetry with Start().
func (c *CodexBackend) ensureStarted(ctx context.Context) error {
	c.startOnce.Do(func() {
		c.startErr = c.doStart(ctx)
	})
	return c.startErr
}

// Start is an idempotent shim that delegates to ensureStarted. Safe to
// call many times. The HTTP path still calls it during buildBackends;
// Phase C removes that call and exports only Run/Healthy/Stop.
func (c *CodexBackend) Start(ctx context.Context) error {
	return c.ensureStarted(ctx)
}

// Stop kills the codex subprocess and releases resources.
// It grabs references under lock then operates without the lock
// to avoid deadlock if stdin.Write is blocked holding c.mu.
func (c *CodexBackend) Stop() error {
	c.mu.Lock()
	cmd := c.cmd
	stdin := c.stdin
	c.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = stdin.Close()

	// Kill the process group so any codex-spawned children die with it.
	if cmd.Process.Pid > 0 {
		killProcessGroup(cmd.Process.Pid)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	<-c.done
	return nil
}

// Healthy is a cheap liveness check. It does NOT start the subprocess;
// it only verifies the exec path still resolves. This lets run.go's
// probe phase succeed without paying the app-server startup cost.
//
// If the subprocess was already started (via a prior Run or Start),
// Healthy also checks that readLoop has not exited. Otherwise nil.
func (c *CodexBackend) Healthy(_ context.Context) error {
	if c.execPath == "" {
		return errors.New("codex exec path empty")
	}
	if _, err := os.Stat(c.execPath); err != nil {
		return fmt.Errorf("codex exec path %q: %w", c.execPath, err)
	}

	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		// Not started yet — that's fine, Run will start lazily.
		return nil
	}
	select {
	case <-c.done:
		return errors.New("codex process exited")
	default:
		return nil
	}
}

// Run sends a prompt to the Codex app-server and collects the response.
// Protocol: thread/start -> turn/start -> collect notifications until turn/completed.
func (c *CodexBackend) Run(ctx context.Context, req Request) (*Result, error) {
	start := time.Now()

	// Lazy-start the app-server subprocess on first use.
	if err := c.ensureStarted(ctx); err != nil {
		return c.errorResult(req, start, fmt.Errorf("codex start: %w", err)), nil
	}

	// Step 1: thread/start
	threadResp, err := c.call(ctx, "thread/start", map[string]any{})
	if err != nil {
		return c.errorResult(req, start, err), nil
	}

	var threadResult struct {
		Result struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		} `json:"result"`
	}
	if err := json.Unmarshal(threadResp, &threadResult); err != nil {
		return c.errorResult(req, start, fmt.Errorf("parse thread/start: %w", err)), nil
	}
	threadID := threadResult.Result.Thread.ID

	// Register notification channel for this turn
	notifCh := make(chan json.RawMessage, 256)
	c.notifMu.Lock()
	c.notifs[threadID] = notifCh
	c.notifMu.Unlock()
	defer func() {
		c.notifMu.Lock()
		delete(c.notifs, threadID)
		c.notifMu.Unlock()
	}()

	// Step 2: turn/start
	// input is an array of UserInput objects per the schema
	turnParams := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{
			{"type": "text", "text": req.Prompt},
		},
	}
	if req.Model != "" {
		turnParams["model"] = req.Model
	}

	_, err = c.call(ctx, "turn/start", turnParams)
	if err != nil {
		return c.errorResult(req, start, err), nil
	}

	// Step 3: Collect notifications until turn/completed
	return c.collectTurn(ctx, req, threadID, notifCh, start)
}

// call sends a JSON-RPC request and waits for the response.
func (c *CodexBackend) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	respCh := make(chan json.RawMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", method, err)
	}

	c.mu.Lock()
	_, err = c.stdin.Write(append(data, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case resp := <-respCh:
		// Check for JSON-RPC error
		var rpcErr struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(resp, &rpcErr) == nil && rpcErr.Error != nil {
			return resp, fmt.Errorf("rpc error %d: %s", rpcErr.Error.Code, rpcErr.Error.Message)
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("codex process exited during call")
	}
}

// collectTurn reads notifications until turn/completed or context cancellation.
func (c *CodexBackend) collectTurn(
	ctx context.Context,
	req Request,
	threadID string,
	notifCh <-chan json.RawMessage,
	start time.Time,
) (*Result, error) {
	var messages []string
	var sessionID string
	model := req.Model
	if model == "" {
		model = c.model
	}
	if model == "" {
		model = "cli-default"
	}

	for {
		select {
		case raw := <-notifCh:
			var notif struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(raw, &notif); err != nil {
				continue
			}

			switch notif.Method {
			case "item/completed":
				var p struct {
					Item struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				}
				if json.Unmarshal(notif.Params, &p) == nil && p.Item.Type == "agentMessage" {
					text := p.Item.Text
					if text != "" {
						messages = append(messages, text)
					}
				}

			case "turn/completed":
				sessionID = threadID

				elapsed := time.Since(start).Milliseconds()
				response := ""
				for i, m := range messages {
					if i > 0 {
						response += "\n\n"
					}
					response += m
				}

				status := "ok"
				if len(messages) == 0 {
					status = "error"
				}

				sid := &sessionID
				return &Result{
					Response:  response,
					Model:     model,
					Status:    status,
					ElapsedMs: elapsed,
					SessionID: sid,
				}, nil

			case "threadTokenUsage/updated":
				// Could extract usage stats here; not needed for Phase 2A
			}

		case <-ctx.Done():
			elapsed := time.Since(start).Milliseconds()
			// Attempt to interrupt the turn with a bounded timeout
			interruptCtx, interruptCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer interruptCancel()
			_, _ = c.call(interruptCtx, "turn/interrupt", map[string]any{
				"threadId": threadID,
			})
			return &Result{
				Response:  "Request timed out",
				Model:     model,
				Status:    "timeout",
				ElapsedMs: elapsed,
			}, nil

		case <-c.done:
			elapsed := time.Since(start).Milliseconds()
			return &Result{
				Model:     model,
				Status:    "error",
				Stderr:    "codex process exited during turn",
				ElapsedMs: elapsed,
			}, nil
		}
	}
}

// readLoop reads newline-delimited JSON from stdout and routes messages
// to pending request channels or notification channels.
func (c *CodexBackend) readLoop() {
	defer close(c.done)

	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}

		if msg.ID != nil {
			// This is a response to a request
			c.pendingMu.Lock()
			ch, ok := c.pending[*msg.ID]
			c.pendingMu.Unlock()
			if ok {
				select {
				case ch <- json.RawMessage(line):
				default:
				}
			}
			continue
		}

		if msg.Method != "" {
			// This is a notification — route to the appropriate turn
			// Extract threadId from params to find the right channel
			var params struct {
				ThreadID string `json:"threadId"`
			}
			if json.Unmarshal(msg.Params, &params) == nil && params.ThreadID != "" {
				c.notifMu.Lock()
				ch, ok := c.notifs[params.ThreadID]
				c.notifMu.Unlock()
				if ok {
					select {
					case ch <- json.RawMessage(line):
					default:
					}
				}
			}
		}
	}
}

func (c *CodexBackend) errorResult(req Request, start time.Time, err error) *Result {
	model := req.Model
	if model == "" {
		model = c.model
	}
	if model == "" {
		model = "cli-default"
	}
	parseErr := err.Error()
	return &Result{
		Model:      model,
		Status:     "error",
		Stderr:     err.Error(),
		ElapsedMs:  time.Since(start).Milliseconds(),
		ParseError: &parseErr,
	}
}
