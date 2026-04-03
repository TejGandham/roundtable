defmodule Roundtable.IntegrationTest do
  @moduledoc "End-to-end tests running the actual ./roundtable escript with mock CLIs."
  use ExUnit.Case, async: false

  @moduletag timeout: 120_000

  @escript_path Path.expand("../roundtable-cli", __DIR__)
  @bin_dir Path.expand("support/bin", __DIR__)
  @roles_dir Path.expand("../roles", __DIR__)
  @timeout 30_000

  setup_all do
    project_dir = Path.expand("..", __DIR__)
    {_, 0} = System.cmd("mix", ["escript.build"], cd: project_dir)
    :ok
  end

  setup do
    for script <- ["gemini", "codex", "claude", "gemini_timeout", "gemini_rate_limited"] do
      path = Path.join(@bin_dir, script)
      if File.exists?(path), do: File.chmod!(path, 0o755)
    end

    :ok
  end

  defp run_roundtable(extra_args, opts \\ []) do
    timeout = Keyword.get(opts, :timeout, @timeout)
    env_extra = Keyword.get(opts, :env, [])
    bin_dir = Keyword.get(opts, :bin_dir, @bin_dir)

    current_path = System.get_env("PATH", "")
    env = [{"PATH", "#{bin_dir}:#{@bin_dir}:#{current_path}"}] ++ env_extra

    args = ["--roles-dir", @roles_dir] ++ extra_args

    task =
      Task.async(fn ->
        System.cmd(@escript_path, args, env: env)
      end)

    case Task.yield(task, timeout) do
      {:ok, {stdout, exit_code}} ->
        case Jason.decode(stdout) do
          {:ok, json} -> {:ok, json, exit_code}
          {:error, _} -> {:raw, stdout, exit_code}
        end

      nil ->
        Task.shutdown(task, :brutal_kill)
        {:timeout}
    end
  end

  defp with_script_replacement(target_name, replacement_name, fun) do
    temp_dir =
      Path.join(System.tmp_dir!(), "roundtable_bin_#{System.unique_integer([:positive])}")

    replacement_path = Path.join(@bin_dir, replacement_name)
    target_path = Path.join(temp_dir, target_name)

    File.mkdir_p!(temp_dir)
    File.cp!(replacement_path, target_path)
    File.chmod!(target_path, 0o755)

    try do
      fun.(temp_dir)
    after
      File.rm_rf!(temp_dir)
    end
  end

  test "full success: both mock CLIs return ok" do
    {:ok, result, exit_code} = run_roundtable(["--prompt", "test question", "--timeout", "30"])

    assert exit_code == 0
    assert result["gemini"]["status"] == "ok"
    assert result["codex"]["status"] == "ok"
    assert result["claude"]["status"] == "ok"
    assert result["gemini"]["response"] == "test response"
    assert result["codex"]["response"] == "Here is my analysis of the code."
    assert result["claude"]["response"] == "\n\nHello from Claude"
    assert result["gemini"]["session_id"] == "ses_abc123"
    assert result["codex"]["session_id"] == "thread_xyz789"
    assert result["claude"]["session_id"] == "sess_claude_001"
    assert result["meta"]["gemini_role"] == "default"
    assert result["meta"]["codex_role"] == "default"
    assert result["meta"]["claude_role"] == "default"
  end

  test "recursion guard: ROUNDTABLE_ACTIVE=1 exits with error" do
    {:ok, result, exit_code} =
      run_roundtable(
        ["--prompt", "test"],
        env: [{"ROUNDTABLE_ACTIVE", "1"}]
      )

    assert exit_code == 1
    assert result["error"] =~ "Recursive invocation"
  end

  test "missing prompt: exits 1 with usage error" do
    {:ok, result, exit_code} = run_roundtable([])

    assert exit_code == 1
    assert Map.has_key?(result, "error")
    assert Map.has_key?(result, "usage")
    assert result["error"] =~ "Missing required --prompt"
  end

  test "CLI not found: status is not_found when PATH has no CLIs" do
    clean_path =
      System.get_env("PATH", "")
      |> String.split(":")
      |> Enum.reject(fn dir ->
        File.exists?(Path.join(dir, "gemini")) or File.exists?(Path.join(dir, "codex")) or
          File.exists?(Path.join(dir, "claude"))
      end)
      |> Enum.join(":")

    task =
      Task.async(fn ->
        System.cmd(
          @escript_path,
          ["--prompt", "test", "--timeout", "5", "--roles-dir", @roles_dir],
          env: [{"PATH", clean_path}]
        )
      end)

    {:ok, {stdout, exit_code}} = Task.yield(task, @timeout)
    {:ok, result} = Jason.decode(stdout)

    assert exit_code == 0
    assert result["gemini"]["status"] == "not_found"
    assert result["codex"]["status"] == "not_found"
    assert result["claude"]["status"] == "not_found"
  end

  test "planner role: meta shows correct role" do
    {:ok, result, exit_code} =
      run_roundtable(["--prompt", "test", "--role", "planner", "--timeout", "30"])

    assert exit_code == 0
    assert result["meta"]["gemini_role"] == "planner"
    assert result["meta"]["codex_role"] == "planner"
    assert result["meta"]["claude_role"] == "planner"
  end

  test "per-CLI roles: gemini-role and codex-role set independently" do
    {:ok, result, exit_code} =
      run_roundtable([
        "--prompt",
        "test",
        "--gemini-role",
        "planner",
        "--codex-role",
        "codereviewer",
        "--timeout",
        "30"
      ])

    assert exit_code == 0
    assert result["meta"]["gemini_role"] == "planner"
    assert result["meta"]["codex_role"] == "codereviewer"
    assert result["meta"]["claude_role"] == "default"
  end

  test "files referenced: files_referenced in meta" do
    {:ok, result, exit_code} =
      run_roundtable([
        "--prompt",
        "test",
        "--files",
        "test/test_helper.exs",
        "--timeout",
        "30"
      ])

    assert exit_code == 0
    assert result["meta"]["files_referenced"] == ["test/test_helper.exs"]
  end

  test "timeout: CLI that sleeps gets timeout status" do
    with_script_replacement("gemini", "gemini_timeout", fn temp_dir ->
      {:ok, result, exit_code} =
        run_roundtable(["--prompt", "test", "--timeout", "2"], timeout: 30_000, bin_dir: temp_dir)

      assert exit_code == 0
      assert result["gemini"]["status"] == "timeout"
      assert result["gemini"]["response"] =~ "Request timed out after"
      assert result["gemini"]["response"] =~ "Retry with a longer timeout or resume the session"
      assert result["gemini"]["parse_error"] == nil
      assert result["codex"]["status"] == "ok"
    end)
  end

  test "gemini 429 is reported as rate_limited with actionable feedback" do
    with_script_replacement("gemini", "gemini_rate_limited", fn temp_dir ->
      {:ok, result, exit_code} =
        run_roundtable(["--prompt", "test", "--timeout", "30"],
          timeout: 30_000,
          bin_dir: temp_dir
        )

      assert exit_code == 0
      assert result["gemini"]["status"] == "rate_limited"
      assert result["gemini"]["response"] =~ "Gemini rate limited"
      assert result["gemini"]["response"] =~ "Retry later or resume the session"
      assert result["codex"]["status"] == "ok"
      assert result["claude"]["status"] == "ok"
    end)
  end

  test "output is structurally valid JSON with all required keys" do
    {:ok, result, exit_code} = run_roundtable(["--prompt", "test", "--timeout", "30"])

    assert exit_code == 0

    for key <- [
          "response",
          "model",
          "status",
          "exit_code",
          "elapsed_ms",
          "parse_error",
          "truncated",
          "stderr_truncated",
          "session_id"
        ] do
      assert Map.has_key?(result["gemini"], key), "gemini missing key: #{key}"
    end

    for key <- [
          "response",
          "model",
          "status",
          "exit_code",
          "elapsed_ms",
          "parse_error",
          "truncated",
          "stderr_truncated",
          "session_id"
        ] do
      assert Map.has_key?(result["codex"], key), "codex missing key: #{key}"
    end

    for key <- [
          "response",
          "model",
          "status",
          "exit_code",
          "elapsed_ms",
          "parse_error",
          "truncated",
          "stderr_truncated",
          "session_id"
        ] do
      assert Map.has_key?(result["claude"], key), "claude missing key: #{key}"
    end

    for key <- [
          "total_elapsed_ms",
          "gemini_role",
          "codex_role",
          "claude_role",
          "files_referenced"
        ] do
      assert Map.has_key?(result["meta"], key), "meta missing key: #{key}"
    end
  end
end
