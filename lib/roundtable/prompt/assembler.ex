defmodule Roundtable.Prompt.Assembler do
  @moduledoc "Assembles prompts from role text, user request, and file references."

  @spec format_file_references([String.t()]) :: String.t() | nil
  def format_file_references([]), do: nil

  def format_file_references(file_paths) do
    refs =
      Enum.map(file_paths, fn path ->
        case File.stat(path) do
          {:ok, %{size: size}} -> "- #{path} (#{size} bytes)"
          {:error, _} -> "- #{path} (unavailable)"
        end
      end)

    "=== FILES ===\n" <>
      Enum.join(refs, "\n") <>
      "\n\nReview the files listed above using your own tools to read their contents."
  end

  @spec assemble(String.t(), String.t(), [String.t()]) :: String.t()
  def assemble(role_prompt, user_request, file_paths \\ []) do
    sections = [
      String.trim(role_prompt),
      "=== REQUEST ===\n" <> String.trim(user_request)
    ]

    file_refs = format_file_references(file_paths)

    sections =
      if file_refs do
        sections ++ [file_refs]
      else
        sections
      end

    Enum.join(sections, "\n\n")
  end
end
