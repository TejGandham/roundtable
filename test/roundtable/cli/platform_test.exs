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

  test "wrap_run_command/1 does not include setsid on non-linux" do
    wrapped = Platform.wrap_run_command("echo hi")

    case :os.type() do
      {:unix, :linux} ->
        assert wrapped =~ "setsid"
        assert wrapped =~ "trap"

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

  test "kill_tree/1 does not crash for nonexistent pid" do
    # Use a very high PID that's unlikely to exist
    assert Platform.kill_tree(999_999_999) == :ok
  end
end
