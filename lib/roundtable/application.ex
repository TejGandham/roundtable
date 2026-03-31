defmodule Roundtable.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children =
      if mcp_enabled?() do
        [
          Hermes.Server.Registry,
          {Roundtable.MCP.Server, transport: :stdio, request_timeout: :timer.minutes(15)}
        ]
      else
        []
      end

    # Low restart intensity: if the stdio transport crashes (e.g. client disconnects),
    # allow at most 1 restart in 5 seconds to avoid a restart storm on EOF.
    opts = [strategy: :one_for_one, name: Roundtable.Supervisor, max_restarts: 1, max_seconds: 5]
    Supervisor.start_link(children, opts)
  end

  defp mcp_enabled? do
    # Only start the MCP server when stdin is a pipe (i.e. an MCP client is connected).
    # When running as escript or `mix test`, stdin is a terminal or closed — skip MCP.
    System.get_env("ROUNDTABLE_MCP") == "1"
  end
end
