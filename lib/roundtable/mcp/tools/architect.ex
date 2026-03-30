defmodule Roundtable.MCP.Tools.Architect do
  use Hermes.Server.Component, type: :tool
  @moduledoc "Run multi-model hivemind consensus (all models use default role)"

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
  def execute(_params, _frame) do
    {:ok, "stub - not yet implemented"}
  end
end
