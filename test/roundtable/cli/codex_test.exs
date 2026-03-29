defmodule Roundtable.CLI.CodexTest do
  use ExUnit.Case, async: true
  alias Roundtable.CLI.Codex

  @fixtures_dir Path.expand("../../../test/fixtures", __DIR__)
  defp read_fixture(name), do: File.read!(Path.join(@fixtures_dir, name))

  test "probe_args/0 returns [\"--version\"]" do
    assert Codex.probe_args() == ["--version"]
  end

  test "build_args/2 basic with no optional args" do
    args =
      Codex.build_args(
        %{codex_model: nil, codex_reasoning: nil, codex_resume: nil},
        "my prompt"
      )

    assert "exec" in args
    assert "--json" in args
    assert "--dangerously-bypass-approvals-and-sandbox" in args
    assert "my prompt" in args
    refute "-c" in args
  end

  test "build_args/2 with model" do
    args =
      Codex.build_args(%{codex_model: "gpt-4", codex_reasoning: nil, codex_resume: nil}, "p")

    assert "-c" in args
    assert "model=gpt-4" in args
  end

  test "build_args/2 with reasoning" do
    args =
      Codex.build_args(%{codex_model: nil, codex_reasoning: "high", codex_resume: nil}, "p")

    assert "reasoning_effort=high" in args
  end

  test "build_args/2 with resume last" do
    args =
      Codex.build_args(%{codex_model: nil, codex_reasoning: nil, codex_resume: "last"}, "p")

    assert "resume" in args
    assert "--last" in args
    assert "p" in args
  end

  test "build_args/2 with resume session id" do
    args =
      Codex.build_args(
        %{codex_model: nil, codex_reasoning: nil, codex_resume: "thread_xyz"},
        "p"
      )

    assert "resume" in args
    assert "thread_xyz" in args
    assert "p" in args
  end

  test "parse_output/2 parses successful JSONL" do
    stdout = read_fixture("codex_success.jsonl")
    result = Codex.parse_output(stdout, "")
    assert result.status == :ok
    assert result.response =~ "analysis"
    assert result.session_id == "thread_xyz789"
    assert result.metadata.usage != nil
    assert result.parse_error == nil
  end

  test "parse_output/2 messages joined with double newline" do
    jsonl =
      ~s({"type":"thread.started","thread_id":"t1"}\n{"type":"item.completed","item":{"type":"agent_message","text":"first"}}\n{"type":"item.completed","item":{"type":"agent_message","text":"second"}})

    result = Codex.parse_output(jsonl, "")
    assert result.status == :ok
    assert result.response == "first\n\nsecond"
  end

  test "parse_output/2 with error events" do
    stdout = read_fixture("codex_errors.jsonl")
    result = Codex.parse_output(stdout, "")
    assert result.status == :error
    assert result.response =~ "Authentication"
    assert result.parse_error == nil
  end

  test "parse_output/2 messages win over errors when both present" do
    jsonl =
      ~s({"type":"error","message":"err"}\n{"type":"item.completed","item":{"type":"agent_message","text":"answer"}})

    result = Codex.parse_output(jsonl, "")
    assert result.status == :ok
    assert result.response == "answer"
  end

  test "parse_output/2 empty output returns descriptive error" do
    result = Codex.parse_output("", "")
    assert result.status == :error
    assert result.parse_error == "No output from codex"
    assert result.response == ""
  end

  test "parse_output/2 blank lines only returns no output error" do
    stdout = read_fixture("codex_empty.jsonl")
    result = Codex.parse_output(stdout, "")
    assert result.status == :error
    assert result.parse_error == "No output from codex"
  end

  test "parse_output/2 raw text with no JSONL events" do
    result = Codex.parse_output("some raw text output", "")
    assert result.status == :error
    assert result.parse_error == "No JSONL events found; using raw output"
    assert result.response == "some raw text output"
  end

  test "parse_output/2 skips non-JSON lines gracefully" do
    jsonl =
      "not json\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"answer\"}}\nmore text"

    result = Codex.parse_output(jsonl, "")
    assert result.status == :ok
    assert result.response == "answer"
  end
end
