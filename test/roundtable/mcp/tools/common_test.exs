defmodule Roundtable.MCP.Tools.CommonTest do
  use ExUnit.Case, async: false

  @moduletag timeout: 60_000

  alias Roundtable.MCP.Tools.Common
  alias Hermes.Server.Response

  @bin_dir Path.expand("../../../support/bin", __DIR__)
  @fake_frame nil

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
    {:reply, %Response{} = resp, @fake_frame} = Common.dispatch(params, role_config, @fake_frame)
    assert resp.isError == false
    [%{"type" => "text", "text" => json}] = resp.content
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
      {:reply, %Response{isError: false}, @fake_frame} =
        Common.dispatch(
          base_params(%{gemini_model: "gemini-2.0", codex_model: "gpt-4", claude_model: "opus"}),
          %{role: "default"},
          @fake_frame
        )
    end

    test "succeeds with resume params" do
      {:reply, %Response{isError: false}, @fake_frame} =
        Common.dispatch(
          base_params(%{gemini_resume: "ses_1", codex_resume: "ses_2", claude_resume: "ses_3"}),
          %{role: "default"},
          @fake_frame
        )
    end
  end

  describe "dispatch/3 returns hermes-compatible response tuple" do
    test "success returns {:reply, %Response{isError: false}, frame}" do
      result = Common.dispatch(base_params(), %{role: "default"}, @fake_frame)
      assert {:reply, %Response{isError: false} = resp, @fake_frame} = result
      assert [%{"type" => "text", "text" => json}] = resp.content
      assert {:ok, _} = Jason.decode(json)
    end
  end

  describe "dispatch/3 with agents param" do
    test "agents nil dispatches all 3 default agents" do
      result = dispatch_ok!(base_params(%{agents: nil}), %{role: "default"})
      assert Map.has_key?(result, "gemini")
      assert Map.has_key?(result, "codex")
      assert Map.has_key?(result, "claude")
    end

    test "agents selects subset of agents" do
      agents = Jason.encode!([%{"cli" => "gemini"}, %{"cli" => "codex"}])
      result = dispatch_ok!(base_params(%{agents: agents}), %{role: "default"})
      assert Map.has_key?(result, "gemini")
      assert Map.has_key?(result, "codex")
      refute Map.has_key?(result, "claude")
    end

    test "agents with custom names as result keys" do
      agents =
        Jason.encode!([
          %{"name" => "fast", "cli" => "codex"},
          %{"name" => "deep", "cli" => "codex"}
        ])

      result = dispatch_ok!(base_params(%{agents: agents}), %{role: "default"})
      assert Map.has_key?(result, "fast")
      assert Map.has_key?(result, "deep")
      refute Map.has_key?(result, "codex")
    end

    test "agents with single agent" do
      agents = Jason.encode!([%{"cli" => "gemini"}])
      result = dispatch_ok!(base_params(%{agents: agents}), %{role: "default"})
      assert Map.has_key?(result, "gemini")
      assert Map.has_key?(result, "meta")
      refute Map.has_key?(result, "codex")
      refute Map.has_key?(result, "claude")
    end

    test "duplicate agent names returns error" do
      agents = Jason.encode!([%{"cli" => "gemini"}, %{"cli" => "gemini"}])

      {:reply, %Response{isError: true} = resp, @fake_frame} =
        Common.dispatch(base_params(%{agents: agents}), %{role: "default"}, @fake_frame)

      [%{"type" => "text", "text" => msg}] = resp.content
      assert msg =~ "duplicate agent names"
    end

    test "invalid CLI type returns error" do
      agents = Jason.encode!([%{"cli" => "bard"}])

      {:reply, %Response{isError: true} = resp, @fake_frame} =
        Common.dispatch(base_params(%{agents: agents}), %{role: "default"}, @fake_frame)

      [%{"type" => "text", "text" => msg}] = resp.content
      assert msg =~ "unknown CLI type: bard"
    end

    test "empty agents list returns error" do
      agents = Jason.encode!([])

      {:reply, %Response{isError: true} = resp, @fake_frame} =
        Common.dispatch(base_params(%{agents: agents}), %{role: "default"}, @fake_frame)

      [%{"type" => "text", "text" => msg}] = resp.content
      assert msg =~ "agents list cannot be empty"
    end

    test "invalid JSON returns error" do
      {:reply, %Response{isError: true} = resp, @fake_frame} =
        Common.dispatch(base_params(%{agents: "not json"}), %{role: "default"}, @fake_frame)

      [%{"type" => "text", "text" => msg}] = resp.content
      assert msg =~ "not valid JSON"
    end

    test "missing cli field returns error" do
      agents = Jason.encode!([%{"name" => "oops"}])

      {:reply, %Response{isError: true} = resp, @fake_frame} =
        Common.dispatch(base_params(%{agents: agents}), %{role: "default"}, @fake_frame)

      [%{"type" => "text", "text" => msg}] = resp.content
      assert msg =~ "must specify a \"cli\" field"
    end
  end

  describe "dispatch/3 error handling" do
    test "returns error response for nonexistent role" do
      {:reply, %Response{isError: true} = resp, @fake_frame} =
        Common.dispatch(base_params(), %{role: "nonexistent_role_xyz"}, @fake_frame)

      [%{"type" => "text", "text" => msg}] = resp.content
      assert msg =~ "Role prompt not found"
    end
  end
end
