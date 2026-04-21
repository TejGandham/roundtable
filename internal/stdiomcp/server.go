package stdiomcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const keepaliveInterval = 5 * time.Second

var toolSpecs = []ToolSpec{
	{
		Name:        "hivemind",
		Description: "Run multi-model consensus with default role across all models.",
		Role:        "default",
	},
	{
		Name:         "deepdive",
		Description:  "Run deeper analysis consensus using planner role across all models.",
		Role:         "planner",
		PromptSuffix: "\n\nProvide conclusions, assumptions, alternatives, and confidence level.",
	},
	{
		Name:         "architect",
		Description:  "Generate implementation architecture with planner role across models.",
		Role:         "planner",
		PromptSuffix: "\n\nProvide phases, dependencies, risks, and milestones.",
	},
	{
		Name:         "challenge",
		Description:  "Run critical review consensus using codereviewer role across models.",
		Role:         "codereviewer",
		PromptSuffix: "\n\nAct as a critical reviewer. Find flaws, risks, and weaknesses.",
	},
	{
		Name:        "xray",
		Description: "Run architecture and quality xray with per-model role assignments.",
		GeminiRole:  "planner",
		CodexRole:   "codereviewer",
		ClaudeRole:  "default",
	},
}

var toolInputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "prompt": {"type": "string"},
    "files": {"type": "string"},
    "timeout": {"type": "integer", "minimum": 1, "maximum": 900},
    "gemini_model": {"type": "string"},
    "codex_model": {"type": "string"},
    "claude_model": {"type": "string"},
    "gemini_resume": {"type": "string"},
    "codex_resume": {"type": "string"},
    "claude_resume": {"type": "string"},
    "agents": {"type": "string"}
  },
  "required": ["prompt"]
}`)

// Config is the subset of runtime config that the stdio server needs.
// Intentionally has no HTTP fields — Addr, MCPPath, ProbeTimeout belong
// in httpmcp/config.go only.
type Config struct {
	RolesDir        string
	ProjectRolesDir string
	ServerName      string
	ServerVersion   string
}

// NewServer constructs an mcp.Server with all five roundtable tools
// registered against the given dispatch function. It does NOT connect
// to any transport — the caller passes the returned *mcp.Server to
// Serve() or an equivalent.
func NewServer(cfg Config, dispatch DispatchFunc, logger *slog.Logger) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    cfg.ServerName,
		Version: cfg.ServerVersion,
	}, nil)

	for _, spec := range toolSpecs {
		registerTool(srv, spec, dispatch, logger)
	}
	return srv
}

func registerTool(srv *mcp.Server, spec ToolSpec, dispatch DispatchFunc, logger *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        spec.Name,
		Description: spec.Description,
		InputSchema: toolInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ToolInput) (*mcp.CallToolResult, any, error) {
		token := req.Params.GetProgressToken()

		type callResult struct {
			text    string
			isError bool
		}
		done := make(chan callResult, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("dispatch panic", "tool", spec.Name, "panic", r)
					done <- callResult{text: fmt.Sprintf("internal error: %v", r), isError: true}
				}
			}()
			data, err := dispatch(ctx, spec, input)
			if err != nil {
				logger.Error("dispatch error", "tool", spec.Name, "error", err)
				done <- callResult{text: fmt.Sprintf("roundtable dispatch error: %v", err), isError: true}
				return
			}
			done <- callResult{text: string(data), isError: false}
		}()

		notify := func(tick int) {
			if token == nil || req.Session == nil {
				return
			}
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: token,
				Progress:      float64(tick),
				Message:       "backend running",
			})
		}

		notify(0)

		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case result := <-done:
				return &mcp.CallToolResult{
					IsError: result.isError,
					Content: []mcp.Content{&mcp.TextContent{Text: result.text}},
				}, nil, nil
			case <-ctx.Done():
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("request cancelled: %v", ctx.Err())}},
				}, nil, nil
			case <-ticker.C:
				ticks++
				notify(ticks)
			}
		}
	})
}

// Serve wires the server to an mcp.StdioTransport and blocks until ctx
// is cancelled or stdin is closed.
func Serve(ctx context.Context, srv *mcp.Server) error {
	return srv.Run(ctx, &mcp.StdioTransport{})
}
