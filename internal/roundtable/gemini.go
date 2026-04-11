package roundtable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// GeminiBackend implements Backend for the Gemini CLI (subprocess-per-request).
type GeminiBackend struct {
	mu       sync.Mutex
	runner   *SubprocessRunner
	execPath string // resolved at Healthy() time
	model    string // default model, may be overridden per-request
}

func NewGeminiBackend(model string) *GeminiBackend {
	return &GeminiBackend{
		runner: &SubprocessRunner{},
		model:  model,
	}
}

func (g *GeminiBackend) Name() string { return "gemini" }

func (g *GeminiBackend) Start(_ context.Context) error { return nil }

func (g *GeminiBackend) Stop() error { return nil }

// Healthy resolves the executable and runs --version as a probe.
func (g *GeminiBackend) Healthy(ctx context.Context) error {
	path := ResolveExecutable("gemini")
	if path == "" {
		return fmt.Errorf("gemini executable not found")
	}

	g.mu.Lock()
	g.execPath = path
	g.mu.Unlock()

	out := g.runner.Probe(ctx, path, []string{"--version"})
	if out.ExitCode == nil || *out.ExitCode != 0 {
		return fmt.Errorf("gemini probe failed: exit_code=%v", out.ExitCode)
	}
	return nil
}

// Run executes a Gemini CLI request and returns a normalized Result.
func (g *GeminiBackend) Run(ctx context.Context, req Request) (*Result, error) {
	g.mu.Lock()
	ep := g.execPath
	g.mu.Unlock()
	if ep == "" {
		return NotFoundResult("gemini", req.Model), nil
	}

	args := geminiBuildArgs(req)
	raw := g.runner.Run(ctx, ep, args)
	parsed := geminiParseOutput(string(raw.Stdout), raw.Stderr)

	model := req.Model
	if model == "" {
		model = g.model
	}

	return BuildResult(raw, parsed, model), nil
}

// geminiBuildArgs produces CLI arguments matching gemini.ex build_args/2.
//
// Format: [-p, <prompt>, -o, json, --yolo, -m <model>?, --resume <resume>?]
// Resume flag comes first when present (matches Elixir ordering).
func geminiBuildArgs(req Request) []string {
	base := []string{"-p", req.Prompt, "-o", "json", "--yolo"}
	var modelArgs []string
	if req.Model != "" {
		modelArgs = []string{"-m", req.Model}
	}

	if req.Resume != "" {
		return append(append([]string{"--resume", req.Resume}, base...), modelArgs...)
	}
	return append(base, modelArgs...)
}

// geminiParseOutput parses Gemini CLI JSON output into ParsedOutput.
//
// Try order: stdout JSON -> stderr JSON -> raw text error.
// Rate limit detection: case-insensitive substring match for "429",
// "rate limit", "too many requests", "resource_exhausted", "quota".
func geminiParseOutput(stdout, stderr string) ParsedOutput {
	// Try stdout JSON first
	if data, err := geminiDecodeJSON(stdout); err == nil {
		return data
	}

	// Try stderr JSON fallback
	if data, err := geminiDecodeJSON(stderr); err == nil {
		return data
	}

	// Raw text fallback
	return geminiParseRawError(stdout, stderr)
}

func geminiDecodeJSON(text string) (ParsedOutput, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return ParsedOutput{}, err
	}

	// Check for error response
	if errObj, ok := data["error"]; ok {
		if errMap, ok := errObj.(map[string]any); ok {
			message := ""
			if m, ok := errMap["message"].(string); ok {
				message = m
			} else {
				b, _ := json.Marshal(errMap)
				message = string(b)
			}

			haystack := geminiErrorHaystack(message, errMap)
			status := "error"
			if geminiIsRateLimited(haystack) {
				status = "rate_limited"
			}

			return ParsedOutput{
				Response: geminiFormatError(message, status),
				Status:   status,
			}, nil
		}
	}

	// Success response
	response := ""
	if r, ok := data["response"].(string); ok {
		response = r
	}

	metadata := geminiExtractMetadata(data)

	var sessionID *string
	if sid, ok := data["session_id"].(string); ok {
		sessionID = &sid
	}

	return ParsedOutput{
		Response:  response,
		Status:    "ok",
		Metadata:  metadata,
		SessionID: sessionID,
	}, nil
}

func geminiParseRawError(stdout, stderr string) ParsedOutput {
	raw := strings.TrimSpace(stdout)
	if raw == "" {
		raw = strings.TrimSpace(stderr)
	}

	haystack := strings.ToLower(raw)
	status := "error"
	if geminiIsRateLimited(haystack) {
		status = "rate_limited"
	}

	var parseError *string
	if status != "rate_limited" {
		pe := "JSON parse failed"
		parseError = &pe
	}

	return ParsedOutput{
		Response:   geminiFormatError(raw, status),
		Status:     status,
		ParseError: parseError,
	}
}

// geminiIsRateLimited checks for rate limit indicators in normalized text.
func geminiIsRateLimited(text string) bool {
	normalized := strings.ToLower(text)
	return strings.Contains(normalized, "429") ||
		strings.Contains(normalized, "rate limit") ||
		strings.Contains(normalized, "too many requests") ||
		strings.Contains(normalized, "resource_exhausted") ||
		strings.Contains(normalized, "quota")
}

func geminiFormatError(message, status string) string {
	if status == "rate_limited" {
		suffix := ""
		if strings.TrimSpace(message) != "" {
			suffix = ": " + strings.TrimSpace(message)
		}
		return "Gemini rate limited (429/RESOURCE_EXHAUSTED). Retry later or resume the session" + suffix
	}
	return message
}

// geminiErrorHaystack builds the string used for rate limit substring search.
// Combines message, status, and code fields from the error object.
func geminiErrorHaystack(message string, errMap map[string]any) string {
	parts := []string{message}
	if s, ok := errMap["status"].(string); ok {
		parts = append(parts, s)
	}
	if c, ok := errMap["code"]; ok {
		parts = append(parts, fmt.Sprint(c))
	}
	return strings.ToLower(strings.Join(parts, " "))
}

// geminiExtractMetadata extracts model name and token count from
// stats.models.<first_key>.tokens.
func geminiExtractMetadata(data map[string]any) map[string]any {
	stats, ok := data["stats"].(map[string]any)
	if !ok {
		return nil
	}
	models, ok := stats["models"].(map[string]any)
	if !ok || len(models) == 0 {
		return nil
	}

	// Get first model key
	var modelName string
	for k := range models {
		modelName = k
		break
	}

	meta := map[string]any{"model_used": modelName}
	if modelData, ok := models[modelName].(map[string]any); ok {
		if tokens, ok := modelData["tokens"]; ok {
			meta["tokens"] = tokens
		}
	}

	return meta
}
