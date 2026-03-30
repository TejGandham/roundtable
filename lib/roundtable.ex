defmodule Roundtable do
  alias Roundtable.Dispatcher
  alias Roundtable.Output
  alias Roundtable.Prompt.Assembler
  alias Roundtable.Prompt.Roles
  alias Roundtable.Telemetry

  @spec main([String.t()]) :: no_return()
  def main(args), do: Roundtable.CLI.main(args)

  @spec run(map()) :: {:ok, String.t()} | {:error, String.t()}
  def run(args) do
    start_time = System.monotonic_time(:millisecond)
    gemini_role = args.gemini_role || args.role
    codex_role = args.codex_role || args.role
    claude_role = args.claude_role || args.role

    role_prompts =
      try do
        gp = Roles.load_role_prompt(gemini_role, args.roles_dir, args.project_roles_dir)
        cp = Roles.load_role_prompt(codex_role, args.roles_dir, args.project_roles_dir)
        clp = Roles.load_role_prompt(claude_role, args.roles_dir, args.project_roles_dir)
        {:ok, {gp, cp, clp}}
      rescue
        e in RuntimeError ->
          {:error, Exception.message(e)}
      end

    case role_prompts do
      {:error, msg} ->
        {:error, msg}

      {:ok, {gemini_role_prompt, codex_role_prompt, claude_role_prompt}} ->
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
        {:ok, Output.encode(results)}
    end
  end
end
