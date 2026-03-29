defmodule Roundtable.CLI.RunnerTest do
  use ExUnit.Case
  alias Roundtable.CLI.Runner

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
    sh = System.find_executable("sh") || "/bin/sh"
    result = Runner.probe_cli(sh, ["-c", "exit 0"], 5_000)
    assert result.alive == true
  end

  test "probe_cli returns alive: false for failing command" do
    sh = System.find_executable("sh") || "/bin/sh"
    result = Runner.probe_cli(sh, ["-c", "exit 1"], 5_000)
    assert result.alive == false
    assert result.exit_code == 1
  end

  test "probe_cli times out" do
    sh = System.find_executable("sh") || "/bin/sh"
    result = Runner.probe_cli(sh, ["-c", "sleep 999"], 300)
    assert result.alive == false
    assert result.reason =~ "timeout"
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
