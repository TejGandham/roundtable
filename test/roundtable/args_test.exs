defmodule Roundtable.ArgsTest do
  use ExUnit.Case, async: true
  alias Roundtable.Args

  test "parses --prompt flag" do
    {:ok, args} = Args.parse(["--prompt", "hello world"])
    assert args.prompt == "hello world"
  end

  test "uses positional argument as prompt" do
    {:ok, args} = Args.parse(["hello world"])
    assert args.prompt == "hello world"
  end

  test "all defaults are correct" do
    {:ok, args} = Args.parse(["--prompt", "test"])

    assert args.role == "default"
    assert args.timeout == 900
    assert args.files == []
    assert args.gemini_model == nil
    assert args.codex_model == nil
    assert args.gemini_role == nil
    assert args.codex_role == nil
    assert args.codex_reasoning == nil
    assert args.gemini_resume == nil
    assert args.codex_resume == nil
  end

  test "missing prompt returns error" do
    assert {:error, msg} = Args.parse([])
    assert msg =~ "Missing required --prompt"
  end

  test "invalid timeout (string) returns error" do
    assert {:error, _} = Args.parse(["--prompt", "test", "--timeout", "abc"])
  end

  test "zero timeout returns error" do
    assert {:error, msg} = Args.parse(["--prompt", "test", "--timeout", "0"])
    assert msg =~ "positive integer"
  end

  test "parses --files with comma separation" do
    {:ok, args} = Args.parse(["--prompt", "test", "--files", "a.ts,b.ts, c.ts"])
    assert args.files == ["a.ts", "b.ts", "c.ts"]
  end

  test "parses --gemini-role and --codex-role separately" do
    {:ok, args} = Args.parse(["--prompt", "test", "--gemini-role", "planner", "--codex-role", "codereviewer"])
    assert args.gemini_role == "planner"
    assert args.codex_role == "codereviewer"
  end

  test "parses all resume flags" do
    {:ok, args} = Args.parse(["--prompt", "test", "--gemini-resume", "latest", "--codex-resume", "last"])
    assert args.gemini_resume == "latest"
    assert args.codex_resume == "last"
  end

  test "parses --codex-reasoning" do
    {:ok, args} = Args.parse(["--prompt", "test", "--codex-reasoning", "high"])
    assert args.codex_reasoning == "high"
  end

  test "uses project root roles dir in tests" do
    {:ok, args} = Args.parse(["--prompt", "test"])
    assert args.roles_dir == Path.expand("roles")
  end

  test "unknown flag returns error" do
    assert {:error, msg} = Args.parse(["--prompt", "test", "--unknown", "x"])
    assert msg =~ "Unknown flag"
  end
end
