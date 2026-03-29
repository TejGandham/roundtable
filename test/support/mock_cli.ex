defmodule Roundtable.MockCLI do
  @moduledoc "Mock CLI backend for tests with canned parse output."
  @behaviour Roundtable.CLI.Behaviour

  @impl true
  def probe_args, do: ["--version"]

  @impl true
  def build_args(_args, prompt), do: [prompt]

  @impl true
  def parse_output(stdout, _stderr) do
    %{response: stdout, status: :ok, parse_error: nil, metadata: %{}, session_id: nil}
  end
end

defmodule Roundtable.MockErrorCLI do
  @moduledoc false
  @behaviour Roundtable.CLI.Behaviour

  @impl true
  def probe_args, do: ["--version"]

  @impl true
  def build_args(_args, prompt), do: [prompt]

  @impl true
  def parse_output(_stdout, _stderr) do
    %{
      response: "error response",
      status: :error,
      parse_error: nil,
      metadata: %{},
      session_id: nil
    }
  end
end
