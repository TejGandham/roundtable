package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/TejGandham/roundtable/internal/httpmcp"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	config := httpmcp.LoadConfig(logger)
	app := httpmcp.NewApp(config, nil)

	if path, err := config.ResolvedBackendPath(); err == nil {
		logger.Info("resolved backend", "path", path)
	} else {
		logger.Warn("backend not ready at startup", "error", err)
	}

	logger.Info("starting roundtable HTTP MCP server", "addr", config.Addr, "mcp_path", config.MCPPath)

	server := &http.Server{
		Addr:              config.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: config.ProbeTimeout,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
