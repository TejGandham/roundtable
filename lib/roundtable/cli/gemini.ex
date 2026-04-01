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
        parse_decoded(data)

      {:error, _} ->
        case Jason.decode(stderr) do
          {:ok, data} ->
            parse_decoded(data)

          _ ->
            parse_raw_error(stdout, stderr)
        end
    end
  end

  defp parse_decoded(%{"error" => err}) do
    message = Map.get(err, "message") || Jason.encode!(err)
    status = classify_error_status(message, err)

    %{
      response: format_error_message(message, status),
      status: status,
      parse_error: nil,
      metadata: %{},
      session_id: nil
    }
  end

  defp parse_decoded(data) do
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

  defp parse_raw_error(stdout, stderr) do
    raw =
      cond do
        String.trim(stdout) != "" -> String.trim(stdout)
        String.trim(stderr) != "" -> String.trim(stderr)
        true -> ""
      end

    status = classify_error_status(raw, %{})

    %{
      response: format_error_message(raw, status),
      status: status,
      parse_error: if(status == :rate_limited, do: nil, else: "JSON parse failed"),
      metadata: %{},
      session_id: nil
    }
  end

  defp classify_error_status(message, err) do
    parts = [message, Map.get(err, "status"), Map.get(err, "code")]

    haystack =
      parts
      |> Enum.reject(&is_nil/1)
      |> Enum.map(&to_string/1)
      |> Enum.join(" ")

    if rate_limited?(haystack), do: :rate_limited, else: :error
  end

  defp rate_limited?(text) when is_binary(text) do
    normalized = String.downcase(text)

    String.contains?(normalized, "429") or
      String.contains?(normalized, "rate limit") or
      String.contains?(normalized, "too many requests") or
      String.contains?(normalized, "resource_exhausted") or
      String.contains?(normalized, "quota")
  end

  defp format_error_message(message, :rate_limited) do
    suffix = if String.trim(message) == "", do: "", else: ": #{String.trim(message)}"
    "Gemini rate limited (429/RESOURCE_EXHAUSTED). Retry later or resume the session#{suffix}"
  end

  defp format_error_message(message, _status), do: message

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
