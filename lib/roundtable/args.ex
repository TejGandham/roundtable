defmodule Roundtable.Args do
  @moduledoc "Parses CLI arguments for the roundtable escript."

  @spec parse([String.t()]) :: {:ok, map()} | {:error, String.t()}
  def parse(argv) do
    {parsed, rest, invalid} =
      OptionParser.parse(argv,
        strict: [
          {:prompt, :string},
          {:role, :string},
          {:gemini_role, :string},
          {:codex_role, :string},
          {:files, :string},
          {:gemini_model, :string},
          {:codex_model, :string},
          {:timeout, :integer},
          {:roles_dir, :string},
          {:project_roles_dir, :string},
          {:codex_reasoning, :string},
          {:gemini_resume, :string},
          {:codex_resume, :string}
        ]
      )

    with :ok <- check_invalid(invalid),
         {:ok, args} <- build_args(parsed, rest) do
      {:ok, args}
    end
  end

  defp check_invalid([]), do: :ok
  defp check_invalid([{flag, _} | _]), do: {:error, "Unknown flag: #{flag}"}

  defp build_args(parsed, rest) do
    prompt = parsed[:prompt] || List.first(rest)

    if is_nil(prompt) or prompt == "" do
      {:error, "Missing required --prompt argument"}
    else
      timeout = Keyword.get(parsed, :timeout, 900)

      if not is_integer(timeout) or timeout <= 0 do
        {:error, "--timeout must be a positive integer"}
      else
        roles_dir = Keyword.get(parsed, :roles_dir) || default_roles_dir()
        files = parse_files(Keyword.get(parsed, :files, ""))

        {:ok,
         %{
           prompt: prompt,
           role: Keyword.get(parsed, :role, "default"),
           gemini_role: Keyword.get(parsed, :gemini_role),
           codex_role: Keyword.get(parsed, :codex_role),
           files: files,
           gemini_model: Keyword.get(parsed, :gemini_model),
           codex_model: Keyword.get(parsed, :codex_model),
           timeout: timeout,
           roles_dir: roles_dir,
           project_roles_dir: Keyword.get(parsed, :project_roles_dir),
           codex_reasoning: Keyword.get(parsed, :codex_reasoning),
           gemini_resume: Keyword.get(parsed, :gemini_resume),
           codex_resume: Keyword.get(parsed, :codex_resume)
         }}
      end
    end
  end

  defp parse_files(nil), do: []
  defp parse_files(""), do: []

  defp parse_files(files_str) do
    files_str
    |> String.split(",")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(&(&1 == ""))
  end

  defp default_roles_dir do
    case :escript.script_name() do
      [] ->
        Path.expand("roles")

      script ->
        script = to_string(script)
        basename = Path.basename(script)

        if basename in ["mix", "elixir", "iex"] or String.starts_with?(script, "-") or
             not String.contains?(script, "/") do
          Path.expand("roles")
        else
          Path.join(Path.dirname(script), "roles")
        end
    end
  rescue
    _ -> Path.expand("roles")
  end
end
