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

  test "build_span produces valid OTEL structure" do
    start_ms = System.monotonic_time(:millisecond) - 5000
    span = Telemetry.build_span(sample_results(), sample_args(), start_ms)
    assert Map.has_key?(span, "resourceSpans")
    [rs] = span["resourceSpans"]
    [ss] = rs["scopeSpans"]
    [s] = ss["spans"]
    assert s["name"] == "roundtable.invoke"
    assert s["traceId"] |> String.length() == 32
    assert s["spanId"] |> String.length() == 16
    # gemini ok -> status 1
    assert s["status"]["code"] == 1
  end

  test "span attributes include all required fields" do
    start_ms = System.monotonic_time(:millisecond) - 1000
    span = Telemetry.build_span(sample_results(), sample_args(), start_ms)

    attrs =
      span["resourceSpans"]
      |> hd()
      |> get_in(["scopeSpans", Access.at(0), "spans", Access.at(0), "attributes"])

    keys = Enum.map(attrs, & &1["key"])
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

    s =
      span["resourceSpans"]
      |> hd()
      |> get_in(["scopeSpans", Access.at(0), "spans", Access.at(0)])

    assert s["status"]["code"] == 2
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
end
