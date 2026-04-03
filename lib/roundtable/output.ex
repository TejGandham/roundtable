defmodule Roundtable.Output do
  @moduledoc "Builds normalized result maps and encodes final JSON output."

  @spec build_result(
          cli_name :: String.t(),
          path :: String.t() | nil,
          model :: String.t() | nil,
          probe_result :: map() | nil,
          run_result :: map() | nil,
          cli_module :: module()
        ) :: map()
  def build_result(cli_name, nil, model, _probe_result, _run_result, _cli_module) do
    %{
      "response" => "",
      "model" => model || "cli-default",
      "status" => "not_found",
      "exit_code" => nil,
      "exit_signal" => nil,
      "stderr" => "#{cli_name} CLI not found in PATH",
      "elapsed_ms" => 0,
      "parse_error" => nil,
      "truncated" => false,
      "stderr_truncated" => false,
      "session_id" => nil
    }
  end

  def build_result(cli_name, _path, model, %{alive: false} = probe_result, nil, _cli_module) do
    reason = Map.get(probe_result, :reason, "unknown")
    cli_lower = String.downcase(cli_name)

    %{
      "response" => "",
      "model" => model || "cli-default",
      "status" => "probe_failed",
      "exit_code" => Map.get(probe_result, :exit_code),
      "exit_signal" => nil,
      "stderr" =>
        "#{cli_name} CLI probe failed: #{reason}. Run #{cli_lower} --version to diagnose.",
      "elapsed_ms" => 0,
      "parse_error" => nil,
      "truncated" => false,
      "stderr_truncated" => false,
      "session_id" => nil
    }
  end

  def build_result(_cli_name, _path, model, _probe_result, raw, cli_module) do
    parsed = cli_module.parse_output(raw.stdout, raw.stderr)

    timeout_message =
      if raw.timed_out do
        timeout_seconds = max(1, div(raw.elapsed_ms + 999, 1_000))

        "Request timed out after #{timeout_seconds}s. Retry with a longer timeout or resume the session."
      end

    status =
      cond do
        raw.timed_out -> "timeout"
        raw.exit_signal != nil -> "terminated"
        raw.exit_code != nil and raw.exit_code != 0 and parsed.status == :ok -> "error"
        true -> to_string(parsed.status)
      end

    model_used = Map.get(parsed.metadata, :model_used) || model || "cli-default"

    %{
      "response" => timeout_message || parsed.response,
      "model" => model_used,
      "status" => status,
      "exit_code" => raw.exit_code,
      "exit_signal" => raw.exit_signal,
      "stderr" => raw.stderr,
      "elapsed_ms" => raw.elapsed_ms,
      "parse_error" => if(raw.timed_out, do: nil, else: parsed.parse_error),
      "truncated" => raw.truncated,
      "stderr_truncated" => raw.stderr_truncated,
      "session_id" => parsed.session_id
    }
  end

  @spec build_meta(results :: map(), cli_configs :: [map()]) :: map()
  def build_meta(results, cli_configs) do
    files =
      case List.first(cli_configs) do
        %{files: files} -> files
        _ -> []
      end

    roles =
      Enum.reduce(cli_configs, %{}, fn cfg, acc ->
        Map.put(acc, "#{cfg.name}_role", cfg.role)
      end)

    total_elapsed_ms =
      results
      |> Map.delete("meta")
      |> Map.values()
      |> Enum.map(&(Map.get(&1, "elapsed_ms") || 0))
      |> Enum.max(fn -> 0 end)

    %{
      "total_elapsed_ms" => total_elapsed_ms,
      "files_referenced" => files
    }
    |> Map.merge(roles)
  end

  @spec encode(map()) :: String.t()
  def encode(results) do
    Jason.encode!(results, pretty: true)
  end
end
