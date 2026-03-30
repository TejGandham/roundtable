defmodule Roundtable do
  alias Roundtable.Args
  alias Roundtable.Dispatcher
  alias Roundtable.Output
  alias Roundtable.Prompt.Assembler
  alias Roundtable.Prompt.Roles
  alias Roundtable.Telemetry

  @spec main([String.t()]) :: no_return()
  def main(args) do
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
        run(parsed)
    end
  end

  defp run(args) do
    start_time = System.monotonic_time(:millisecond)
    gemini_role = args.gemini_role || args.role
    codex_role = args.codex_role || args.role
    claude_role = args.claude_role || args.role

    {gemini_role_prompt, codex_role_prompt, claude_role_prompt} =
      try do
        gp = Roles.load_role_prompt(gemini_role, args.roles_dir, args.project_roles_dir)
        cp = Roles.load_role_prompt(codex_role, args.roles_dir, args.project_roles_dir)
        clp = Roles.load_role_prompt(claude_role, args.roles_dir, args.project_roles_dir)
        {gp, cp, clp}
      rescue
        e in RuntimeError ->
          IO.puts(Jason.encode!(%{"error" => Exception.message(e)}))
          System.halt(1)
      end

    gemini_prompt = Assembler.assemble(gemini_role_prompt, args.prompt, args.files)
    codex_prompt = Assembler.assemble(codex_role_prompt, args.prompt, args.files)
    claude_prompt = Assembler.assemble(claude_role_prompt, args.prompt, args.files)

    timeout_ms = args.timeout * 1_000

    cli_configs = [
      %{
        name: "gemini",
        module: Roundtable.CLI.Gemini,
        model: args.gemini_model,
        role: gemini_role,
        files: args.files,
        args: args,
        prompt: gemini_prompt
      },
      %{
        name: "codex",
        module: Roundtable.CLI.Codex,
        model: args.codex_model,
        role: codex_role,
        files: args.files,
        args: args,
        prompt: codex_prompt
      },
      %{
        name: "claude",
        module: Roundtable.CLI.Claude,
        model: args.claude_model,
        role: claude_role,
        files: args.files,
        args: args,
        prompt: claude_prompt
      }
    ]

    results =
      Dispatcher.dispatch(%{
        cli_configs: cli_configs,
        timeout_ms: timeout_ms
      })

    Telemetry.emit(results, args, start_time)
    IO.puts(Output.encode(results))
    System.halt(0)
  end
end
