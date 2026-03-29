Code.require_file(Path.expand("../support/mock_cli.ex", __DIR__))

defmodule Roundtable.DispatcherTest do
  use ExUnit.Case, async: false

  alias Roundtable.Dispatcher

  @support_dir Path.expand("../support", __DIR__)

  defp success_script, do: Path.join(@support_dir, "fake_cli_success.sh")
  defp timeout_script, do: Path.join(@support_dir, "fake_cli_timeout.sh")
  defp error_script, do: Path.join(@support_dir, "fake_cli_error.sh")

  setup do
    [success_script(), timeout_script(), error_script()]
    |> Enum.each(fn script ->
      if File.exists?(script), do: File.chmod!(script, 0o755)
    end)

    :ok
  end

  defp make_config(name, module, path, extra \\ %{}) do
    Map.merge(
      %{
        name: name,
        module: module,
        path: path,
        model: nil,
        role: "default",
        files: ["lib/roundtable/dispatcher.ex"],
        args: %{},
        prompt: "test prompt"
      },
      extra
    )
  end

  defp write_runtime_script(body) do
    path =
      Path.join(System.tmp_dir!(), "dispatcher_test_#{System.unique_integer([:positive])}.sh")

    File.write!(path, body)
    File.chmod!(path, 0o755)
    path
  end

  test "dispatches to multiple CLIs and returns results with meta" do
    configs = [
      make_config("gemini", Roundtable.MockCLI, success_script()),
      make_config("codex", Roundtable.MockCLI, success_script())
    ]

    result = Dispatcher.dispatch(%{cli_configs: configs, timeout_ms: 10_000})

    assert Map.has_key?(result, "gemini")
    assert Map.has_key?(result, "codex")
    assert Map.has_key?(result, "meta")
    assert result["gemini"]["status"] == "ok"
    assert result["codex"]["status"] == "ok"
  end

  test "returns not_found when executable path is nil" do
    configs = [
      make_config("gemini", Roundtable.MockCLI, nil),
      make_config("codex", Roundtable.MockCLI, success_script())
    ]

    result = Dispatcher.dispatch(%{cli_configs: configs, timeout_ms: 10_000})

    assert result["gemini"]["status"] == "not_found"
    assert result["codex"]["status"] == "ok"
  end

  test "returns probe_failed when probe times out" do
    configs = [
      make_config("gemini", Roundtable.MockCLI, timeout_script()),
      make_config("codex", Roundtable.MockCLI, success_script())
    ]

    result = Dispatcher.dispatch(%{cli_configs: configs, timeout_ms: 2_000})

    assert result["gemini"]["status"] == "probe_failed"
    assert result["codex"]["status"] == "ok"
  end

  test "returns timeout when run exceeds timeout but probe succeeds" do
    long_run =
      write_runtime_script("""
      #!/bin/sh
      if [ \"$1\" = \"--version\" ]; then
        echo "v1"
        exit 0
      fi

      sleep 2
      echo "LATE"
      exit 0
      """)

    configs = [
      make_config("gemini", Roundtable.MockCLI, long_run),
      make_config("codex", Roundtable.MockCLI, success_script())
    ]

    result = Dispatcher.dispatch(%{cli_configs: configs, timeout_ms: 500})

    assert result["gemini"]["status"] == "timeout"
    assert result["codex"]["status"] == "ok"
  end

  test "meta includes role assignments and referenced files" do
    configs = [
      make_config("gemini", Roundtable.MockCLI, success_script(), %{
        role: "planner",
        files: ["a.ex"]
      }),
      make_config("codex", Roundtable.MockCLI, success_script(), %{role: "codereviewer"})
    ]

    result = Dispatcher.dispatch(%{cli_configs: configs, timeout_ms: 10_000})

    assert result["meta"]["gemini_role"] == "planner"
    assert result["meta"]["codex_role"] == "codereviewer"
    assert result["meta"]["files_referenced"] == ["a.ex"]
    assert is_integer(result["meta"]["total_elapsed_ms"])
  end

  test "supports parser error status from CLI module" do
    run_error =
      write_runtime_script("""
      #!/bin/sh
      if [ \"$1\" = \"--version\" ]; then
        echo "v1"
        exit 0
      fi

      echo "runtime error" >&2
      exit 1
      """)

    configs = [
      make_config("gemini", Roundtable.MockErrorCLI, run_error),
      make_config("codex", Roundtable.MockCLI, success_script())
    ]

    result = Dispatcher.dispatch(%{cli_configs: configs, timeout_ms: 10_000})

    assert result["gemini"]["status"] == "error"
    assert result["gemini"]["response"] == "error response"
    assert result["codex"]["status"] == "ok"
  end

  test "executes CLIs in parallel" do
    one_second =
      write_runtime_script("""
      #!/bin/sh
      if [ \"$1\" = \"--version\" ]; then
        echo "v1"
        exit 0
      fi

      sleep 1
      echo "DONE"
      exit 0
      """)

    configs = [
      make_config("gemini", Roundtable.MockCLI, one_second),
      make_config("codex", Roundtable.MockCLI, one_second)
    ]

    start_ms = System.monotonic_time(:millisecond)
    result = Dispatcher.dispatch(%{cli_configs: configs, timeout_ms: 5_000})
    elapsed_ms = System.monotonic_time(:millisecond) - start_ms

    assert result["gemini"]["status"] == "ok"
    assert result["codex"]["status"] == "ok"
    assert elapsed_ms < 1_900
  end
end
