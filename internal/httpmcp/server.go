package httpmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

type App struct {
	config  Config
	backend *Backend
	server  *mcp.Server
}

func NewApp(config Config, runner Runner) *App {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    config.ServerName,
		Version: config.ServerVersion,
	}, nil)

	app := &App{
		config:  config,
		backend: NewBackend(config, runner),
		server:  server,
	}

	for _, spec := range toolSpecs {
		registerTool(server, app.backend, spec)
	}

	return app
}

func (a *App) Handler() http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return a.server
	}, &mcp.StreamableHTTPOptions{
		JSONResponse: true,
		Stateless:    true,
	})

	mux := http.NewServeMux()
	mux.Handle(a.config.MCPPath, mcpHandler)
	mux.HandleFunc("/healthz", a.healthz)
	mux.HandleFunc("/readyz", a.readyz)
	mux.HandleFunc("/", a.index)
	return mux
}

func registerTool(server *mcp.Server, backend *Backend, spec ToolSpec) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        spec.Name,
		Description: spec.Description,
		InputSchema: toolInputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ToolInput) (*mcp.CallToolResult, struct{}, error) {
		text, isError := backend.Call(ctx, spec, input)
		return &mcp.CallToolResult{
			IsError: isError,
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, struct{}{}, nil
	})
}

func (a *App) healthz(w http.ResponseWriter, _ *http.Request) {
	writePlain(w, http.StatusOK, "ok")
}

func (a *App) readyz(w http.ResponseWriter, r *http.Request) {
	if err := a.backend.Probe(r.Context()); err != nil {
		writePlain(w, http.StatusServiceUnavailable, fmt.Sprintf("not ready: %v", err))
		return
	}
	writePlain(w, http.StatusOK, "ready")
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writePlain(w, http.StatusOK, fmt.Sprintf("%s listening on %s", a.config.ServerName, a.config.MCPPath))
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
