defmodule Roundtable.MCP.Tools.Architect do
  @moduledoc "Generate implementation architecture with planner role across models."
  use Hermes.Server.Component, type: :tool

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
    field(:agents, :string)
  end

  @impl true
  def execute(params, frame) do
    enhanced_params =
      Map.update!(params, :prompt, fn prompt ->
        prompt <> "\n\nProvide phases, dependencies, risks, and milestones."
      end)

    Roundtable.MCP.Tools.Common.dispatch(enhanced_params, %{role: "planner"}, frame)
  end
end
