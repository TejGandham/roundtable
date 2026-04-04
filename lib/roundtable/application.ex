defmodule Roundtable.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children =
      if mcp_enabled?() do
        [
          Hermes.Server.Registry,
          {Roundtable.MCP.Server, transport: :stdio, request_timeout: :timer.minutes(16)},
          Roundtable.MCP.TransportWatchdog
        ]
      else
        []
      end

    # Allow a few restarts before giving up. The inner Hermes supervisor uses
    # :one_for_all with its own max_restarts:3/5s limit — a single transient
    # failure exhausts the inner limit, so the outer supervisor needs headroom
    # to restart the whole subtree when that happens.
    opts = [strategy: :one_for_one, name: Roundtable.Supervisor, max_restarts: 3, max_seconds: 30]
    Supervisor.start_link(children, opts)
  end

  @impl true
  def stop(_state) do
    if mcp_enabled?() do
      # When the supervisor exceeds max_restarts, it exits with :shutdown —
      # a "normal" exit that doesn't trigger :permanent app halt. Combined
      # with --no-halt in the release, this leaves a stale BEAM process.
      # Force halt so Claude Code can restart a fresh MCP server.
      System.halt(1)
    end
  end

  defp mcp_enabled? do
    # Only start the MCP server when ROUNDTABLE_MCP=1 is set.
    # Escript and `mix test` runs omit this, avoiding stdio conflicts.
    System.get_env("ROUNDTABLE_MCP") == "1"
  end
end
