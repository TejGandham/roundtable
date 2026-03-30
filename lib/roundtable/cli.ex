defmodule Roundtable.CLI do
  @moduledoc "Escript entrypoint for roundtable-cli."

  alias Roundtable.Args

  @spec main([String.t()]) :: no_return()
  def main(args) do
    System.put_env("ERL_CRASH_DUMP_SECONDS", "0")

    if System.get_env("ROUNDTABLE_ACTIVE") do
      IO.puts(
        Jason.encode!(%{
          "error" =>
            "Recursive invocation detected. Roundtable is already running in a parent process."
        })
      )

      System.halt(1)
    end

    case Args.parse(args) do
      {:error, msg} ->
        IO.puts(
          Jason.encode!(%{
            "error" => msg,
            "usage" =>
              ~s(roundtable --prompt "..." [--role default|planner|codereviewer] [--files a.ts,b.ts])
          })
        )

        System.halt(1)

      {:ok, parsed} ->
        case Roundtable.run(parsed) do
          {:ok, json} ->
            IO.puts(json)
            System.halt(0)

          {:error, msg} ->
            IO.puts(Jason.encode!(%{"error" => msg}))
            System.halt(1)
        end
    end
  end
end
