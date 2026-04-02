defmodule Roundtable.CLI.Runner do
  @moduledoc "Runs external CLI processes with stdout/stderr capture, timeout, and cleanup."

  @max_output 1_048_576

  @spec find_executable(String.t()) :: String.t() | nil
  def find_executable(name), do: System.find_executable(name)

  @doc """
  Resolves the absolute path for a CLI executable.

  Resolution order:
  1. `ROUNDTABLE_<NAME>_PATH` env var (e.g. `ROUNDTABLE_CLAUDE_PATH=/usr/local/bin/claude`)
  2. Directories in `ROUNDTABLE_EXTRA_PATH` (colon-separated, searched before system PATH)
  3. `System.find_executable/1` (uses system PATH)
  """
  @spec resolve_executable(String.t()) :: String.t() | nil
  def resolve_executable(name) do
    env_key = "ROUNDTABLE_#{String.upcase(name)}_PATH"

    case System.get_env(env_key) do
      path when is_binary(path) and path != "" ->
        if File.exists?(path), do: path, else: nil

      _ ->
        find_in_extra_path(name) || System.find_executable(name)
    end
  end

  defp find_in_extra_path(name) do
    case System.get_env("ROUNDTABLE_EXTRA_PATH") do
      extra when is_binary(extra) and extra != "" ->
        extra
        |> String.split(":")
        |> Enum.reject(&(&1 == ""))
        |> Enum.find_value(fn dir ->
          candidate = Path.join(dir, name)
          if File.exists?(candidate) and not File.dir?(candidate), do: candidate
        end)

      _ ->
        nil
    end
  end

  @doc false
  def port_env do
    base = [{~c"ROUNDTABLE_ACTIVE", ~c"1"}]

    case System.get_env("ROUNDTABLE_EXTRA_PATH") do
      extra when is_binary(extra) and extra != "" ->
        sys_path = System.get_env("PATH") || ""
        [{~c"PATH", String.to_charlist(extra <> ":" <> sys_path)} | base]

      _ ->
        base
    end
  end

  @spec probe_cli(String.t(), [String.t()], non_neg_integer()) :: map()
  def probe_cli(executable, test_args, probe_timeout_ms \\ 5_000) do
    env = port_env()

    # Wrap in shell to redirect stdin from /dev/null — prevents probes
    # from consuming MCP stdio bytes when running under the MCP transport.
    args_str = Enum.map_join(test_args, " ", &shell_escape/1)
    cmd = "#{shell_escape(executable)} #{args_str} </dev/null"

    port =
      Port.open({:spawn_executable, "/bin/sh"}, [
        :binary,
        :exit_status,
        {:env, env},
        args: ["-c", cmd]
      ])

    os_pid =
      case Port.info(port, :os_pid) do
        {:os_pid, pid} -> pid
        nil -> nil
      end

    deadline_ms = System.monotonic_time(:millisecond) + probe_timeout_ms
    probe_receive(port, os_pid, deadline_ms, "")
  rescue
    _ -> %{alive: false, exit_code: nil, stdout: "", reason: "probe failed to start"}
  end

  defp probe_receive(port, os_pid, deadline_ms, stdout_acc) do
    remaining = max(0, deadline_ms - System.monotonic_time(:millisecond))

    receive do
      {^port, {:data, chunk}} ->
        probe_receive(port, os_pid, deadline_ms, stdout_acc <> chunk)

      {^port, {:exit_status, exit_code}} ->
        %{
          alive: exit_code == 0,
          exit_code: exit_code,
          stdout: String.trim(stdout_acc),
          reason: if(exit_code != 0, do: "exited with code #{exit_code}")
        }
    after
      remaining ->
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

    # Run CLI as a child (not exec'd) so the shell trap stays active.
    # trap "kill 0" kills ALL processes in the setsid group when the
    # Erlang port closes — prevents orphaned CLIs on parent crash.
    child = "#{command_line} </dev/null 2>#{shell_escape(stderr_path)}"

    wrapper_cmd =
      "exec setsid --wait /bin/sh -c " <>
        shell_escape("trap 'kill 0' EXIT; #{child}; s=$?; trap - EXIT; exit $s") <>
        " </dev/null"

    port =
      Port.open({:spawn_executable, "/bin/sh"}, [
        :binary,
        :exit_status,
        {:env, port_env()},
        args: ["-c", wrapper_cmd]
      ])

    os_pid = get_os_pid(port)
    caller = self()

    cleanup_monitor =
      spawn(fn ->
        ref = Process.monitor(caller)

        receive do
          {:DOWN, ^ref, :process, _, _} -> kill_process_group(os_pid)
        end
      end)

    try do
      collect_output(port, timeout_ms, start_time, stderr_path, "", false, nil)
    after
      Process.exit(cleanup_monitor, :kill)
      kill_process_group(get_os_pid(port))
      safe_close_port(port)
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
    case File.open(path, [:read, :binary]) do
      {:ok, file} ->
        content = IO.read(file, 524_288)
        File.close(file)

        if is_binary(content), do: content, else: ""

      {:error, _} ->
        ""
    end
  end

  defp shell_escape(arg) do
    "'" <> String.replace(arg, "'", "'\\''") <> "'"
  end
end
