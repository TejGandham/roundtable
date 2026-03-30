defmodule Roundtable.CLITest do
  use ExUnit.Case, async: false

  @moduletag timeout: 60_000

  @bin_dir Path.expand("../support/bin", __DIR__)

  defp run_cli_main(argv, extra_env \\ []) do
    pa_args =
      Path.expand("_build/test/lib/*/ebin")
      |> Path.wildcard()
      |> Enum.flat_map(&["-pa", &1])

    current_path = System.get_env("PATH", "")
    eval = ~s|args = Jason.decode!(System.get_env("ROUNDTABLE_ARGS")); Roundtable.CLI.main(args)|

    System.cmd("elixir", pa_args ++ ["-e", eval],
      env: [
        {"ROUNDTABLE_ARGS", Jason.encode!(argv)},
        {"PATH", "#{@bin_dir}:#{current_path}"}
        | extra_env
      ],
      stderr_to_stdout: true
    )
  end

  defp decode(output), do: output |> String.trim() |> Jason.decode!()

  describe "Roundtable.CLI.main/1 error paths" do
    test "missing prompt returns JSON error with usage and exits 1" do
      {output, status} = run_cli_main([])
      payload = decode(output)

      assert status == 1
      assert payload["error"] =~ "Missing required --prompt"
      assert payload["usage"] =~ "roundtable --prompt"
    end

    test "unknown flag returns JSON error with usage and exits 1" do
      {output, status} = run_cli_main(["--prompt", "hello", "--bogus", "x"])
      payload = decode(output)

      assert status == 1
      assert payload["error"] =~ "Unknown flag"
      assert payload["usage"] =~ "roundtable --prompt"
    end

    test "recursion guard exits 1 before arg parsing" do
      {output, status} = run_cli_main([], [{"ROUNDTABLE_ACTIVE", "1"}])
      payload = decode(output)

      assert status == 1
      assert payload["error"] =~ "Recursive invocation detected"
      refute Map.has_key?(payload, "usage")
    end
  end

  describe "Roundtable.CLI.main/1 success path" do
    test "valid args return JSON with gemini/codex/claude/meta and exit 0" do
      {output, status} = run_cli_main(["--prompt", "hello", "--timeout", "30"])
      payload = decode(output)

      assert status == 0
      assert is_map(payload["gemini"])
      assert is_map(payload["codex"])
      assert is_map(payload["claude"])
      assert is_map(payload["meta"])
    end
  end

  describe "Roundtable.main/1 delegation" do
    test "delegates to CLI.main/1 producing identical error shape" do
      pa_args =
        Path.expand("_build/test/lib/*/ebin")
        |> Path.wildcard()
        |> Enum.flat_map(&["-pa", &1])

      eval = ~s|args = Jason.decode!(System.get_env("ROUNDTABLE_ARGS")); Roundtable.main(args)|

      {output, status} =
        System.cmd("elixir", pa_args ++ ["-e", eval],
          env: [{"ROUNDTABLE_ARGS", Jason.encode!([])}],
          stderr_to_stdout: true
        )

      payload = decode(output)

      assert status == 1
      assert payload["error"] =~ "Missing required --prompt"
    end
  end

  describe "escript config" do
    test "main_module is Roundtable.CLI" do
      config = Roundtable.MixProject.project()
      assert config[:escript][:main_module] == Roundtable.CLI
    end

    test "escript binary name is roundtable-cli" do
      config = Roundtable.MixProject.project()
      assert config[:escript][:name] == "roundtable-cli"
    end
  end
end
