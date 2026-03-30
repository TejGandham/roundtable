defmodule Roundtable.MCP.Tools.Common do
  @moduledoc "Shared dispatch logic for MCP tool execute callbacks."

  @spec dispatch(map(), map()) :: {:ok, String.t()} | {:error, String.t()}
  def dispatch(params, role_config) do
    files = parse_files(Map.get(params, :files))
    timeout = Map.get(params, :timeout) || 900

    args = %{
      prompt: params.prompt,
      role: Map.get(role_config, :role, "default"),
      gemini_role: Map.get(role_config, :gemini_role),
      codex_role: Map.get(role_config, :codex_role),
      claude_role: Map.get(role_config, :claude_role),
      files: files,
      gemini_model: Map.get(params, :gemini_model),
      codex_model: Map.get(params, :codex_model),
      claude_model: Map.get(params, :claude_model),
      timeout: timeout,
      roles_dir: default_roles_dir(),
      project_roles_dir: nil,
      codex_reasoning: nil,
      gemini_resume: Map.get(params, :gemini_resume),
      codex_resume: Map.get(params, :codex_resume),
      claude_resume: Map.get(params, :claude_resume)
    }

    Roundtable.run(args)
  end

  defp parse_files(nil), do: []
  defp parse_files(""), do: []

  defp parse_files(files_str) do
    files_str
    |> String.split(",")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(&(&1 == ""))
  end

  defp default_roles_dir do
    case :code.priv_dir(:roundtable) do
      {:error, _} -> Path.expand("roles")
      priv_dir -> Path.join(to_string(priv_dir), "roles")
    end
  end
end
