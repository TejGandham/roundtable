defmodule Roundtable.Telemetry do
  @moduledoc """
  Optional OTEL telemetry. Opt-in: set OTEL_EXPORTER_OTLP_ENDPOINT.
  Emits three spans per invocation (root + per-CLI children) to /v1/traces.
  Fire-and-forget.
  """

  @service_name "roundtable"

  @spec emit(map(), map(), integer()) :: :ok
  def emit(results, args, start_time_ms) do
    case System.get_env("OTEL_EXPORTER_OTLP_ENDPOINT") do
      nil ->
        :ok

      endpoint ->
        task =
          Task.async(fn ->
            try do
              span = build_span(results, args, start_time_ms)
              post_span(endpoint, span)
            rescue
              _ -> :ok
            catch
              _, _ -> :ok
            end
          end)

        # Give telemetry up to 2s to transmit before System.halt kills the VM
        Task.yield(task, 2_000) || Task.shutdown(task, :brutal_kill)
        :ok
    end
  end

  @doc false
  @spec build_span(map(), map(), integer()) :: map()
  def build_span(results, args, start_time_ms) do
    end_time_ms = System.monotonic_time(:millisecond)
    now_unix_ns = System.os_time(:nanosecond)
    duration_ms = end_time_ms - start_time_ms
    start_unix_ns = now_unix_ns - duration_ms * 1_000_000

    meta = Map.get(results, "meta", %{})
    agents = results |> Map.delete("meta") |> Enum.to_list()

    resource_attrs = parse_resource_attributes()

    trace_id = random_hex(32)
    root_span_id = random_hex(16)

    status_code = if Enum.any?(agents, fn {_, r} -> r["status"] == "ok" end), do: 1, else: 2

    agent_spans =
      Enum.map(agents, fn {name, agent_result} ->
        agent_span_id = random_hex(16)
        agent_elapsed_ms = agent_result["elapsed_ms"] || 0
        agent_start_ns = now_unix_ns - agent_elapsed_ms * 1_000_000
        agent_status_code = if agent_result["status"] == "ok", do: 1, else: 2

        %{
          "traceId" => trace_id,
          "spanId" => agent_span_id,
          "parentSpanId" => root_span_id,
          "name" => "roundtable.#{name}",
          "kind" => 3,
          "startTimeUnixNano" => Integer.to_string(agent_start_ns),
          "endTimeUnixNano" => Integer.to_string(now_unix_ns),
          "status" => %{"code" => agent_status_code},
          "attributes" => [
            otel_str("roundtable.#{name}.status", agent_result["status"] || "unknown"),
            otel_str("roundtable.#{name}.model", agent_result["model"] || "unknown")
          ]
        }
      end)

    root_span = %{
      "traceId" => trace_id,
      "spanId" => root_span_id,
      "name" => "roundtable.invoke",
      "kind" => 3,
      "startTimeUnixNano" => Integer.to_string(start_unix_ns),
      "endTimeUnixNano" => Integer.to_string(now_unix_ns),
      "status" => %{"code" => status_code},
      "attributes" => build_attributes(agents, meta, args)
    }

    %{
      "resourceSpans" => [
        %{
          "resource" => %{
            "attributes" =>
              [
                otel_str("service.name", @service_name)
              ] ++ resource_attrs
          },
          "scopeSpans" => [
            %{
              "scope" => %{"name" => @service_name, "version" => "1.0.0"},
              "spans" => [root_span | agent_spans]
            }
          ]
        }
      ]
    }
  end

  defp build_attributes(agents, meta, args) do
    [
      otel_str("roundtable.role", Map.get(args, :role) || "default"),
      otel_int("roundtable.total_elapsed_ms", meta["total_elapsed_ms"] || 0),
      otel_int("roundtable.files_count", length(Map.get(args, :files) || []))
    ] ++
      Enum.flat_map(agents, fn {name, agent_result} ->
        [
          otel_str(
            "roundtable.#{name}.role",
            meta["#{name}_role"] || Map.get(args, :role) || "default"
          ),
          otel_str("roundtable.#{name}.status", agent_result["status"] || "unknown"),
          otel_int("roundtable.#{name}.elapsed_ms", agent_result["elapsed_ms"] || 0),
          otel_str("roundtable.#{name}.model", agent_result["model"] || "unknown")
        ]
      end)
  end

  defp parse_resource_attributes do
    case System.get_env("OTEL_RESOURCE_ATTRIBUTES") do
      nil ->
        []

      str ->
        str
        |> String.split(",")
        |> Enum.flat_map(fn pair ->
          case String.split(pair, "=", parts: 2) do
            [k, v] -> [otel_str(String.trim(k), String.trim(v))]
            _ -> []
          end
        end)
    end
  end

  defp post_span(endpoint, span) do
    Application.ensure_all_started(:inets)
    url = String.trim_trailing(endpoint, "/") <> "/v1/traces"
    body = Jason.encode!(span)

    headers = [
      {~c"Content-Type", ~c"application/json"},
      {~c"User-Agent", ~c"roundtable-elixir/1.0"}
    ]

    :httpc.request(
      :post,
      {String.to_charlist(url), headers, ~c"application/json", String.to_charlist(body)},
      [{:timeout, 3000}, {:connect_timeout, 2000}],
      []
    )

    :ok
  end

  defp random_hex(len) do
    :crypto.strong_rand_bytes(div(len, 2)) |> Base.encode16(case: :lower)
  end

  defp otel_str(key, value),
    do: %{"key" => key, "value" => %{"stringValue" => to_string(value)}}

  defp otel_int(key, value),
    do: %{"key" => key, "value" => %{"intValue" => Integer.to_string(value)}}
end
