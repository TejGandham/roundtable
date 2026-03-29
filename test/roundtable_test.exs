defmodule RoundtableTest do
  use ExUnit.Case, async: false

  defp run_main(argv, extra_env \\ []) do
    pa_args =
      Path.expand("_build/test/lib/*/ebin")
      |> Path.wildcard()
      |> Enum.flat_map(&["-pa", &1])

    eval = "args = Jason.decode!(System.get_env(\"ROUNDTABLE_ARGS\")); Roundtable.main(args)"

    System.cmd("elixir", pa_args ++ ["-e", eval],
      env: [{"ROUNDTABLE_ARGS", Jason.encode!(argv)} | extra_env],
      stderr_to_stdout: true
    )
  end

  defp decode_json_output(output) do
    output
    |> String.trim()
    |> Jason.decode!()
  end

  test "recursion guard exits 1 and returns JSON error before arg parsing" do
    {output, status} = run_main([], [{"ROUNDTABLE_ACTIVE", "1"}])
    payload = decode_json_output(output)

    assert status == 1
    assert payload["error"] =~ "Recursive invocation detected"
    refute Map.has_key?(payload, "usage")
  end

  test "missing prompt returns JSON error with usage" do
    {output, status} = run_main([])
    payload = decode_json_output(output)

    assert status == 1
    assert payload["error"] =~ "Missing required --prompt"
    assert payload["usage"] =~ "roundtable --prompt"
  end

  test "invalid flag returns JSON error with usage" do
    {output, status} = run_main(["--prompt", "hello", "--unknown", "x"])
    payload = decode_json_output(output)

    assert status == 1
    assert payload["error"] =~ "Unknown flag"
    assert payload["usage"] =~ "roundtable --prompt"
  end

  test "valid args execute full pipeline and emit result JSON" do
    {output, status} = run_main(["--prompt", "hello", "--timeout", "5"])
    payload = decode_json_output(output)

    assert status == 0
    assert is_map(payload["gemini"])
    assert is_map(payload["codex"])
    assert is_map(payload["meta"])
    assert payload["meta"]["gemini_role"] == "default"
    assert payload["meta"]["codex_role"] == "default"
  end

  test "missing role file returns JSON error and exits 1" do
    {output, status} = run_main(["--prompt", "hello", "--role", "definitely_missing_role"])
    payload = decode_json_output(output)

    assert status == 1
    assert payload["error"] =~ "Role prompt not found"
  end
end
