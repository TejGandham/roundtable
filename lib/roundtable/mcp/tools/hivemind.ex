defmodule Roundtable.MCP.Tools.Hivemind do
  use Hermes.Server.Component, type: :tool
  @moduledoc "Run multi-model consensus with default role across all models."

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
    Roundtable.MCP.Tools.Common.dispatch(params, %{role: "default"})
  end
end
