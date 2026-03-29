defmodule Roundtable.CLI.Behaviour do
  @moduledoc """
  Behaviour defining the contract for CLI backend modules (Gemini, Codex, etc.).
  Each CLI implementation must implement these 3 callbacks.
  """

  @type parse_result :: %{
          response: String.t(),
          status: :ok | :error,
          parse_error: String.t() | nil,
          metadata: map(),
          session_id: String.t() | nil
        }

  @doc "Returns the CLI args used for health probing (e.g., [\"--version\"])."
  @callback probe_args() :: [String.t()]

  @doc "Builds the full CLI argument list from parsed args and assembled prompt."
  @callback build_args(args :: map(), prompt :: String.t()) :: [String.t()]

  @doc "Parses raw CLI stdout and stderr into a structured parse_result map."
  @callback parse_output(stdout :: String.t(), stderr :: String.t()) :: parse_result()
end
