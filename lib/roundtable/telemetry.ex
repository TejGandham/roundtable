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

    gemini = Map.get(results, "gemini", %{})
    codex = Map.get(results, "codex", %{})
    meta = Map.get(results, "meta", %{})

    resource_attrs = parse_resource_attributes()

    trace_id = random_hex(32)
    root_span_id = random_hex(16)
    gemini_span_id = random_hex(16)
    codex_span_id = random_hex(16)

    status_code = if gemini["status"] == "ok" or codex["status"] == "ok", do: 1, else: 2
    gemini_status_code = if gemini["status"] == "ok", do: 1, else: 2
    codex_status_code = if codex["status"] == "ok", do: 1, else: 2

    gemini_elapsed_ms = gemini["elapsed_ms"] || 0
    codex_elapsed_ms = codex["elapsed_ms"] || 0
    gemini_start_ns = now_unix_ns - gemini_elapsed_ms * 1_000_000
    codex_start_ns = now_unix_ns - codex_elapsed_ms * 1_000_000

    root_span = %{
      "traceId" => trace_id,
      "spanId" => root_span_id,
      "name" => "roundtable.invoke",
      "kind" => 3,
      "startTimeUnixNano" => Integer.to_string(start_unix_ns),
      "endTimeUnixNano" => Integer.to_string(now_unix_ns),
      "status" => %{"code" => status_code},
      "attributes" => build_attributes(gemini, codex, meta, args)
    }

    gemini_span = %{
      "traceId" => trace_id,
      "spanId" => gemini_span_id,
      "parentSpanId" => root_span_id,
      "name" => "roundtable.gemini",
      "kind" => 3,
      "startTimeUnixNano" => Integer.to_string(gemini_start_ns),
      "endTimeUnixNano" => Integer.to_string(now_unix_ns),
      "status" => %{"code" => gemini_status_code},
      "attributes" => [
        otel_str("roundtable.gemini.status", gemini["status"] || "unknown"),
        otel_str("roundtable.gemini.model", gemini["model"] || "unknown")
      ]
    }

    codex_span = %{
      "traceId" => trace_id,
      "spanId" => codex_span_id,
      "parentSpanId" => root_span_id,
      "name" => "roundtable.codex",
      "kind" => 3,
      "startTimeUnixNano" => Integer.to_string(codex_start_ns),
      "endTimeUnixNano" => Integer.to_string(now_unix_ns),
      "status" => %{"code" => codex_status_code},
      "attributes" => [
        otel_str("roundtable.codex.status", codex["status"] || "unknown"),
        otel_str("roundtable.codex.model", codex["model"] || "unknown")
      ]
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
              "spans" => [root_span, gemini_span, codex_span]
            }
          ]
        }
      ]
    }
  end

  defp build_attributes(gemini, codex, meta, args) do
    [
      otel_str("roundtable.role", Map.get(args, :role) || "default"),
      otel_str(
        "roundtable.gemini.role",
        meta["gemini_role"] || Map.get(args, :role) || "default"
      ),
      otel_str("roundtable.codex.role", meta["codex_role"] || Map.get(args, :role) || "default"),
      otel_str("roundtable.gemini.status", gemini["status"] || "unknown"),
      otel_str("roundtable.codex.status", codex["status"] || "unknown"),
      otel_int("roundtable.gemini.elapsed_ms", gemini["elapsed_ms"] || 0),
      otel_int("roundtable.codex.elapsed_ms", codex["elapsed_ms"] || 0),
      otel_int("roundtable.total_elapsed_ms", meta["total_elapsed_ms"] || 0),
      otel_int("roundtable.files_count", length(Map.get(args, :files) || [])),
      otel_str("roundtable.gemini.model", gemini["model"] || "unknown"),
      otel_str("roundtable.codex.model", codex["model"] || "unknown")
    ]
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
