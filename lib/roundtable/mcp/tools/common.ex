defmodule Roundtable.MCP.Tools.Common do
  @moduledoc "Shared dispatch logic for MCP tool execute callbacks."

  alias Hermes.Server.Response

  @doc """
  Dispatches an MCP tool call and returns a Hermes-compatible response.

  Returns `{:reply, %Response{}, frame}` on success or
  `{:reply, %Response{isError: true}, frame}` on failure, matching
  what `Hermes.Server.Handlers.Tools.forward_to/4` expects.
  """
  @spec dispatch(map(), map(), term()) ::
          {:reply, Response.t(), term()}
  def dispatch(params, role_config, frame) do
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

    case Roundtable.run(args) do
      {:ok, json} ->
        response = Response.tool() |> Response.text(json)
        {:reply, response, frame}

      {:error, message} ->
        response = Response.tool() |> Response.error(message)
        {:reply, response, frame}
    end
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
