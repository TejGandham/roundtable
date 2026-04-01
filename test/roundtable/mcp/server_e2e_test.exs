defmodule Roundtable.MCP.ServerE2ETest do
  use ExUnit.Case, async: false

  @moduletag timeout: 120_000

  @root Path.expand("../../..", __DIR__)
  @bin_dir Path.expand("../../support/bin", __DIR__)

  setup do
    for script <- ["gemini", "codex", "claude", "gemini_timeout", "gemini_rate_limited"] do
      path = Path.join(@bin_dir, script)
      if File.exists?(path), do: File.chmod!(path, 0o755)
    end

    :ok
  end

  test "happy path returns ok statuses over real MCP stdio" do
    {:ok, payload} =
      run_mcp_call(%{
        "name" => "hivemind",
        "arguments" => %{"prompt" => "Say hello", "timeout" => 30}
      })

    assert status_map(payload) == %{"gemini" => "ok", "codex" => "ok", "claude" => "ok"}
    assert payload["gemini"]["response"] == "test response"
  end

  test "gemini 429 returns rate_limited over real MCP stdio" do
    with_script_replacement("gemini", "gemini_rate_limited", fn temp_dir ->
      {:ok, payload} =
        run_mcp_call(
          %{"name" => "hivemind", "arguments" => %{"prompt" => "Say hello", "timeout" => 30}},
          temp_dir
        )

      assert payload["gemini"]["status"] == "rate_limited"
      assert payload["gemini"]["response"] =~ "Gemini rate limited"
      assert payload["codex"]["status"] == "ok"
      assert payload["claude"]["status"] == "ok"
    end)
  end

  test "gemini timeout returns timeout over real MCP stdio" do
    with_script_replacement("gemini", "gemini_timeout", fn temp_dir ->
      {:ok, payload} =
        run_mcp_call(
          %{"name" => "hivemind", "arguments" => %{"prompt" => "Say hello", "timeout" => 2}},
          temp_dir
        )

      assert payload["gemini"]["status"] == "timeout"
      assert payload["gemini"]["response"] =~ "Request timed out after"
      assert payload["gemini"]["response"] =~ "Retry with a longer timeout or resume the session"
      assert payload["gemini"]["parse_error"] == nil
      assert payload["codex"]["status"] == "ok"
      assert payload["claude"]["status"] == "ok"
    end)
  end

  test "invalid timeout gets fast actionable tool error over real MCP stdio" do
    {:error, message} =
      run_mcp_call(%{
        "name" => "hivemind",
        "arguments" => %{"prompt" => "Say hello", "timeout" => 0}
      })

    assert message =~ "timeout must be an integer between 1 and 900 seconds"
  end

  defp run_mcp_call(call_params, extra_bin_dir \\ nil) do
    path =
      Enum.join(
        Enum.reject([extra_bin_dir, @bin_dir, System.get_env("PATH", "")], &is_nil/1),
        ":"
      )

    command =
      "cd #{@root} && export ROUNDTABLE_MCP=1 && export PATH=#{shell_escape(path)} && mix run --no-halt"

    port =
      Port.open({:spawn_executable, "/bin/sh"}, [
        :binary,
        :exit_status,
        :use_stdio,
        :hide,
        :stderr_to_stdout,
        args: ["-lc", command]
      ])

    try do
      send_json(port, %{
        "jsonrpc" => "2.0",
        "id" => 1,
        "method" => "initialize",
        "params" => %{
          "protocolVersion" => "2025-03-26",
          "capabilities" => %{},
          "clientInfo" => %{"name" => "exunit", "version" => "1.0"}
        }
      })

      assert {:ok, %{"id" => 1}, _} = read_json(port, "", 60_000)
      Process.sleep(200)

      send_json(port, %{
        "jsonrpc" => "2.0",
        "method" => "notifications/initialized",
        "params" => %{}
      })

      Process.sleep(200)

      send_json(port, %{
        "jsonrpc" => "2.0",
        "id" => 2,
        "method" => "tools/call",
        "params" => call_params
      })

      case read_until_response(port, "", 35_000) do
        {:ok, %{"result" => %{"content" => [%{"text" => text}], "isError" => false}}} ->
          {:ok, Jason.decode!(text)}

        {:ok, %{"result" => %{"content" => [%{"text" => text}], "isError" => true}}} ->
          {:error, text}

        {:ok, %{"error" => %{"message" => message}}} ->
          {:error, message}

        {:error, reason} ->
          flunk("failed to read MCP response: #{inspect(reason)}")
      end
    after
      Port.close(port)
    end
  end

  defp read_until_response(port, buffer, timeout_ms) do
    deadline = System.monotonic_time(:millisecond) + timeout_ms
    do_read_until_response(port, buffer, deadline)
  end

  defp do_read_until_response(port, buffer, deadline) do
    case read_json(port, buffer, max(0, deadline - System.monotonic_time(:millisecond))) do
      {:ok, %{"id" => 2} = msg, _rest} ->
        {:ok, msg}

      {:ok, _msg, rest} ->
        do_read_until_response(port, rest, deadline)

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp read_json(port, buffer, timeout_ms) do
    deadline = System.monotonic_time(:millisecond) + timeout_ms
    do_read_json(port, buffer, deadline)
  end

  defp do_read_json(port, buffer, deadline) do
    case next_json_line(buffer) do
      {:ok, msg, rest} ->
        {:ok, msg, rest}

      :more ->
        remaining = max(0, deadline - System.monotonic_time(:millisecond))

        receive do
          {^port, {:data, chunk}} ->
            do_read_json(port, buffer <> chunk, deadline)

          {^port, {:exit_status, status}} ->
            {:error, {:exit_status, status}}
        after
          remaining ->
            {:error, :timeout}
        end
    end
  end

  defp next_json_line(buffer) do
    case String.split(buffer, "\n", parts: 2) do
      [line, rest] when line != "" ->
        case Jason.decode(line) do
          {:ok, message} -> {:ok, message, rest}
          {:error, _} -> next_json_line(rest)
        end

      [""] ->
        :more

      ["", rest] ->
        next_json_line(rest)

      [_partial] ->
        :more
    end
  end

  defp send_json(port, message) do
    Port.command(port, Jason.encode!(message) <> "\n")
  end

  defp with_script_replacement(target_name, replacement_name, fun) do
    temp_dir =
      Path.join(System.tmp_dir!(), "roundtable_mcp_bin_#{System.unique_integer([:positive])}")

    replacement_path = Path.join(@bin_dir, replacement_name)
    target_path = Path.join(temp_dir, target_name)

    File.mkdir_p!(temp_dir)
    File.cp!(replacement_path, target_path)
    File.chmod!(target_path, 0o755)

    try do
      fun.(temp_dir)
    after
      File.rm_rf!(temp_dir)
    end
  end

  defp status_map(payload) do
    for key <- ["gemini", "codex", "claude"], into: %{} do
      {key, payload[key]["status"]}
    end
  end

  defp shell_escape(value) do
    "'" <> String.replace(value, "'", "'\\''") <> "'"
  end
end
