defmodule Roundtable.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      Hermes.Server.Registry,
      {Roundtable.MCP.Server, transport: :stdio}
    ]

    opts = [strategy: :one_for_one, name: Roundtable.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
