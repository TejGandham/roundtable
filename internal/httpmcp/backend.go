package httpmcp

// ToolInput is the MCP tool input schema shared by all five Roundtable tools.
// The tool handler deserializes the MCP request params into this struct before
// passing it to the dispatch function.
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

// ToolSpec describes a static tool registration. Each of the five Roundtable
// tools (hivemind, deepdive, architect, challenge, xray) has a ToolSpec that
// captures its name, description, per-CLI roles, and prompt suffix.
type ToolSpec struct {
	Name         string
	Description  string
	PromptSuffix string
	Role         string
	GeminiRole   string
	CodexRole    string
	ClaudeRole   string
}
