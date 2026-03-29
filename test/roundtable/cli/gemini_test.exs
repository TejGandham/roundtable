defmodule Roundtable.CLI.GeminiTest do
  use ExUnit.Case, async: true
  alias Roundtable.CLI.Gemini

  @fixtures_dir Path.expand("../../../test/fixtures", __DIR__)

  defp read_fixture(name), do: File.read!(Path.join(@fixtures_dir, name))

  test "probe_args/0 returns [\"--version\"]" do
    assert Gemini.probe_args() == ["--version"]
  end

  test "build_args/2 basic prompt" do
    args = Gemini.build_args(%{gemini_model: nil, gemini_resume: nil}, "my prompt")
    assert "-p" in args
    assert "my prompt" in args
    assert "-o" in args
    assert "json" in args
    assert "--yolo" in args
    refute "-m" in args
  end

  test "build_args/2 with model override" do
    args = Gemini.build_args(%{gemini_model: "gemini-2.5-pro", gemini_resume: nil}, "p")
    assert "-m" in args
    assert "gemini-2.5-pro" in args
  end

  test "build_args/2 with resume puts --resume before -p" do
    args = Gemini.build_args(%{gemini_model: nil, gemini_resume: "ses_123"}, "p")
    resume_idx = Enum.find_index(args, &(&1 == "--resume"))
    p_idx = Enum.find_index(args, &(&1 == "-p"))
    assert resume_idx != nil
    assert resume_idx < p_idx
    assert "ses_123" in args
  end

  test "parse_output/2 parses successful Gemini JSON" do
    stdout = read_fixture("gemini_success.json")
    result = Gemini.parse_output(stdout, "")
    assert result.status == :ok
    assert result.response == "test response"
    assert result.metadata.model_used == "gemini-2.5-pro"
    assert result.session_id == "ses_abc123"
    assert result.parse_error == nil
  end

  test "parse_output/2 parses Gemini error JSON" do
    stdout = read_fixture("gemini_error.json")
    result = Gemini.parse_output(stdout, "")
    assert result.status == :error
    assert result.response == "Rate limit exceeded"
    assert result.parse_error == nil
  end

  test "parse_output/2 falls back to stderr on malformed stdout" do
    stderr = read_fixture("gemini_stderr_error.json")
    result = Gemini.parse_output("not valid json", stderr)
    assert result.status == :error
    assert result.response == "Auth failed"
    assert result.parse_error == nil
  end

  test "parse_output/2 handles completely malformed output" do
    result = Gemini.parse_output("garbage stdout", "garbage stderr")
    assert result.status == :error
    assert result.parse_error != nil
    assert result.response == "garbage stdout"
  end

  test "parse_output/2 with empty stdout uses stderr as response" do
    result = Gemini.parse_output("", "some stderr")
    assert result.status == :error
    assert result.response == "some stderr"
  end
end
