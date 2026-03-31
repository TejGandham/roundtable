defmodule Roundtable.MCP.Tools.ToolsTest do
  use ExUnit.Case, async: false

  @moduletag timeout: 60_000

  alias Roundtable.MCP.Tools.{Hivemind, Deepdive, Architect, Challenge, Xray}
  alias Hermes.Server.Response

  @bin_dir Path.expand("../../../support/bin", __DIR__)
  @prompt_file "/tmp/roundtable_test_prompt.txt"
  @fake_frame nil

  setup do
    original_path = System.get_env("PATH", "")
    System.put_env("PATH", "#{@bin_dir}:#{original_path}")

    for script <- ["gemini", "codex", "claude", "gemini_echo"] do
      path = Path.join(@bin_dir, script)
      if File.exists?(path), do: File.chmod!(path, 0o755)
    end

    on_exit(fn ->
      System.put_env("PATH", original_path)
      File.rm(@prompt_file)
    end)

    :ok
  end

  defp base_params, do: %{prompt: "test question", files: nil, timeout: 30}

  defp execute_and_parse(module) do
    {:reply, %Response{isError: false} = resp, @fake_frame} =
      module.execute(base_params(), @fake_frame)

    [%{"type" => "text", "text" => json}] = resp.content
    Jason.decode!(json)
  end

  defp with_echo_gemini(fun) do
    gemini_path = Path.join(@bin_dir, "gemini")
    echo_path = Path.join(@bin_dir, "gemini_echo")
    {:ok, original} = File.read(gemini_path)

    File.cp!(echo_path, gemini_path)
    File.chmod!(gemini_path, 0o755)
    File.rm(@prompt_file)

    try do
      result = fun.()
      prompt = File.read!(@prompt_file)
      {result, prompt}
    after
      File.write!(gemini_path, original)
      File.chmod!(gemini_path, 0o755)
    end
  end

  describe "Hivemind.execute/2" do
    test "assigns default role to all models" do
      result = execute_and_parse(Hivemind)
      assert result["meta"]["gemini_role"] == "default"
      assert result["meta"]["codex_role"] == "default"
      assert result["meta"]["claude_role"] == "default"
    end

    test "returns valid three-model response" do
      result = execute_and_parse(Hivemind)
      assert is_map(result["gemini"])
      assert is_map(result["codex"])
      assert is_map(result["claude"])
      assert is_map(result["meta"])
    end
  end

  describe "Deepdive.execute/2" do
    test "assigns planner role to all models" do
      result = execute_and_parse(Deepdive)
      assert result["meta"]["gemini_role"] == "planner"
      assert result["meta"]["codex_role"] == "planner"
      assert result["meta"]["claude_role"] == "planner"
    end

    test "appends analysis guidance to prompt" do
      {_result, prompt} =
        with_echo_gemini(fn -> Deepdive.execute(base_params(), nil) end)

      assert prompt =~ "test question"
      assert prompt =~ "conclusions, assumptions, alternatives, and confidence level"
    end
  end

  describe "Architect.execute/2" do
    test "assigns planner role to all models" do
      result = execute_and_parse(Architect)
      assert result["meta"]["gemini_role"] == "planner"
      assert result["meta"]["codex_role"] == "planner"
      assert result["meta"]["claude_role"] == "planner"
    end

    test "appends architecture guidance to prompt" do
      {_result, prompt} =
        with_echo_gemini(fn -> Architect.execute(base_params(), nil) end)

      assert prompt =~ "test question"
      assert prompt =~ "phases, dependencies, risks, and milestones"
    end
  end

  describe "Challenge.execute/2" do
    test "assigns codereviewer role to all models" do
      result = execute_and_parse(Challenge)
      assert result["meta"]["gemini_role"] == "codereviewer"
      assert result["meta"]["codex_role"] == "codereviewer"
      assert result["meta"]["claude_role"] == "codereviewer"
    end

    test "appends critical review guidance to prompt" do
      {_result, prompt} =
        with_echo_gemini(fn -> Challenge.execute(base_params(), nil) end)

      assert prompt =~ "test question"
      assert prompt =~ "critical reviewer"
    end
  end

  describe "tool descriptions (MCP protocol compliance)" do
    for {mod, name} <- [
          {Hivemind, "hivemind"},
          {Deepdive, "deepdive"},
          {Architect, "architect"},
          {Challenge, "challenge"},
          {Xray, "xray"}
        ] do
      test "#{name} has a non-nil description" do
        desc = unquote(mod).__description__()
        assert is_binary(desc) and desc != ""
      end
    end
  end

  describe "Xray.execute/2" do
    test "assigns per-model roles: planner, codereviewer, default" do
      result = execute_and_parse(Xray)
      assert result["meta"]["gemini_role"] == "planner"
      assert result["meta"]["codex_role"] == "codereviewer"
      assert result["meta"]["claude_role"] == "default"
    end

    test "does not modify the original prompt" do
      {_result, prompt} =
        with_echo_gemini(fn -> Xray.execute(base_params(), nil) end)

      assert prompt =~ "test question"
      refute prompt =~ "conclusions"
      refute prompt =~ "critical reviewer"
      refute prompt =~ "phases, dependencies"
    end
  end
end
