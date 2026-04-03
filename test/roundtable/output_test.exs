defmodule Roundtable.OutputTest do
  use ExUnit.Case, async: true
  alias Roundtable.Output

  defmodule MockCLI do
    def parse_output(_stdout, _stderr) do
      %{response: "mock response", status: :ok, parse_error: nil, metadata: %{}, session_id: nil}
    end
  end

  defmodule MockErrorCLI do
    def parse_output(_stdout, _stderr) do
      %{
        response: "error response",
        status: :error,
        parse_error: nil,
        metadata: %{},
        session_id: nil
      }
    end
  end

  defmodule MockRateLimitedCLI do
    def parse_output(_stdout, _stderr) do
      %{
        response: "Gemini rate limited (429/RESOURCE_EXHAUSTED). Retry later",
        status: :rate_limited,
        parse_error: nil,
        metadata: %{},
        session_id: nil
      }
    end
  end

  defmodule MockModelCLI do
    def parse_output(_stdout, _stderr) do
      %{
        response: "model response",
        status: :ok,
        parse_error: nil,
        metadata: %{model_used: "gemini-2.0-flash"},
        session_id: "ses_123"
      }
    end
  end

  defp make_raw(overrides \\ %{}) do
    Map.merge(
      %{
        stdout: "output",
        stderr: "",
        exit_code: 0,
        exit_signal: nil,
        elapsed_ms: 100,
        timed_out: false,
        truncated: false,
        stderr_truncated: false
      },
      overrides
    )
  end

  describe "build_result/6 - not found" do
    test "returns not_found when path is nil" do
      result = Output.build_result("Gemini", nil, nil, nil, nil, MockCLI)
      assert result["status"] == "not_found"
      assert result["stderr"] =~ "Gemini CLI not found in PATH"
      assert result["elapsed_ms"] == 0
      assert result["model"] == "cli-default"
    end
  end

  describe "build_result/6 - probe failed" do
    test "returns probe_failed when probe alive: false" do
      probe = %{alive: false, reason: "timeout", exit_code: nil}
      result = Output.build_result("Codex", "/usr/bin/codex", "gpt-4", probe, nil, MockCLI)
      assert result["status"] == "probe_failed"
      assert result["stderr"] =~ "Codex CLI probe failed: timeout"
      assert result["stderr"] =~ "codex --version"
      assert result["model"] == "gpt-4"
      assert result["exit_code"] == nil
    end
  end

  describe "build_result/6 - normal execution" do
    test "ok status from successful execution" do
      result =
        Output.build_result("Gemini", "/bin/gemini", nil, %{alive: true}, make_raw(), MockCLI)

      assert result["status"] == "ok"
      assert result["response"] == "mock response"
      assert result["exit_code"] == 0
      assert result["elapsed_ms"] == 100
    end

    test "timeout overrides parser ok" do
      result =
        Output.build_result(
          "Gemini",
          "/bin/gemini",
          nil,
          %{alive: true},
          make_raw(%{timed_out: true, exit_code: nil}),
          MockCLI
        )

      assert result["status"] == "timeout"
      assert result["response"] =~ "Request timed out after"
      assert result["response"] =~ "Retry with a longer timeout or resume the session"
      assert result["parse_error"] == nil
    end

    test "terminated when exit_signal present" do
      result =
        Output.build_result(
          "Gemini",
          "/bin/gemini",
          nil,
          %{alive: true},
          make_raw(%{exit_signal: "SIGTERM", exit_code: nil}),
          MockCLI
        )

      assert result["status"] == "terminated"
    end

    test "non-zero exit downgrades parser ok to error" do
      result =
        Output.build_result(
          "Gemini",
          "/bin/gemini",
          nil,
          %{alive: true},
          make_raw(%{exit_code: 1}),
          MockCLI
        )

      assert result["status"] == "error"
    end

    test "non-zero exit keeps error status from parser" do
      result =
        Output.build_result(
          "Gemini",
          "/bin/gemini",
          nil,
          %{alive: true},
          make_raw(%{exit_code: 1}),
          MockErrorCLI
        )

      assert result["status"] == "error"
    end

    test "custom parser statuses like rate_limited are preserved" do
      result =
        Output.build_result(
          "Gemini",
          "/bin/gemini",
          nil,
          %{alive: true},
          make_raw(%{exit_code: 1}),
          MockRateLimitedCLI
        )

      assert result["status"] == "rate_limited"
      assert result["response"] =~ "Gemini rate limited"
    end

    test "uses model_used from parsed metadata when available" do
      result =
        Output.build_result(
          "Gemini",
          "/bin/gemini",
          "fallback-model",
          %{alive: true},
          make_raw(),
          MockModelCLI
        )

      assert result["model"] == "gemini-2.0-flash"
      assert result["session_id"] == "ses_123"
    end
  end

  describe "build_meta/2" do
    test "total_elapsed_ms is max of both" do
      results = %{"gemini" => %{"elapsed_ms" => 500}, "codex" => %{"elapsed_ms" => 1200}}

      cli_configs = [
        %{name: "gemini", role: "planner", files: ["src/auth.ts"]},
        %{name: "codex", role: "codereviewer", files: ["src/auth.ts"]}
      ]

      meta = Output.build_meta(results, cli_configs)
      assert meta["total_elapsed_ms"] == 1200
      assert meta["gemini_role"] == "planner"
      assert meta["codex_role"] == "codereviewer"
      assert meta["files_referenced"] == ["src/auth.ts"]
    end

    test "with 3 agents includes all agent roles" do
      results = %{
        "gemini" => %{"elapsed_ms" => 500},
        "codex" => %{"elapsed_ms" => 1200},
        "claude" => %{"elapsed_ms" => 800}
      }

      cli_configs = [
        %{name: "gemini", role: "planner", files: ["src/auth.ts"]},
        %{name: "codex", role: "default", files: ["src/auth.ts"]},
        %{name: "claude", role: "reviewer", files: ["src/auth.ts"]}
      ]

      meta = Output.build_meta(results, cli_configs)

      assert meta["gemini_role"] == "planner"
      assert meta["codex_role"] == "default"
      assert meta["claude_role"] == "reviewer"
      assert meta["total_elapsed_ms"] == 1200
    end
  end

  describe "encode/1" do
    test "produces pretty JSON with string keys" do
      data = %{"gemini" => %{"status" => "ok"}, "codex" => %{"status" => "ok"}}
      encoded = Output.encode(data)
      decoded = Jason.decode!(encoded)
      assert is_map(decoded)
      assert decoded["gemini"]["status"] == "ok"
      assert encoded =~ "\n"
    end
  end

  describe "all output maps use string keys" do
    test "not_found result has only string keys" do
      result = Output.build_result("Gemini", nil, nil, nil, nil, MockCLI)
      assert Enum.all?(Map.keys(result), &is_binary/1)
    end
  end
end
