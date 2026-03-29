defmodule Roundtable.CLI.Runner do
  @moduledoc "Runs external CLI processes with stdout/stderr capture, timeout, and cleanup."

  @max_output 1_048_576

  @spec find_executable(String.t()) :: String.t() | nil
  def find_executable(name), do: System.find_executable(name)

  @spec probe_cli(String.t(), [String.t()], non_neg_integer()) :: map()
  def probe_cli(executable, test_args, probe_timeout_ms \\ 5_000) do
    env = [{String.to_charlist("ROUNDTABLE_ACTIVE"), String.to_charlist("1")}]

    port =
      Port.open({:spawn_executable, executable}, [
        :binary,
        :exit_status,
        {:env, env},
        args: test_args
      ])

    os_pid =
      case Port.info(port, :os_pid) do
        {:os_pid, pid} -> pid
        nil -> nil
      end

    probe_receive(port, os_pid, probe_timeout_ms, "")
  rescue
    _ -> %{alive: false, exit_code: nil, stdout: "", reason: "probe failed to start"}
  end

  defp probe_receive(port, os_pid, timeout_ms, stdout_acc) do
    receive do
      {^port, {:data, chunk}} ->
        probe_receive(port, os_pid, timeout_ms, stdout_acc <> chunk)

      {^port, {:exit_status, exit_code}} ->
        %{
          alive: exit_code == 0,
          exit_code: exit_code,
          stdout: String.trim(stdout_acc),
          reason: if(exit_code != 0, do: "exited with code #{exit_code}")
        }
    after
      timeout_ms ->
        if os_pid do
          :os.cmd(String.to_charlist("kill -KILL #{os_pid} 2>/dev/null; true"))
        end

        safe_close_port(port)
        drain_port_messages(port)
        %{alive: false, exit_code: nil, stdout: "", reason: "probe timeout"}
    end
  end

  @spec run_cli(String.t(), [String.t()], non_neg_integer()) :: map()
  def run_cli(command, cli_args, timeout_ms) do
    stderr_path = Path.join(System.tmp_dir!(), "rt_stderr_#{System.unique_integer([:positive])}")
    start_time = System.monotonic_time(:millisecond)
    args_joined = Enum.map_join(cli_args, " ", &shell_escape/1)

    command_line =
      [shell_escape(command), args_joined]
      |> Enum.reject(&(&1 == ""))
      |> Enum.join(" ")

    inner = "exec #{command_line} 2>#{shell_escape(stderr_path)}"
    wrapper_cmd = "exec setsid --wait /bin/sh -c #{shell_escape(inner)}"

    port =
      Port.open({:spawn_executable, "/bin/sh"}, [
        :binary,
        :exit_status,
        {:env, [{String.to_charlist("ROUNDTABLE_ACTIVE"), String.to_charlist("1")}]},
        args: ["-c", wrapper_cmd]
      ])

    try do
      collect_output(port, timeout_ms, start_time, stderr_path, "", false, nil)
    after
      File.rm(stderr_path)
    end
  end

  defp collect_output(port, timeout_ms, start_time, stderr_path, stdout_acc, truncated, os_pid) do
    os_pid = os_pid || get_os_pid(port)
    elapsed_ms = System.monotonic_time(:millisecond) - start_time
    remaining_ms = timeout_ms - elapsed_ms

    receive do
      {^port, {:data, chunk}} ->
        {new_stdout, new_truncated} = append_capped(stdout_acc, chunk, truncated)

        collect_output(
          port,
          timeout_ms,
          start_time,
          stderr_path,
          new_stdout,
          new_truncated,
          os_pid
        )

      {^port, {:exit_status, exit_code}} ->
        %{
          stdout: stdout_acc,
          stderr: read_stderr(stderr_path),
          exit_code: exit_code,
          exit_signal: nil,
          elapsed_ms: System.monotonic_time(:millisecond) - start_time,
          timed_out: false,
          truncated: truncated
        }
    after
      max(0, remaining_ms) ->
        kill_process_group(os_pid)
        safe_close_port(port)
        drain_port_messages(port)

        %{
          stdout: stdout_acc,
          stderr: read_stderr(stderr_path),
          exit_code: nil,
          exit_signal: nil,
          elapsed_ms: System.monotonic_time(:millisecond) - start_time,
          timed_out: true,
          truncated: truncated
        }
    end
  end

  defp append_capped(stdout_acc, chunk, truncated) do
    current_size = byte_size(stdout_acc)

    cond do
      current_size >= @max_output ->
        {stdout_acc, true}

      true ->
        remaining = @max_output - current_size
        chunk_size = byte_size(chunk)

        if chunk_size <= remaining do
          {stdout_acc <> chunk, truncated}
        else
          {stdout_acc <> binary_part(chunk, 0, remaining), true}
        end
    end
  end

  defp get_os_pid(port) do
    case Port.info(port, :os_pid) do
      {:os_pid, pid} -> pid
      nil -> nil
    end
  rescue
    ArgumentError -> nil
  end

  defp kill_process_group(os_pid) do
    if os_pid do
      child_pids =
        :os.cmd(String.to_charlist("pgrep -P #{os_pid} 2>/dev/null"))
        |> to_string()
        |> String.split()
        |> Enum.reject(&(&1 == ""))

      :os.cmd(String.to_charlist("kill -TERM #{os_pid} 2>/dev/null; true"))

      Enum.each(child_pids, fn child_pid ->
        :os.cmd(String.to_charlist("kill -TERM -#{child_pid} 2>/dev/null; true"))
      end)

      Process.sleep(3_000)

      :os.cmd(String.to_charlist("kill -KILL #{os_pid} 2>/dev/null; true"))

      Enum.each(child_pids, fn child_pid ->
        :os.cmd(String.to_charlist("kill -KILL -#{child_pid} 2>/dev/null; true"))
      end)
    end

    :ok
  end

  defp safe_close_port(port) do
    Port.close(port)
  rescue
    ArgumentError -> :ok
  end

  defp drain_port_messages(port) do
    receive do
      {^port, _} -> drain_port_messages(port)
    after
      0 -> :ok
    end
  end

  defp read_stderr(path) do
    case File.read(path) do
      {:ok, content} -> content
      {:error, _} -> ""
    end
  end

  defp shell_escape(arg) do
    "'" <> String.replace(arg, "'", "'\\''") <> "'"
  end
end
