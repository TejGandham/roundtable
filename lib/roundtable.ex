defmodule Roundtable do
  alias Roundtable.Dispatcher
  alias Roundtable.Output
  alias Roundtable.Prompt.Assembler
  alias Roundtable.Prompt.Roles
  alias Roundtable.Telemetry

  @spec main([String.t()]) :: no_return()
  def main(args), do: Roundtable.CLI.main(args)

  @cli_modules %{
    "gemini" => Roundtable.CLI.Gemini,
    "codex" => Roundtable.CLI.Codex,
    "claude" => Roundtable.CLI.Claude
  }

  @spec run(map()) :: {:ok, String.t()} | {:error, String.t()}
  def run(args) do
    start_time = System.monotonic_time(:millisecond)
    agent_specs = agents_or_default(args)

    cli_configs =
      try do
        Enum.map(agent_specs, fn spec ->
          role = spec.role || resolve_default_role(spec.cli, args)
          role_prompt = Roles.load_role_prompt(role, args.roles_dir, args.project_roles_dir)
          prompt = Assembler.assemble(role_prompt, args.prompt, args.files)

          %{
            name: spec.name,
            cli: spec.cli,
            module: Map.fetch!(@cli_modules, spec.cli),
            model: spec.model || resolve_default_model(spec.cli, args),
            role: role,
            files: args.files,
            args: build_agent_args(spec, args),
            prompt: prompt
          }
        end)
      rescue
        e in RuntimeError ->
          {:error, Exception.message(e)}
      end

    case cli_configs do
      {:error, msg} ->
        {:error, msg}

      configs when is_list(configs) ->
        timeout_ms = args.timeout * 1_000

        results =
          Dispatcher.dispatch(%{
            cli_configs: configs,
            timeout_ms: timeout_ms
          })

        Telemetry.emit(results, args, start_time)
        {:ok, Output.encode(results)}
    end
  end

  defp agents_or_default(args) do
    case Map.get(args, :agents) do
      nil ->
        [
          %{name: "gemini", cli: "gemini", model: nil, role: nil, resume: nil},
          %{name: "codex", cli: "codex", model: nil, role: nil, resume: nil},
          %{name: "claude", cli: "claude", model: nil, role: nil, resume: nil}
        ]

      agents ->
        agents
    end
  end

  defp resolve_default_role(cli, args) do
    case cli do
      "gemini" -> args.gemini_role || args.role
      "codex" -> args.codex_role || args.role
      "claude" -> args.claude_role || args.role
      _ -> args.role
    end
  end

  defp resolve_default_model(cli, args) do
    case cli do
      "gemini" -> args.gemini_model
      "codex" -> args.codex_model
      "claude" -> args.claude_model
      _ -> nil
    end
  end

  @cli_resume_keys %{
    "gemini" => :gemini_resume,
    "codex" => :codex_resume,
    "claude" => :claude_resume
  }

  @cli_model_keys %{
    "gemini" => :gemini_model,
    "codex" => :codex_model,
    "claude" => :claude_model
  }

  defp build_agent_args(spec, args) do
    resume_key = Map.fetch!(@cli_resume_keys, spec.cli)
    model_key = Map.fetch!(@cli_model_keys, spec.cli)
    default_resume = Map.get(args, resume_key)

    args
    |> Map.put(model_key, spec.model || resolve_default_model(spec.cli, args))
    |> Map.put(resume_key, spec.resume || default_resume)
  end
end
