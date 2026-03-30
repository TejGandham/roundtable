defmodule Roundtable.MCP.Tools.Xray do
  use Hermes.Server.Component, type: :tool
  @moduledoc "Run architecture and quality xray with per-model role assignments."

  schema do
    field(:prompt, :string, required: true)
    field(:files, :string)
    field(:timeout, :integer)
    field(:gemini_model, :string)
    field(:codex_model, :string)
    field(:claude_model, :string)
    field(:gemini_resume, :string)
    field(:codex_resume, :string)
    field(:claude_resume, :string)
  end

  @impl true
  def execute(params, _frame) do
    Roundtable.MCP.Tools.Common.dispatch(params, %{
      role: nil,
      gemini_role: "planner",
      codex_role: "codereviewer",
      claude_role: "default"
    })
  end
end
