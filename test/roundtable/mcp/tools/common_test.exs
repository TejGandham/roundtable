defmodule Roundtable.MCP.Tools.CommonTest do
  use ExUnit.Case, async: false

  @moduletag timeout: 60_000

  alias Roundtable.MCP.Tools.Common

  @bin_dir Path.expand("../../../support/bin", __DIR__)

  setup do
    original_path = System.get_env("PATH", "")
    System.put_env("PATH", "#{@bin_dir}:#{original_path}")

    for script <- ["gemini", "codex", "claude"] do
      path = Path.join(@bin_dir, script)
      if File.exists?(path), do: File.chmod!(path, 0o755)
    end

    on_exit(fn -> System.put_env("PATH", original_path) end)
    :ok
  end

  defp base_params(overrides \\ %{}) do
    Map.merge(%{prompt: "test question", timeout: 30}, overrides)
  end

  defp dispatch_ok!(params, role_config) do
    {:ok, json} = Common.dispatch(params, role_config)
    Jason.decode!(json)
  end

  describe "dispatch/2 file parsing" do
    test "parses comma-separated files into list" do
      result = dispatch_ok!(base_params(%{files: "a.ts, b.ts,c.ts"}), %{role: "default"})
      assert result["meta"]["files_referenced"] == ["a.ts", "b.ts", "c.ts"]
    end

    test "handles nil files as empty list" do
      result = dispatch_ok!(base_params(%{files: nil}), %{role: "default"})
      assert result["meta"]["files_referenced"] == []
    end

    test "handles empty string files as empty list" do
      result = dispatch_ok!(base_params(%{files: ""}), %{role: "default"})
      assert result["meta"]["files_referenced"] == []
    end

    test "trims whitespace from individual file names" do
      result = dispatch_ok!(base_params(%{files: " a.ts , b.ts "}), %{role: "default"})
      assert result["meta"]["files_referenced"] == ["a.ts", "b.ts"]
    end

    test "rejects empty entries from trailing commas" do
      result = dispatch_ok!(base_params(%{files: "a.ts,,b.ts,"}), %{role: "default"})
      assert result["meta"]["files_referenced"] == ["a.ts", "b.ts"]
    end
  end

  describe "dispatch/2 timeout" do
    test "defaults to 900 when timeout is nil" do
      result = dispatch_ok!(base_params(%{timeout: nil}), %{role: "default"})
      assert is_map(result["meta"])
    end

    test "uses explicit timeout when provided" do
      result = dispatch_ok!(base_params(%{timeout: 10}), %{role: "default"})
      assert is_map(result["meta"])
    end
  end

  describe "dispatch/2 role config passthrough" do
    test "unified role applied to all models" do
      result = dispatch_ok!(base_params(), %{role: "planner"})
      assert result["meta"]["gemini_role"] == "planner"
      assert result["meta"]["codex_role"] == "planner"
      assert result["meta"]["claude_role"] == "planner"
    end

    test "per-model roles override unified role" do
      result =
        dispatch_ok!(base_params(), %{
          role: nil,
          gemini_role: "planner",
          codex_role: "codereviewer",
          claude_role: "default"
        })

      assert result["meta"]["gemini_role"] == "planner"
      assert result["meta"]["codex_role"] == "codereviewer"
      assert result["meta"]["claude_role"] == "default"
    end

    test "default role is 'default' when role_config omits key" do
      result = dispatch_ok!(base_params(), %{})
      assert result["meta"]["gemini_role"] == "default"
      assert result["meta"]["codex_role"] == "default"
      assert result["meta"]["claude_role"] == "default"
    end
  end

  describe "dispatch/2 model and resume params" do
    test "succeeds with model params" do
      {:ok, _json} =
        Common.dispatch(
          base_params(%{gemini_model: "gemini-2.0", codex_model: "gpt-4", claude_model: "opus"}),
          %{role: "default"}
        )
    end

    test "succeeds with resume params" do
      {:ok, _json} =
        Common.dispatch(
          base_params(%{gemini_resume: "ses_1", codex_resume: "ses_2", claude_resume: "ses_3"}),
          %{role: "default"}
        )
    end
  end

  describe "dispatch/2 error handling" do
    test "returns error tuple for nonexistent role" do
      {:error, msg} = Common.dispatch(base_params(), %{role: "nonexistent_role_xyz"})
      assert msg =~ "Role prompt not found"
    end
  end
end
