defmodule Roundtable.CLI.Codex do
  @moduledoc "Codex CLI backend: arg builder and JSONL output parser."
  @behaviour Roundtable.CLI.Behaviour

  @impl true
  def probe_args, do: ["--version"]

  @impl true
  def build_args(args, prompt) do
    base = ["exec", "--json", "--dangerously-bypass-approvals-and-sandbox"]

    model_args =
      if args[:codex_model], do: ["-c", "model=#{args[:codex_model]}"], else: []

    reasoning_args =
      if args[:codex_reasoning],
        do: ["-c", "reasoning_effort=#{args[:codex_reasoning]}"],
        else: []

    resume_or_prompt =
      case args[:codex_resume] do
        nil -> [prompt]
        "last" -> ["resume", "--last", prompt]
        session_id -> ["resume", session_id, prompt]
      end

    base ++ model_args ++ reasoning_args ++ resume_or_prompt
  end

  @impl true
  def parse_output(stdout, _stderr) do
    lines =
      stdout
      |> String.split("\n")
      |> Enum.map(&String.trim/1)
      |> Enum.filter(&(byte_size(&1) > 0))

    {messages, errors, usage, thread_id} =
      Enum.reduce(lines, {[], [], nil, nil}, fn line, {msgs, errs, usage, tid} ->
        if String.starts_with?(line, "{") do
          case Jason.decode(line) do
            {:ok, event} -> handle_event(event, msgs, errs, usage, tid)
            {:error, _} -> {msgs, errs, usage, tid}
          end
        else
          {msgs, errs, usage, tid}
        end
      end)

    cond do
      length(messages) > 0 ->
        %{
          response: messages |> Enum.reverse() |> Enum.join("\n\n"),
          status: :ok,
          parse_error: nil,
          metadata: %{usage: usage},
          session_id: thread_id
        }

      length(errors) > 0 ->
        %{
          response: errors |> Enum.reverse() |> Enum.join("\n"),
          status: :error,
          parse_error: nil,
          metadata: %{usage: usage},
          session_id: thread_id
        }

      String.trim(stdout) != "" ->
        %{
          response: String.trim(stdout),
          status: :error,
          parse_error: "No JSONL events found; using raw output",
          metadata: %{},
          session_id: nil
        }

      true ->
        %{
          response: "",
          status: :error,
          parse_error: "No output from codex",
          metadata: %{},
          session_id: nil
        }
    end
  end

  defp handle_event(
         %{
           "type" => "item.completed",
           "item" => %{"type" => "agent_message", "text" => text}
         },
         msgs,
         errs,
         usage,
         tid
       )
       when is_binary(text) do
    trimmed = String.trim(text)
    if trimmed != "", do: {[trimmed | msgs], errs, usage, tid}, else: {msgs, errs, usage, tid}
  end

  defp handle_event(
         %{"type" => "thread.started", "thread_id" => thread_id},
         msgs,
         errs,
         usage,
         _tid
       ) do
    {msgs, errs, usage, thread_id}
  end

  defp handle_event(
         %{"type" => "turn.completed", "usage" => new_usage},
         msgs,
         errs,
         _usage,
         tid
       ) do
    {msgs, errs, new_usage, tid}
  end

  defp handle_event(%{"type" => "error", "message" => message}, msgs, errs, usage, tid) do
    {msgs, [message | errs], usage, tid}
  end

  defp handle_event(_event, msgs, errs, usage, tid) do
    {msgs, errs, usage, tid}
  end
end
