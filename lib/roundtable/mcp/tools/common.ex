defmodule Roundtable.MCP.Tools.Common do
  @moduledoc "Shared dispatch logic for MCP tool execute callbacks."

  require Logger
  alias Hermes.Server.Response

  @doc """
  Dispatches an MCP tool call and returns a Hermes-compatible response.

  Returns `{:reply, %Response{}, frame}` on success or
  `{:reply, %Response{isError: true}, frame}` on failure, matching
  what `Hermes.Server.Handlers.Tools.forward_to/4` expects.
  """
  @valid_clis ~w(gemini codex claude)

  @spec dispatch(map(), map(), term()) ::
          {:reply, Response.t(), term()}
  def dispatch(params, role_config, frame) do
    files = parse_files(Map.get(params, :files))

    with {:ok, timeout} <- normalize_timeout(Map.get(params, :timeout)),
         {:ok, agents} <- parse_agents(Map.get(params, :agents)) do
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
        claude_resume: Map.get(params, :claude_resume),
        agents: agents
      }

      case Roundtable.run(args) do
        {:ok, json} ->
          response = Response.tool() |> Response.text(json)
          {:reply, response, frame}

        {:error, message} ->
          response = Response.tool() |> Response.error(to_string(message))
          {:reply, response, frame}
      end
    else
      {:error, message} ->
        response = Response.tool() |> Response.error(to_string(message))
        {:reply, response, frame}
    end
  rescue
    e ->
      Logger.error(Exception.format(:error, e, __STACKTRACE__))
      response = Response.tool() |> Response.error(Exception.message(e))
      {:reply, response, frame}
  catch
    kind, reason ->
      Logger.error(Exception.format(kind, reason, __STACKTRACE__))
      response = Response.tool() |> Response.error("#{kind}: #{inspect(reason)}")
      {:reply, response, frame}
  end

  @doc false
  def parse_agents(nil), do: {:ok, nil}
  def parse_agents(""), do: {:ok, nil}

  def parse_agents(agents_str) when is_binary(agents_str) do
    case Jason.decode(agents_str) do
      {:ok, agents} when is_list(agents) -> validate_agents(agents)
      {:ok, _} -> {:error, "agents must be a JSON array"}
      {:error, _} -> {:error, "agents is not valid JSON"}
    end
  end

  def parse_agents(agents) when is_list(agents), do: validate_agents(agents)

  defp validate_agents([]), do: {:error, "agents list cannot be empty"}

  @reserved_names ~w(meta)

  defp validate_agents(agents) do
    with :ok <- validate_agent_entries(agents),
         :ok <- validate_unique_names(agents),
         :ok <- validate_reserved_names(agents) do
      normalized =
        Enum.map(agents, fn agent ->
          cli = agent["cli"]
          name = agent["name"] || cli

          %{
            name: name,
            cli: cli,
            model: agent["model"],
            role: agent["role"],
            resume: agent["resume"]
          }
        end)

      {:ok, normalized}
    end
  end

  defp validate_agent_entries(agents) do
    Enum.reduce_while(agents, :ok, fn agent, :ok ->
      cli = agent["cli"]

      cond do
        not is_map(agent) ->
          {:halt, {:error, "each agent entry must be a JSON object"}}

        is_nil(cli) or cli == "" ->
          {:halt, {:error, "each agent must specify a \"cli\" field"}}

        cli not in @valid_clis ->
          {:halt, {:error, "unknown CLI type: #{cli}. Valid types: #{Enum.join(@valid_clis, ", ")}"}}

        not optional_string?(agent["name"]) ->
          {:halt, {:error, "agent \"name\" must be a string or null"}}

        not optional_string?(agent["model"]) ->
          {:halt, {:error, "agent \"model\" must be a string or null"}}

        not optional_string?(agent["role"]) ->
          {:halt, {:error, "agent \"role\" must be a string or null"}}

        not optional_string?(agent["resume"]) ->
          {:halt, {:error, "agent \"resume\" must be a string or null"}}

        true ->
          {:cont, :ok}
      end
    end)
  end

  defp optional_string?(nil), do: true
  defp optional_string?(v) when is_binary(v), do: true
  defp optional_string?(_), do: false

  defp validate_unique_names(agents) do
    names = Enum.map(agents, fn a -> a["name"] || a["cli"] end)
    dupes = names -- Enum.uniq(names)

    if dupes == [] do
      :ok
    else
      {:error, "duplicate agent names: #{Enum.join(Enum.uniq(dupes), ", ")}"}
    end
  end

  defp validate_reserved_names(agents) do
    names = Enum.map(agents, fn a -> a["name"] || a["cli"] end)
    reserved = Enum.filter(names, &(&1 in @reserved_names))

    if reserved == [] do
      :ok
    else
      {:error, "agent name #{inspect(hd(reserved))} is reserved"}
    end
  end

  defp parse_files(nil), do: []
  defp parse_files(""), do: []
  defp parse_files(files) when is_list(files), do: Enum.filter(files, &is_binary/1)

  defp parse_files(files_str) when is_binary(files_str) do
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

  defp normalize_timeout(nil), do: {:ok, 900}

  defp normalize_timeout(timeout) when is_integer(timeout) and timeout >= 1 and timeout <= 900,
    do: {:ok, timeout}

  defp normalize_timeout(_), do: {:error, "timeout must be an integer between 1 and 900 seconds"}
end
