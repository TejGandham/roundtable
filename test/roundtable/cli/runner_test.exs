defmodule Roundtable.CLI.RunnerTest do
  use ExUnit.Case
  alias Roundtable.CLI.Runner
  alias Roundtable.CLI.Platform

  @support_dir Path.expand("../../support", __DIR__)

  setup do
    Enum.each(
      ["fake_cli_success.sh", "fake_cli_timeout.sh", "fake_cli_error.sh", "fake_cli_large.sh"],
      fn script ->
        path = Path.join(@support_dir, script)
        if File.exists?(path), do: File.chmod!(path, 0o755)
      end
    )

    :ok
  end

  test "captures stdout and stderr separately" do
    script = Path.join(@support_dir, "fake_cli_success.sh")
    result = Runner.run_cli(script, [], 5_000)

    assert result.exit_code == 0
    assert result.stdout =~ "STDOUT_OUTPUT"
    assert result.stderr =~ "STDERR_OUTPUT"
    assert result.timed_out == false
    assert result.elapsed_ms > 0
    assert result.truncated == false
  end

  test "timed_out is true when CLI exceeds timeout" do
    script = Path.join(@support_dir, "fake_cli_timeout.sh")
    result = Runner.run_cli(script, [], 2_000)

    assert result.timed_out == true
    assert result.elapsed_ms >= 2_000
    assert result.elapsed_ms < 8_000
  end

  @tag :unix
  test "no orphan processes after timeout" do
    script = Path.join(@support_dir, "fake_cli_timeout.sh")

    before =
      :os.cmd(String.to_charlist("pgrep -f fake_cli_timeout.sh 2>/dev/null || true"))
      |> to_string()
      |> String.split("\n", trim: true)
      |> MapSet.new()

    Runner.run_cli(script, [], 1_000)
    Process.sleep(500)

    after_pids =
      :os.cmd(String.to_charlist("pgrep -f fake_cli_timeout.sh 2>/dev/null || true"))
      |> to_string()
      |> String.split("\n", trim: true)
      |> MapSet.new()

    assert MapSet.subset?(after_pids, before)
  end

  test "captures non-zero exit code" do
    script = Path.join(@support_dir, "fake_cli_error.sh")
    result = Runner.run_cli(script, [], 5_000)

    assert result.exit_code == 1
    assert result.timed_out == false
  end

  test "truncates output over 1MB" do
    script = Path.join(@support_dir, "fake_cli_large.sh")
    result = Runner.run_cli(script, [], 30_000)

    assert result.truncated == true
    assert byte_size(result.stdout) <= 1_048_576
  end

  test "probe_cli returns alive: true for successful command" do
    sh = Platform.shell()
    result = Runner.probe_cli(sh, ["-c", "exit 0"], 5_000)
    assert result.alive == true
  end

  test "probe_cli returns alive: false for failing command" do
    sh = Platform.shell()
    result = Runner.probe_cli(sh, ["-c", "exit 1"], 5_000)
    assert result.alive == false
    assert result.exit_code == 1
  end

  test "probe_cli times out" do
    sh = Platform.shell()
    result = Runner.probe_cli(sh, ["-c", "sleep 999"], 300)
    assert result.alive == false
    assert result.reason =~ "timeout"
  end

  describe "resolve_executable/1" do
    test "finds executable on system PATH" do
      assert Runner.resolve_executable("sh") != nil
    end

    test "returns nil for nonexistent executable" do
      assert Runner.resolve_executable("definitely_not_a_real_binary_xyz") == nil
    end

    test "respects ROUNDTABLE_<NAME>_PATH env var" do
      sh_path = System.find_executable("sh")
      System.put_env("ROUNDTABLE_SH_PATH", sh_path)
      assert Runner.resolve_executable("sh") == sh_path
    after
      System.delete_env("ROUNDTABLE_SH_PATH")
    end

    test "ROUNDTABLE_<NAME>_PATH returns nil if file does not exist" do
      System.put_env("ROUNDTABLE_FAKE_PATH", "/no/such/binary")
      assert Runner.resolve_executable("fake") == nil
    after
      System.delete_env("ROUNDTABLE_FAKE_PATH")
    end

    test "finds executable via ROUNDTABLE_EXTRA_PATH" do
      # Create a temp dir with a fake executable
      tmp = Path.join(System.tmp_dir!(), "rt_test_extra_path_#{System.unique_integer([:positive])}")
      File.mkdir_p!(tmp)
      fake_bin = Path.join(tmp, "roundtable_test_bin")
      File.write!(fake_bin, "#!/bin/sh\nexit 0")
      File.chmod!(fake_bin, 0o755)

      System.put_env("ROUNDTABLE_EXTRA_PATH", tmp)
      assert Runner.resolve_executable("roundtable_test_bin") == fake_bin
    after
      System.delete_env("ROUNDTABLE_EXTRA_PATH")
    end
  end

  test "no temp stderr files left behind" do
    tmp = System.tmp_dir!()

    before_count =
      tmp
      |> File.ls!()
      |> Enum.count(&String.starts_with?(&1, "rt_stderr_"))

    script = Path.join(@support_dir, "fake_cli_success.sh")
    Runner.run_cli(script, [], 5_000)

    after_count =
      tmp
      |> File.ls!()
      |> Enum.count(&String.starts_with?(&1, "rt_stderr_"))

    assert after_count == before_count
  end
end
