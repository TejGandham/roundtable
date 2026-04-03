defmodule Roundtable.CLI.PlatformTest do
  use ExUnit.Case
  alias Roundtable.CLI.Platform

  test "shell/0 returns an executable path" do
    shell = Platform.shell()
    assert is_binary(shell)
    assert File.exists?(shell) or System.find_executable(shell) != nil
  end

  test "shell_flag/0 returns -c on unix or /c on windows" do
    flag = Platform.shell_flag()

    case :os.type() do
      {:win32, _} -> assert flag == "/c"
      _ -> assert flag == "-c"
    end
  end

  test "path_separator/0 returns : on unix or ; on windows" do
    sep = Platform.path_separator()

    case :os.type() do
      {:win32, _} -> assert sep == ";"
      _ -> assert sep == ":"
    end
  end

  test "shell_escape/1 wraps in single quotes on unix" do
    case :os.type() do
      {:win32, _} ->
        assert Platform.shell_escape("hello") == "\"hello\""

      _ ->
        assert Platform.shell_escape("hello") == "'hello'"
        assert Platform.shell_escape("it's") == "'it'\\''s'"
    end
  end

  test "wrap_run_command/1 uses trap on unix, passthrough on windows" do
    wrapped = Platform.wrap_run_command("echo hi")

    case :os.type() do
      {:unix, _} ->
        refute wrapped =~ "setsid"
        assert wrapped =~ "trap"

      {:win32, _} ->
        refute wrapped =~ "setsid"
        refute wrapped =~ "trap"
    end
  end

  test "null_device/0 returns /dev/null on unix or NUL on windows" do
    dev = Platform.null_device()

    case :os.type() do
      {:win32, _} -> assert dev == "NUL"
      _ -> assert dev == "/dev/null"
    end
  end

  test "kill_tree/1 is a no-op for nil" do
    assert Platform.kill_tree(nil) == :ok
  end

  @tag :unix
  test "Port.open spawns children with their own PGID (not BEAM's)" do
    port =
      Port.open({:spawn_executable, "/bin/sh"}, [
        :binary,
        :exit_status,
        args: ["-c", "sleep 5"]
      ])

    {:os_pid, child_pid} = Port.info(port, :os_pid)
    beam_pid = :os.getpid() |> to_string() |> String.to_integer()

    beam_pgid =
      :os.cmd(String.to_charlist("ps -o pgid= -p #{beam_pid}"))
      |> to_string()
      |> String.trim()

    child_pgid =
      :os.cmd(String.to_charlist("ps -o pgid= -p #{child_pid}"))
      |> to_string()
      |> String.trim()

    Port.close(port)
    Process.sleep(100)

    assert beam_pgid != child_pgid,
           "Child PGID #{child_pgid} matches BEAM PGID #{beam_pgid} — kill 0 would be unsafe"
  end

  test "kill_tree/1 does not crash for nonexistent pid" do
    # Use a very high PID that's unlikely to exist
    assert Platform.kill_tree(999_999_999) == :ok
  end
end
