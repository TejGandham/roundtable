package stdiomcp

import "context"

// ToolInput is the MCP tool input schema shared by all five Roundtable
// tools. Mirrored from internal/httpmcp/backend.go for Phase A — Phase C1
// deletes the httpmcp copy.
type ToolInput struct {
	Prompt       string `json:"prompt"`
	Files        string `json:"files,omitempty"`
	Timeout      *int   `json:"timeout,omitempty"`
	GeminiModel  string `json:"gemini_model,omitempty"`
	CodexModel   string `json:"codex_model,omitempty"`
	ClaudeModel  string `json:"claude_model,omitempty"`
	GeminiResume string `json:"gemini_resume,omitempty"`
	CodexResume  string `json:"codex_resume,omitempty"`
	ClaudeResume string `json:"claude_resume,omitempty"`
	Agents       string `json:"agents,omitempty"`
}

type ToolSpec struct {
	Name         string
	Description  string
	PromptSuffix string
	Role         string
	GeminiRole   string
	CodexRole    string
	ClaudeRole   string
}

// DispatchFunc is the transport-agnostic dispatch entry point. Both the
// stdio and (legacy) HTTP servers call the same signature.
type DispatchFunc func(ctx context.Context, spec ToolSpec, input ToolInput) ([]byte, error)
