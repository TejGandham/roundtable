defmodule Roundtable.MixProject do
  use Mix.Project

  def project do
    [
      app: :roundtable,
      version: "0.2.0",
      elixir: "~> 1.18",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      escript: escript()
    ]
  end

  def application do
    [
      mod: {Roundtable.Application, []},
      extra_applications: [:logger]
    ]
  end

  defp escript do
    [main_module: Roundtable, name: "roundtable-cli"]
  end

  defp deps do
    [
      {:jason, "~> 1.4"},
      {:hermes_mcp, "~> 0.14"}
    ]
  end
end
