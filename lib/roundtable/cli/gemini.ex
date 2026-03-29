defmodule Roundtable.CLI.Gemini do
  @moduledoc "Gemini CLI backend: arg builder and JSON output parser."
  @behaviour Roundtable.CLI.Behaviour

  @impl true
  def probe_args(), do: ["--version"]

  @impl true
  def build_args(args, prompt) do
    base = ["-p", prompt, "-o", "json", "--yolo"]
    model_args = if args[:gemini_model], do: ["-m", args[:gemini_model]], else: []

    if args[:gemini_resume] do
      ["--resume", args[:gemini_resume]] ++ base ++ model_args
    else
      base ++ model_args
    end
  end

  @impl true
  def parse_output(stdout, stderr) do
    case Jason.decode(stdout) do
      {:ok, data} ->
        parse_success(data)

      {:error, _} ->
        # Error recovery: try parsing stderr as JSON error block
        case Jason.decode(stderr) do
          {:ok, %{"error" => err}} ->
            %{
              response: Map.get(err, "message", Jason.encode!(err)),
              status: :error,
              parse_error: nil,
              metadata: %{},
              session_id: nil
            }

          _ ->
            %{
              response: if(stdout != "", do: stdout, else: stderr),
              status: :error,
              parse_error: "JSON parse failed",
              metadata: %{},
              session_id: nil
            }
        end
    end
  end

  defp parse_success(%{"error" => err}) do
    %{
      response: Map.get(err, "message", Jason.encode!(err)),
      status: :error,
      parse_error: nil,
      metadata: %{},
      session_id: nil
    }
  end

  defp parse_success(data) do
    response = if is_binary(data["response"]), do: data["response"], else: ""
    metadata = extract_metadata(data)

    %{
      response: response,
      status: :ok,
      parse_error: nil,
      metadata: metadata,
      session_id: data["session_id"]
    }
  end

  defp extract_metadata(data) do
    case get_in(data, ["stats", "models"]) do
      nil ->
        %{}

      models when map_size(models) == 0 ->
        %{}

      models ->
        model_name = models |> Map.keys() |> List.first()
        tokens = get_in(models, [model_name, "tokens"])
        %{model_used: model_name, tokens: tokens}
    end
  end
end
