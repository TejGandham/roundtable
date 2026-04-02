defmodule Roundtable.MCP.Server do
  use Hermes.Server,
    name: "roundtable",
    version: "0.2.1",
    capabilities: [:tools]

  component(Roundtable.MCP.Tools.Hivemind)
  component(Roundtable.MCP.Tools.Deepdive)
  component(Roundtable.MCP.Tools.Architect)
  component(Roundtable.MCP.Tools.Challenge)
  component(Roundtable.MCP.Tools.Xray)
end
