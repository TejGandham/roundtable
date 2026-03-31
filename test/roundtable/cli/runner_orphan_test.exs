defmodule Roundtable.CLI.RunnerOrphanTest do
  use ExUnit.Case, async: false
  @moduletag timeout: 30_000

  @bin_dir Path.expand("../../support/bin", __DIR__)

  setup do
    original_path = System.get_env("PATH", "")
    System.put_env("PATH", "#{@bin_dir}:#{original_path}")

    for script <- ["gemini", "gemini_timeout"] do
      path = Path.join(@bin_dir, script)
      if File.exists?(path), do: File.chmod!(path, 0o755)
    end

    on_exit(fn ->
      System.put_env("PATH", original_path)
      :os.cmd(~c"pkill -f 'sleep 999' 2>/dev/null; true")
      Process.sleep(200)
    end)

    :ok
  end

  test "no orphan processes after brutal task kill" do
    {:ok, sup} = Task.Supervisor.start_link()

    # async_nolink: task is linked to the supervisor, NOT to the test process.
    # This means killing the task won't propagate :kill to this test process.
    task =
      Task.Supervisor.async_nolink(sup, fn ->
        executable = System.find_executable("gemini_timeout")
        Roundtable.CLI.Runner.run_cli(executable, ["--run"], 60_000)
      end)

    Process.sleep(500)

    pids_before = find_sleep_pids()
    assert length(pids_before) > 0, "Expected sleep 999 to be running before kill"

    Process.unlink(sup)
    Process.exit(task.pid, :kill)
    Process.exit(sup, :kill)

    Process.sleep(1500)

    pids_after = find_sleep_pids()

    assert pids_after == [],
           "Orphan processes found after brutal kill: #{inspect(pids_after)}"
  end

  test "no orphan processes after normal run_cli completion" do
    executable = System.find_executable("gemini")
    _result = Roundtable.CLI.Runner.run_cli(executable, ["--run"], 5_000)
    assert find_sleep_pids() == []
  end

  defp find_sleep_pids do
    :os.cmd(~c"pgrep -f 'sleep 999' 2>/dev/null")
    |> to_string()
    |> String.split()
    |> Enum.reject(&(&1 == ""))
  end
end
