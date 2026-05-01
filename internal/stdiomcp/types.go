package stdiomcp

import (
	"context"
	"encoding/json"
)

// ToolInput is the MCP tool input schema shared by all five Roundtable
// tools.
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
	// Schema is an optional JSON-Schema-lite document (per F01) carried
	// verbatim from the MCP wire as RawMessage. Absent / null / empty is
	// treated as "no schema": no parse, no prompt suffix append, no
	// validation. The dispatch glue (cmd/roundtable/main.go) parses this
	// before invoking roundtable.Run; a parse failure surfaces as
	// IsError: true before any backend is invoked.
	Schema json.RawMessage `json:"schema,omitempty"`
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
