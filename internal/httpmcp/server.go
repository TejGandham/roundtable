package httpmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

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

// DispatchFunc is the native dispatch entry point. It takes a context, a tool
// spec, and tool input, and returns the JSON-encoded DispatchResult.
type DispatchFunc func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error)

// BackendProbe is the minimal interface required for startup and readyz
// health checks. The main package wires each roundtable backend in here.
type BackendProbe interface {
	Healthy(ctx context.Context) error
}

const healthCacheTTL = 10 * time.Second

type healthCache struct {
	mu        sync.Mutex
	healthy   bool
	checkedAt time.Time
}

type App struct {
	config   Config
	dispatch DispatchFunc
	metrics  *Metrics
	server   *mcp.Server

	backends map[string]BackendProbe
	health   healthCache
}

// NewApp creates an App wired to a native Go dispatch function.
// Optionally pass backend probes to enable per-backend readyz health checks.
// metrics must be non-nil; main owns the instance so it can also be shared
// with buildBackends (so OllamaBackend.observe can route into the same
// counters).
func NewApp(config Config, dispatch DispatchFunc, backends map[string]BackendProbe, metrics *Metrics) *App {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    config.ServerName,
		Version: config.ServerVersion,
	}, nil)

	app := &App{
		config:   config,
		dispatch: dispatch,
		metrics:  metrics,
		server:   server,
		backends: backends,
	}

	for _, spec := range toolSpecs {
		registerTool(server, app, spec)
	}

	// Probe backends at startup so readyz has fresh state.
	if len(backends) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		app.reprobeBackends(ctx)
		cancel()
	} else {
		// No backends wired: assume healthy so dispatch-only deployments work.
		app.health.mu.Lock()
		app.health.healthy = true
		app.health.checkedAt = time.Now()
		app.health.mu.Unlock()
	}

	return app
}

func (a *App) Handler() http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return a.server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})

	mux := http.NewServeMux()
	mux.Handle(a.config.MCPPath, mcpHandler)
	mux.HandleFunc("/healthz", a.healthz)
	mux.HandleFunc("/readyz", a.readyz)
	mux.HandleFunc("/metricsz", a.metricsz)
	mux.HandleFunc("/", a.index)
	return mux
}

func registerTool(server *mcp.Server, app *App, spec ToolSpec) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        spec.Name,
		Description: spec.Description,
		InputSchema: toolInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ToolInput) (*mcp.CallToolResult, any, error) {
		token := req.Params.GetProgressToken()
		app.metrics.TotalRequests.Add(1)

		type callResult struct {
			text    string
			isError bool
		}
		done := make(chan callResult, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					app.metrics.DispatchErrors.Add(1)
					done <- callResult{text: fmt.Sprintf("internal error: %v", r), isError: true}
				}
			}()
			data, err := app.dispatch(ctx, spec, input)
			if err != nil {
				app.metrics.DispatchErrors.Add(1)
				done <- callResult{text: fmt.Sprintf("roundtable dispatch error: %v", err), isError: true}
				return
			}
			done <- callResult{text: string(data), isError: false}
		}()

		ticker := time.NewTicker(30 * time.Second)
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
				if token != nil && req.Session != nil {
					_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
						ProgressToken: token,
						Progress:      float64(ticks),
						Message:       "backend running",
					})
				}
			}
		}
	})
}

func (a *App) healthz(w http.ResponseWriter, _ *http.Request) {
	writePlain(w, http.StatusOK, "ok")
}

func (a *App) readyz(w http.ResponseWriter, r *http.Request) {
	a.health.mu.Lock()
	stale := time.Since(a.health.checkedAt) > healthCacheTTL
	a.health.mu.Unlock()

	if stale && len(a.backends) > 0 {
		a.reprobeBackends(r.Context())
	}

	a.health.mu.Lock()
	healthy := a.health.healthy
	a.health.mu.Unlock()

	if !healthy {
		writePlain(w, http.StatusServiceUnavailable, "not ready: one or more backends unhealthy")
		return
	}
	writePlain(w, http.StatusOK, "ready")
}

func (a *App) reprobeBackends(ctx context.Context) {
	allHealthy := true
	for name, b := range a.backends {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := b.Healthy(probeCtx); err != nil {
			log.Printf("backend %s unhealthy: %v", name, err)
			allHealthy = false
		}
		cancel()
	}
	a.health.mu.Lock()
	a.health.healthy = allHealthy
	a.health.checkedAt = time.Now()
	a.health.mu.Unlock()
}

func (a *App) metricsz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a.metrics.JSON())
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
