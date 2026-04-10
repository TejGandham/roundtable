package httpmcp

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultAddr         = "127.0.0.1:4040"
	defaultMCPPath      = "/mcp"
	defaultProbeTimeout = 2 * time.Second
	defaultRequestGrace = 15 * time.Second
	defaultVersion      = "0.6.0"
)

type Config struct {
	Addr            string
	MCPPath         string
	BackendPath     string
	RolesDir        string
	ProjectRolesDir string
	ProbeTimeout    time.Duration
	RequestGrace    time.Duration
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
		BackendPath:     os.Getenv("ROUNDTABLE_HTTP_BACKEND_PATH"),
		RolesDir:        os.Getenv("ROUNDTABLE_HTTP_ROLES_DIR"),
		ProjectRolesDir: os.Getenv("ROUNDTABLE_HTTP_PROJECT_ROLES_DIR"),
		ProbeTimeout:    durationEnv("ROUNDTABLE_HTTP_PROBE_TIMEOUT", defaultProbeTimeout),
		RequestGrace:    durationEnv("ROUNDTABLE_HTTP_REQUEST_GRACE", defaultRequestGrace),
		ServerName:      stringEnv("ROUNDTABLE_HTTP_SERVER_NAME", "roundtable-http-mcp"),
		ServerVersion:   stringEnv("ROUNDTABLE_HTTP_SERVER_VERSION", defaultVersion),
		Logger:          logger,
	}
}

func (c Config) ResolvedBackendPath() (string, error) {
	if candidate := strings.TrimSpace(c.BackendPath); candidate != "" {
		if path, err := resolveCandidate(candidate); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("configured backend path %q is not executable", candidate)
	}

	candidates := []string{
		"./roundtable-cli",
		"release/roundtable",
		"roundtable-cli",
		"roundtable",
	}

	var errs []error
	for _, candidate := range candidates {
		path, err := resolveCandidate(candidate)
		if err == nil {
			return path, nil
		}
		errs = append(errs, err)
	}

	return "", errors.Join(errs...)
}

func resolveCandidate(candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", errors.New("empty backend candidate")
	}

	if strings.ContainsRune(candidate, os.PathSeparator) || strings.HasPrefix(candidate, ".") {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%q is a directory", abs)
		}
		return abs, nil
	}

	path, err := exec.LookPath(candidate)
	if err != nil {
		return "", err
	}
	return path, nil
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
