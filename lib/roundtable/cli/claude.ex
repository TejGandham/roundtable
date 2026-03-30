defmodule Roundtable.CLI.Claude do
  @moduledoc "Claude CLI backend: arg builder and JSON output parser."
  @behaviour Roundtable.CLI.Behaviour

  @impl true
  def probe_args(), do: ["--version"]

  @impl true
  def build_args(args, prompt) do
    base = ["-p", "--output-format", "json", "--dangerously-skip-permissions"]
    model_args = if args[:claude_model], do: ["--model", args[:claude_model]], else: []
    resume_args = if args[:claude_resume], do: ["-r", args[:claude_resume]], else: []
    base ++ model_args ++ resume_args ++ [prompt]
  end

  @impl true
  def parse_output(stdout, _stderr) do
    case Jason.decode(stdout) do
      {:ok, data} ->
        parse_success(data)

      {:error, _} ->
        %{
          response: if(stdout != "", do: stdout, else: ""),
          status: :error,
          parse_error: "JSON parse failed",
          metadata: %{},
          session_id: nil
        }
    end
  end

  defp parse_success(%{"is_error" => true} = data) do
    %{
      response: data["result"] || "",
      status: :error,
      parse_error: nil,
      metadata: %{},
      session_id: data["session_id"]
    }
  end

  defp parse_success(data) do
    %{
      response: data["result"] || "",
      status: :ok,
      parse_error: nil,
      metadata: extract_metadata(data),
      session_id: data["session_id"]
    }
  end

  defp extract_metadata(data) do
    case data["modelUsage"] do
      nil ->
        %{}

      model_usage when map_size(model_usage) == 0 ->
        %{}

      model_usage ->
        raw_name = model_usage |> Map.keys() |> List.first()
        # Strip ANSI escape codes (\e[...m) and residual bracket artifacts ([1m])
        clean_name =
          if raw_name,
            do: Regex.replace(~r/\x1B\[[0-9;]*[mGKHF]|\[[0-9;]*m\]/u, raw_name, ""),
            else: nil

        %{model_used: clean_name || raw_name}
    end
  end
end
