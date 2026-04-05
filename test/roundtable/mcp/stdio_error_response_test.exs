defmodule Roundtable.MCP.StdioErrorResponseTest do
  use ExUnit.Case, async: false

  @moduletag timeout: 60_000

  @root Path.expand("../../..", __DIR__)
  @bin_dir Path.expand("../support/bin", __DIR__)

  test "server_call_failed still returns JSON-RPC error to client" do
    command =
      "cd #{@root} && export ROUNDTABLE_MCP=1 && " <>
        "export ROUNDTABLE_REQUEST_TIMEOUT_MS=1 && " <>
        "export PATH=#{shell_escape(@bin_dir <> ":" <> System.get_env("PATH", ""))} && " <>
        "mix run --no-halt"

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

      # With 1ms timeout, initialize may succeed (fast enough) or return an
      # error response.  Either way the transport must NOT hang.
      case read_json(port, "", 10_000) do
        {:ok, %{"id" => 1, "result" => _}, rest} ->
          # Initialize succeeded — proceed to test tools/call timeout
          Process.sleep(100)

          send_json(port, %{
            "jsonrpc" => "2.0",
            "method" => "notifications/initialized",
            "params" => %{}
          })

          Process.sleep(100)

          send_json(port, %{
            "jsonrpc" => "2.0",
            "id" => 2,
            "method" => "tools/call",
            "params" => %{
              "name" => "hivemind",
              "arguments" => %{"prompt" => "hello", "timeout" => 5}
            }
          })

          case read_json(port, rest, 15_000) do
            {:ok, %{"id" => 2, "error" => %{"code" => code, "message" => msg}}, _rest} ->
              assert is_integer(code)
              assert is_binary(msg)

            {:ok, %{"id" => 2, "result" => _}, _rest} ->
              :ok

            {:error, :timeout} ->
              flunk("Transport hung — no JSON-RPC response within 15s (the bug)")
          end

        {:ok, %{"id" => 1, "error" => %{"code" => code, "message" => msg}}, _rest} ->
          # Initialize itself timed out at 1ms — but we got a proper error
          # response instead of the transport hanging.  This proves the fix.
          assert is_integer(code)
          assert is_binary(msg)

        {:error, :timeout} ->
          flunk("Transport hung — no JSON-RPC response within 10s (the bug)")
      end
    after
      Port.close(port)
    end
  end

  defp send_json(port, message) do
    Port.command(port, Jason.encode!(message) <> "\n")
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

          {^port, {:exit_status, _status}} ->
            {:error, :exit}
        after
          remaining -> {:error, :timeout}
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

      ["", rest] -> next_json_line(rest)
      _ -> :more
    end
  end

  defp shell_escape(value) do
    "'" <> String.replace(value, "'", "'\\''") <> "'"
  end
end
