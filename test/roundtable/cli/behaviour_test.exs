defmodule Roundtable.CLI.BehaviourTest do
  use ExUnit.Case, async: true

  defmodule ValidCLI do
    @behaviour Roundtable.CLI.Behaviour

    @impl true
    def probe_args(), do: ["--version"]

    @impl true
    def build_args(_args, _prompt), do: ["exec", "--json"]

    @impl true
    def parse_output(_stdout, _stderr),
      do: %{response: "", status: :ok, parse_error: nil, metadata: %{}, session_id: nil}
  end

  test "a module implementing all callbacks compiles without warnings" do
    assert function_exported?(ValidCLI, :probe_args, 0)
    assert function_exported?(ValidCLI, :build_args, 2)
    assert function_exported?(ValidCLI, :parse_output, 2)
  end

  test "probe_args/0 returns a list of strings" do
    result = ValidCLI.probe_args()
    assert is_list(result)
    assert Enum.all?(result, &is_binary/1)
  end

  test "build_args/2 returns a list of strings" do
    result = ValidCLI.build_args(%{}, "test prompt")
    assert is_list(result)
  end

  test "parse_output/2 returns map with required keys" do
    result = ValidCLI.parse_output("stdout", "stderr")
    assert Map.has_key?(result, :response)
    assert Map.has_key?(result, :status)
    assert Map.has_key?(result, :parse_error)
    assert Map.has_key?(result, :metadata)
    assert Map.has_key?(result, :session_id)
  end
end
