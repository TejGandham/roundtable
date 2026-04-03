defmodule Roundtable.CLI.Platform do
  @moduledoc "Platform-specific helpers for shell execution and process cleanup."

  @doc "Returns the shell executable path for the current OS."
  @spec shell() :: String.t()
  def shell do
    case :os.type() do
      {:win32, _} -> System.find_executable("cmd.exe") || "cmd.exe"
      _ -> "/bin/sh"
    end
  end

  @doc "Returns the shell flag to execute a command string."
  @spec shell_flag() :: String.t()
  def shell_flag do
    case :os.type() do
      {:win32, _} -> "/c"
      _ -> "-c"
    end
  end

  @doc "Returns the null device path for the current OS."
  @spec null_device() :: String.t()
  def null_device do
    case :os.type() do
      {:win32, _} -> "NUL"
      _ -> "/dev/null"
    end
  end

  @doc "Returns the PATH separator for the current OS."
  @spec path_separator() :: String.t()
  def path_separator do
    case :os.type() do
      {:win32, _} -> ";"
      _ -> ":"
    end
  end

  @doc """
  Wraps a child command string for `run_cli` with platform-appropriate
  process group management and orphan cleanup.

  `child` is the raw command with stdin/stderr redirects already applied.
  """
  @spec wrap_run_command(String.t()) :: String.t()
  def wrap_run_command(child) do
    case :os.type() do
      {:unix, :linux} ->
        # setsid creates a new process group; trap kills the group on exit
        "exec setsid --wait /bin/sh -c " <>
          shell_escape("trap 'kill 0' EXIT; #{child}; s=$?; trap - EXIT; exit $s") <>
          " <" <> null_device()

      {:unix, _} ->
        # macOS/BSD: no setsid, but trap still kills the shell's process group
        "trap 'kill 0' EXIT; #{child}; s=$?; trap - EXIT; exit $s"

      {:win32, _} ->
        # Windows: no traps or process groups; rely on kill_tree for cleanup
        child
    end
  end

  @doc "Kills a process and all its descendants recursively. No-op if pid is nil."
  @spec kill_tree(non_neg_integer() | nil) :: :ok
  def kill_tree(nil), do: :ok

  def kill_tree(os_pid) do
    case :os.type() do
      {:win32, _} ->
        # /T kills the process tree
        :os.cmd(String.to_charlist("taskkill /PID #{os_pid} /F /T 2>nul & exit /b 0"))

      _ ->
        # Collect all descendants before killing (depth-first)
        all_pids = collect_descendants(os_pid)

        # Kill leaf processes first, then parent
        Enum.reverse(all_pids)
        |> Enum.each(fn pid ->
          :os.cmd(String.to_charlist("kill -KILL #{pid} 2>/dev/null; true"))
        end)

        :os.cmd(String.to_charlist("kill -KILL #{os_pid} 2>/dev/null; true"))
    end

    :ok
  end

  defp collect_descendants(pid) do
    children =
      :os.cmd(String.to_charlist("pgrep -P #{pid} 2>/dev/null"))
      |> to_string()
      |> String.split()
      |> Enum.reject(&(&1 == ""))

    children ++ Enum.flat_map(children, &collect_descendants/1)
  end

  @doc "Escapes a string for safe inclusion in a shell command."
  @spec shell_escape(String.t()) :: String.t()
  def shell_escape(arg) do
    case :os.type() do
      {:win32, _} ->
        # cmd.exe: escape metacharacters with ^, use "" for literal quotes
        escaped =
          arg
          |> String.replace("^", "^^")
          |> String.replace("\"", "\"\"")
          |> String.replace("%", "^%")
          |> String.replace("!", "^!")
          |> String.replace("&", "^&")
          |> String.replace("|", "^|")
          |> String.replace("<", "^<")
          |> String.replace(">", "^>")
          |> String.replace("(", "^(")
          |> String.replace(")", "^)")

        "\"" <> escaped <> "\""

      _ ->
        "'" <> String.replace(arg, "'", "'\\''") <> "'"
    end
  end
end
