defmodule Roundtable.Dispatcher do
  @moduledoc "Dispatches CLI execution in parallel using Task.Supervisor."

  alias Roundtable.CLI.Runner
  alias Roundtable.Output

  @probe_timeout_ms 5_000

  @type cli_config :: %{
          name: String.t(),
          module: module(),
          path: String.t() | nil,
          model: String.t() | nil,
          args: map(),
          prompt: String.t()
        }

  @spec dispatch(map()) :: map()
  def dispatch(%{cli_configs: cli_configs, timeout_ms: timeout_ms}) do
    {:ok, sup} = Task.Supervisor.start_link()

    try do
      cli_configs_with_paths =
        Enum.map(cli_configs, fn cli_config ->
          path =
            if Map.has_key?(cli_config, :path) do
              Map.get(cli_config, :path)
            else
              Runner.resolve_executable(Map.get(cli_config, :cli, cli_config.name))
            end

          Map.put(cli_config, :path, path)
        end)

      probe_tasks =
        cli_configs_with_paths
        |> Enum.filter(&(&1.path != nil))
        |> Enum.map(fn cli_config ->
          task =
            Task.Supervisor.async(sup, fn ->
              Runner.probe_cli(cli_config.path, cli_config.module.probe_args(), @probe_timeout_ms)
            end)

          {cli_config, task}
        end)

      probe_results =
        probe_tasks
        |> Enum.map(fn {cli_config, task} ->
          probe_result =
            case Task.yield(task, @probe_timeout_ms + 1_000) do
              {:ok, result} ->
                result

              nil ->
                Task.shutdown(task, :brutal_kill)
                %{alive: false, reason: "probe timeout"}

              {:exit, reason} ->
                %{alive: false, reason: inspect(reason)}
            end

          {cli_config, probe_result}
        end)

      run_tasks =
        cli_configs_with_paths
        |> Enum.map(fn cli_config ->
          probe_result =
            case Enum.find(probe_results, fn {cfg, _} -> cfg.name == cli_config.name end) do
              {_, result} -> result
              nil -> nil
            end

          healthy = cli_config.path != nil and (probe_result == nil or probe_result.alive)

          if healthy do
            cli_args = cli_config.module.build_args(cli_config.args, cli_config.prompt)

            task =
              Task.Supervisor.async(sup, fn ->
                Runner.run_cli(cli_config.path, cli_args, timeout_ms)
              end)

            {cli_config, probe_result, task}
          else
            {cli_config, probe_result, nil}
          end
        end)

      results =
        Enum.map(run_tasks, fn
          {cli_config, probe_result, nil} ->
            result =
              Output.build_result(
                cli_config.name,
                cli_config.path,
                cli_config.model,
                probe_result,
                nil,
                cli_config.module
              )

            {cli_config.name, result}

          {cli_config, probe_result, task} ->
            run_result = Task.await(task, timeout_ms + 10_000)

            result =
              Output.build_result(
                cli_config.name,
                cli_config.path,
                cli_config.model,
                probe_result,
                run_result,
                cli_config.module
              )

            {cli_config.name, result}
        end)
        |> Map.new()

      meta =
        Output.build_meta(results, cli_configs)

      Map.put(results, "meta", meta)
    after
      if function_exported?(Task.Supervisor, :stop, 1) do
        apply(Task.Supervisor, :stop, [sup])
      else
        GenServer.stop(sup)
      end
    end
  end
end
