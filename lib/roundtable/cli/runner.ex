defmodule Roundtable.CLI.Runner do
  @moduledoc "Runs external CLI processes with stdout/stderr capture, timeout, and cleanup."

  alias Roundtable.CLI.Platform

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
        |> String.split(Platform.path_separator())
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
        [{~c"PATH", String.to_charlist(extra <> Platform.path_separator() <> sys_path)} | base]

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
    cmd = "#{shell_escape(executable)} #{args_str} <#{Platform.null_device()}"

    port =
      Port.open({:spawn_executable, Platform.shell()}, [
        :binary,
        :exit_status,
        {:env, env},
        args: [Platform.shell_flag(), cmd]
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

  # Cap probe output at 64KB — probes are health checks, not data collectors
  @max_probe_output 65_536

  defp probe_receive(port, os_pid, deadline_ms, stdout_acc) do
    remaining = max(0, deadline_ms - System.monotonic_time(:millisecond))

    receive do
      {^port, {:data, chunk}} ->
        capped =
          if byte_size(stdout_acc) >= @max_probe_output,
            do: stdout_acc,
            else: binary_part(stdout_acc <> chunk, 0, min(byte_size(stdout_acc <> chunk), @max_probe_output))

        probe_receive(port, os_pid, deadline_ms, capped)

      {^port, {:exit_status, exit_code}} ->
        %{
          alive: exit_code == 0,
          exit_code: exit_code,
          stdout: String.trim(stdout_acc),
          reason: if(exit_code != 0, do: "exited with code #{exit_code}")
        }
    after
      remaining ->
        Platform.kill_tree(os_pid)
        safe_close_port(port)
        drain_port_messages(port)
        %{alive: false, exit_code: nil, stdout: "", reason: "probe timeout"}
    end
  end

  @spec run_cli(String.t(), [String.t()], non_neg_integer()) :: map()
  def run_cli(command, cli_args, timeout_ms) do
    stderr_dir = Path.join(System.tmp_dir!(), "rt_#{System.unique_integer([:positive])}")
    File.mkdir_p!(stderr_dir)
    File.chmod!(stderr_dir, 0o700)
    stderr_path = Path.join(stderr_dir, "stderr")
    start_time = System.monotonic_time(:millisecond)
    args_joined = Enum.map_join(cli_args, " ", &shell_escape/1)

    command_line =
      [shell_escape(command), args_joined]
      |> Enum.reject(&(&1 == ""))
      |> Enum.join(" ")

    child = "#{command_line} <#{Platform.null_device()} 2>#{shell_escape(stderr_path)}"
    wrapper_cmd = Platform.wrap_run_command(child)

    port =
      Port.open({:spawn_executable, Platform.shell()}, [
        :binary,
        :exit_status,
        {:env, port_env()},
        args: [Platform.shell_flag(), wrapper_cmd]
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
      File.rmdir(stderr_dir)
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
        {stderr_content, stderr_truncated} = read_stderr(stderr_path)

        %{
          stdout: stdout_acc,
          stderr: stderr_content,
          exit_code: exit_code,
          exit_signal: nil,
          elapsed_ms: System.monotonic_time(:millisecond) - start_time,
          timed_out: false,
          truncated: truncated,
          stderr_truncated: stderr_truncated
        }
    after
      max(0, remaining_ms) ->
        kill_process_group(os_pid)
        safe_close_port(port)
        drain_port_messages(port)
        {stderr_content, stderr_truncated} = read_stderr(stderr_path)

        %{
          stdout: stdout_acc,
          stderr: stderr_content,
          exit_code: nil,
          exit_signal: nil,
          elapsed_ms: System.monotonic_time(:millisecond) - start_time,
          timed_out: true,
          truncated: truncated,
          stderr_truncated: stderr_truncated
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

  defp kill_process_group(os_pid), do: Platform.kill_tree(os_pid)

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

  @max_stderr 524_288

  defp read_stderr(path) do
    case File.open(path, [:read, :binary]) do
      {:ok, file} ->
        content = IO.read(file, @max_stderr + 1)
        File.close(file)

        cond do
          not is_binary(content) -> {"", false}
          byte_size(content) > @max_stderr -> {binary_part(content, 0, @max_stderr), true}
          true -> {content, false}
        end

      {:error, _} ->
        {"", false}
    end
  end

  defp shell_escape(arg), do: Platform.shell_escape(arg)
end
