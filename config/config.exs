import Config

# Suppress Hermes MCP internal logging so stdio transport
# doesn't emit noise that corrupts JSON-RPC initialization.
config :hermes_mcp, log: false

# Redirect logger to stderr so it never pollutes the stdio
# MCP transport pipe; keep only warnings+.
config :logger, level: :warning

config :logger, :default_handler, config: %{type: :standard_error}
