defmodule Roundtable.TelemetryTest do
  use ExUnit.Case, async: true
  alias Roundtable.Telemetry

  defp sample_results do
    %{
      "gemini" => %{"status" => "ok", "elapsed_ms" => 5000, "model" => "gemini-2.5-pro"},
      "codex" => %{
        "status" => "timeout",
        "elapsed_ms" => 30_000,
        "model" => "cli-default"
      },
      "meta" => %{
        "gemini_role" => "planner",
        "codex_role" => "planner",
        "total_elapsed_ms" => 30_000,
        "files_referenced" => ["a.ts", "b.ts"]
      }
    }
  end

  defp sample_args do
    %{role: "planner", gemini_role: "planner", codex_role: "planner", files: ["a.ts", "b.ts"]}
  end

  defp span_by_name(spans, name) do
    Enum.find(spans, fn span -> span["name"] == name end)
  end

  test "build_span produces valid OTEL structure with 3 spans" do
    start_ms = System.monotonic_time(:millisecond) - 5000
    span = Telemetry.build_span(sample_results(), sample_args(), start_ms)
    assert Map.has_key?(span, "resourceSpans")
    [rs] = span["resourceSpans"]
    [ss] = rs["scopeSpans"]
    spans = ss["spans"]
    assert length(spans) == 3

    root = span_by_name(spans, "roundtable.invoke")
    gemini_child = span_by_name(spans, "roundtable.gemini")
    codex_child = span_by_name(spans, "roundtable.codex")

    assert root["name"] == "roundtable.invoke"
    assert root["traceId"] |> String.length() == 32
    assert root["spanId"] |> String.length() == 16
    refute Map.has_key?(root, "parentSpanId")

    assert gemini_child["parentSpanId"] == root["spanId"]
    assert codex_child["parentSpanId"] == root["spanId"]

    assert gemini_child["traceId"] == root["traceId"]
    assert codex_child["traceId"] == root["traceId"]

    # gemini ok -> root status 1
    assert root["status"]["code"] == 1
  end

  test "root span attributes include all required fields" do
    start_ms = System.monotonic_time(:millisecond) - 1000
    span = Telemetry.build_span(sample_results(), sample_args(), start_ms)

    [root | _children] =
      span["resourceSpans"]
      |> hd()
      |> get_in(["scopeSpans", Access.at(0), "spans"])

    keys = Enum.map(root["attributes"], & &1["key"])
    assert "roundtable.role" in keys
    assert "roundtable.gemini.status" in keys
    assert "roundtable.codex.status" in keys
    assert "roundtable.gemini.elapsed_ms" in keys
    assert "roundtable.total_elapsed_ms" in keys
    assert "roundtable.files_count" in keys
  end

  test "status code is error when both fail" do
    results = %{
      "gemini" => %{"status" => "timeout", "elapsed_ms" => 0, "model" => "x"},
      "codex" => %{"status" => "error", "elapsed_ms" => 0, "model" => "x"},
      "meta" => %{
        "total_elapsed_ms" => 0,
        "gemini_role" => "default",
        "codex_role" => "default",
        "files_referenced" => []
      }
    }

    span =
      Telemetry.build_span(
        results,
        %{role: "default", files: []},
        System.monotonic_time(:millisecond)
      )

    spans = span["resourceSpans"] |> hd() |> get_in(["scopeSpans", Access.at(0), "spans"])
    root = span_by_name(spans, "roundtable.invoke")
    gemini_child = span_by_name(spans, "roundtable.gemini")
    codex_child = span_by_name(spans, "roundtable.codex")

    assert root["status"]["code"] == 2
    assert gemini_child["status"]["code"] == 2
    assert codex_child["status"]["code"] == 2
  end

  test "child spans have correct names and parentSpanId" do
    start_ms = System.monotonic_time(:millisecond) - 1000
    span = Telemetry.build_span(sample_results(), sample_args(), start_ms)

    spans = span["resourceSpans"] |> hd() |> get_in(["scopeSpans", Access.at(0), "spans"])
    root = span_by_name(spans, "roundtable.invoke")
    gemini_child = span_by_name(spans, "roundtable.gemini")
    codex_child = span_by_name(spans, "roundtable.codex")

    assert gemini_child["name"] == "roundtable.gemini"
    assert codex_child["name"] == "roundtable.codex"
    assert gemini_child["parentSpanId"] == root["spanId"]
    assert codex_child["parentSpanId"] == root["spanId"]
    assert gemini_child["spanId"] |> String.length() == 16
    assert codex_child["spanId"] |> String.length() == 16
    assert gemini_child["kind"] == 3
    assert codex_child["kind"] == 3

    gemini_keys = Enum.map(gemini_child["attributes"], & &1["key"])
    assert "roundtable.gemini.status" in gemini_keys
    assert "roundtable.gemini.model" in gemini_keys

    codex_keys = Enum.map(codex_child["attributes"], & &1["key"])
    assert "roundtable.codex.status" in codex_keys
    assert "roundtable.codex.model" in codex_keys
  end

  test "child span duration matches elapsed_ms" do
    start_ms = System.monotonic_time(:millisecond) - 5000
    span = Telemetry.build_span(sample_results(), sample_args(), start_ms)

    spans = span["resourceSpans"] |> hd() |> get_in(["scopeSpans", Access.at(0), "spans"])
    gemini_child = span_by_name(spans, "roundtable.gemini")
    codex_child = span_by_name(spans, "roundtable.codex")

    gemini_start = String.to_integer(gemini_child["startTimeUnixNano"])
    gemini_end = String.to_integer(gemini_child["endTimeUnixNano"])
    gemini_duration_ms = div(gemini_end - gemini_start, 1_000_000)
    assert gemini_duration_ms == 5000

    codex_start = String.to_integer(codex_child["startTimeUnixNano"])
    codex_end = String.to_integer(codex_child["endTimeUnixNano"])
    codex_duration_ms = div(codex_end - codex_start, 1_000_000)
    assert codex_duration_ms == 30_000
  end

  test "emit/3 returns :ok when endpoint not set" do
    System.delete_env("OTEL_EXPORTER_OTLP_ENDPOINT")

    assert Telemetry.emit(
             sample_results(),
             sample_args(),
             System.monotonic_time(:millisecond)
           ) == :ok
  end

  test "emit/3 returns :ok when endpoint set (fire and forget)" do
    System.put_env("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:19999")

    result =
      Telemetry.emit(sample_results(), sample_args(), System.monotonic_time(:millisecond))

    assert result == :ok
    # Give the async task a moment to run (it will fail to connect, but should not crash)
    Process.sleep(100)
  after
    System.delete_env("OTEL_EXPORTER_OTLP_ENDPOINT")
  end

  test "OTEL_RESOURCE_ATTRIBUTES parsed into resource attrs" do
    System.put_env("OTEL_RESOURCE_ATTRIBUTES", "project.name=myapp,session.tag=abc1")
    start_ms = System.monotonic_time(:millisecond) - 1000
    span = Telemetry.build_span(sample_results(), sample_args(), start_ms)
    resource_attrs = span["resourceSpans"] |> hd() |> get_in(["resource", "attributes"])
    keys = Enum.map(resource_attrs, & &1["key"])
    assert "project.name" in keys
    assert "session.tag" in keys
  after
    System.delete_env("OTEL_RESOURCE_ATTRIBUTES")
  end

  test "build_span with 3 agents generates 4 spans (1 root + 3 children)" do
    results = %{
      "gemini" => %{"status" => "ok", "elapsed_ms" => 1000, "model" => "gemini"},
      "codex" => %{"status" => "timeout", "elapsed_ms" => 2000, "model" => "codex"},
      "claude" => %{"status" => "ok", "elapsed_ms" => 3000, "model" => "claude"},
      "meta" => %{"total_elapsed_ms" => 3000}
    }

    span =
      Telemetry.build_span(
        results,
        %{role: "planner", files: []},
        System.monotonic_time(:millisecond)
      )

    spans = span["resourceSpans"] |> hd() |> get_in(["scopeSpans", Access.at(0), "spans"])

    assert length(spans) == 4
    assert Enum.any?(spans, fn s -> s["name"] == "roundtable.claude" end)
  end
end
