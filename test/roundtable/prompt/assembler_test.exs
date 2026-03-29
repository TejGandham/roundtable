defmodule Roundtable.Prompt.AssemblerTest do
  use ExUnit.Case, async: true
  alias Roundtable.Prompt.Assembler

  @role_text "You are a senior engineer."
  @request "Review this code"

  test "assembles prompt without files" do
    result = Assembler.assemble(@role_text, @request, [])
    assert result == "You are a senior engineer.\n\n=== REQUEST ===\nReview this code"
  end

  test "assembles prompt with files" do
    path = "test/test_helper.exs"
    result = Assembler.assemble(@role_text, @request, [path])
    assert result =~ "=== REQUEST ==="
    assert result =~ "=== FILES ==="
    assert result =~ path
    assert result =~ "bytes)"
    assert result =~ "Review the files listed above"
  end

  test "handles unavailable files gracefully" do
    result = Assembler.assemble(@role_text, @request, ["/nonexistent/path.ts"])
    assert result =~ "- /nonexistent/path.ts (unavailable)"
  end

  test "no FILES section when file list is empty" do
    result = Assembler.assemble(@role_text, @request, [])
    refute result =~ "=== FILES ==="
  end

  test "trims whitespace from role prompt" do
    result = Assembler.assemble("  role text  ", @request, [])
    assert String.starts_with?(result, "role text")
  end

  test "trims whitespace from user request" do
    result = Assembler.assemble(@role_text, "  my request  ", [])
    assert result =~ "=== REQUEST ===\nmy request"
  end

  test "format_file_references returns nil for empty list" do
    assert Assembler.format_file_references([]) == nil
  end

  test "format_file_references shows file size for existing file" do
    path = "test/test_helper.exs"
    result = Assembler.format_file_references([path])
    assert result =~ "=== FILES ==="
    assert result =~ path
    assert result =~ "bytes)"
  end
end
