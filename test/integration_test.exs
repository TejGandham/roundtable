defmodule Roundtable.IntegrationTest do
  @moduledoc "End-to-end tests running the actual ./roundtable escript with mock CLIs."
  use ExUnit.Case, async: false

  @moduletag timeout: 120_000

  @escript_path Path.expand("../roundtable", __DIR__)
  @bin_dir Path.expand("support/bin", __DIR__)
  @roles_dir Path.expand("../roles", __DIR__)
  @timeout 30_000

  setup do
    for script <- ["gemini", "codex", "gemini_timeout"] do
      path = Path.join(@bin_dir, script)
      if File.exists?(path), do: File.chmod!(path, 0o755)
    end

    unless File.exists?(@escript_path) do
      project_dir = Path.expand("..", __DIR__)
      {_, 0} = System.cmd("mix", ["escript.build"], cd: project_dir)
    end

    :ok
  end

  defp run_roundtable(extra_args, opts \\ []) do
    timeout = Keyword.get(opts, :timeout, @timeout)
    env_extra = Keyword.get(opts, :env, [])

    current_path = System.get_env("PATH", "")
    env = [{"PATH", "#{@bin_dir}:#{current_path}"}] ++ env_extra

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

  test "full success: both mock CLIs return ok" do
    {:ok, result, exit_code} = run_roundtable(["--prompt", "test question", "--timeout", "30"])

    assert exit_code == 0
    assert result["gemini"]["status"] == "ok"
    assert result["codex"]["status"] == "ok"
    assert result["gemini"]["response"] == "test response"
    assert result["codex"]["response"] == "Here is my analysis of the code."
    assert result["gemini"]["session_id"] == "ses_abc123"
    assert result["codex"]["session_id"] == "thread_xyz789"
    assert result["meta"]["gemini_role"] == "default"
    assert result["meta"]["codex_role"] == "default"
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
        File.exists?(Path.join(dir, "gemini")) or File.exists?(Path.join(dir, "codex"))
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
  end

  test "planner role: meta shows correct role" do
    {:ok, result, exit_code} =
      run_roundtable(["--prompt", "test", "--role", "planner", "--timeout", "30"])

    assert exit_code == 0
    assert result["meta"]["gemini_role"] == "planner"
    assert result["meta"]["codex_role"] == "planner"
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
    gemini_path = Path.join(@bin_dir, "gemini")
    timeout_path = Path.join(@bin_dir, "gemini_timeout")

    {:ok, original_content} = File.read(gemini_path)
    File.write!(gemini_path, File.read!(timeout_path))
    File.chmod!(gemini_path, 0o755)

    try do
      {:ok, result, exit_code} =
        run_roundtable(["--prompt", "test", "--timeout", "2"], timeout: 30_000)

      assert exit_code == 0
      assert result["gemini"]["status"] == "timeout"
      assert result["codex"]["status"] == "ok"
    after
      File.write!(gemini_path, original_content)
      File.chmod!(gemini_path, 0o755)
    end
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
          "session_id"
        ] do
      assert Map.has_key?(result["codex"], key), "codex missing key: #{key}"
    end

    for key <- ["total_elapsed_ms", "gemini_role", "codex_role", "files_referenced"] do
      assert Map.has_key?(result["meta"], key), "meta missing key: #{key}"
    end
  end
end
