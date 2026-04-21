package httpmcp

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	defaultAddr         = "127.0.0.1:4040"
	defaultMCPPath      = "/mcp"
	defaultProbeTimeout = 2 * time.Second
	defaultVersion      = "0.8.0"
)

type Config struct {
	Addr            string
	MCPPath         string
	RolesDir        string
	ProjectRolesDir string
	ProbeTimeout    time.Duration
	ServerName      string
	ServerVersion   string
	Logger          *slog.Logger
}

func LoadConfig(logger *slog.Logger) Config {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	return Config{
		Addr:            stringEnv("ROUNDTABLE_HTTP_ADDR", defaultAddr),
		MCPPath:         stringEnv("ROUNDTABLE_HTTP_MCP_PATH", defaultMCPPath),
		RolesDir:        os.Getenv("ROUNDTABLE_HTTP_ROLES_DIR"),
		ProjectRolesDir: os.Getenv("ROUNDTABLE_HTTP_PROJECT_ROLES_DIR"),
		ProbeTimeout:    durationEnv("ROUNDTABLE_HTTP_PROBE_TIMEOUT", defaultProbeTimeout),
		ServerName:      stringEnv("ROUNDTABLE_HTTP_SERVER_NAME", "roundtable-http-mcp"),
		ServerVersion:   stringEnv("ROUNDTABLE_HTTP_SERVER_VERSION", defaultVersion),
		Logger:          logger,
	}
}

func stringEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
