defmodule Roundtable.MCP.Server do
  use Hermes.Server,
    name: "roundtable",
    version: Mix.Project.config()[:version],
    capabilities: [:tools]

  component(Roundtable.MCP.Tools.Hivemind)
  component(Roundtable.MCP.Tools.Deepdive)
  component(Roundtable.MCP.Tools.Architect)
  component(Roundtable.MCP.Tools.Challenge)
  component(Roundtable.MCP.Tools.Xray)
end
