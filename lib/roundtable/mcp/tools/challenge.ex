defmodule Roundtable.MCP.Tools.Challenge do
  use Hermes.Server.Component, type: :tool
  @moduledoc "Run critical review consensus using codereviewer role across models."

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
    enhanced_params =
      Map.update!(params, :prompt, fn prompt ->
        prompt <> "\n\nAct as a critical reviewer. Find flaws, risks, and weaknesses."
      end)

    Roundtable.MCP.Tools.Common.dispatch(enhanced_params, %{role: "codereviewer"})
  end
end
