defmodule Roundtable.MixProject do
  use Mix.Project

  def project do
    [
      app: :roundtable,
      version: "0.3.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      releases: releases(),
      aliases: aliases()
    ]
  end

  def application do
    [
      mod: {Roundtable.Application, []},
      extra_applications: [:logger]
    ]
  end

  defp releases do
    [
      roundtable_mcp: [
        include_erts: false,
        applications: [roundtable: :permanent]
      ]
    ]
  end

  defp aliases do
    [
      "deps.get": ["deps.get", "deps.patch"]
    ]
  end

  defp deps do
    [
      {:jason, "~> 1.4"},
      {:hermes_mcp, "~> 0.14"}
    ]
  end
end
