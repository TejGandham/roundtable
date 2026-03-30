defmodule Roundtable.CLI.ClaudeTest do
  use ExUnit.Case, async: true
  alias Roundtable.CLI.Claude

  @fixtures_dir Path.expand("../../../test/fixtures", __DIR__)

  defp read_fixture(name), do: File.read!(Path.join(@fixtures_dir, name))

  test "probe_args/0 returns [\"--version\"]" do
    assert Claude.probe_args() == ["--version"]
  end

  test "build_args/2 basic prompt" do
    args = Claude.build_args(%{claude_model: nil, claude_resume: nil}, "my prompt")
    assert "-p" in args
    assert "--output-format" in args
    assert "json" in args
    assert "--dangerously-skip-permissions" in args
    assert List.last(args) == "my prompt"
    refute "--model" in args
    refute "-r" in args
  end

  test "build_args/2 with model override" do
    args = Claude.build_args(%{claude_model: "opus", claude_resume: nil}, "p")
    assert "--model" in args
    assert "opus" in args
  end

  test "build_args/2 with resume" do
    args = Claude.build_args(%{claude_model: nil, claude_resume: "ses_abc"}, "p")
    assert "-r" in args
    assert "ses_abc" in args
    assert List.last(args) == "p"
  end

  test "parse_output/2 parses successful Claude JSON" do
    stdout = read_fixture("claude_success.json")
    result = Claude.parse_output(stdout, "")
    assert result.status == :ok
    assert is_binary(result.response)
    assert result.response != ""
    assert result.session_id == "78ca29ae-5083-42e9-a705-bc55da0bf188"
    assert result.parse_error == nil
  end

  test "parse_output/2 metadata model_used stripped of ANSI" do
    stdout = read_fixture("claude_success.json")
    result = Claude.parse_output(stdout, "")
    assert result.metadata.model_used == "claude-opus-4-6"
  end

  test "parse_output/2 parses Claude error JSON (is_error: true)" do
    stdout = read_fixture("claude_error.json")
    result = Claude.parse_output(stdout, "")
    assert result.status == :error
    assert result.parse_error == nil
    assert is_binary(result.response)
    assert result.response != ""
  end

  test "parse_output/2 handles completely malformed output" do
    result = Claude.parse_output("garbage", "stderr")
    assert result.status == :error
    assert result.parse_error != nil
    assert result.response == "garbage"
  end

  test "parse_output/2 with empty stdout returns error" do
    result = Claude.parse_output("", "stderr")
    assert result.status == :error
    assert result.response == ""
  end
end
