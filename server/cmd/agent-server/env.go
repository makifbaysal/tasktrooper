package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/runtime"
)

const defaultShutdownGrace = 9 * time.Minute

const (
	defaultPort    = 8085
	defaultDataDir = "./data"
)

func shutdownGraceFromEnv(getenv func(string) string) time.Duration {
	raw := getenv("SHUTDOWN_GRACE")
	if raw == "" {
		return defaultShutdownGrace
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultShutdownGrace
	}
	return d
}

type localConfig struct {
	Options runtime.Options
	PostgresDSN    string
	PostgresBinDir string
	ShutdownGrace  time.Duration
}

func optionsFromEnv(getenv func(string) string) (localConfig, error) {
	apiKey := strings.TrimSpace(getenv("SERVER_API_KEY"))
	if apiKey == "" {
		return localConfig{}, errors.New("SERVER_API_KEY is required (the bearer token the UI sends)")
	}
	if getenv("MCP_SECRETS_KEY") == "" {
		return localConfig{}, errors.New("MCP_SECRETS_KEY is required (encrypts provider API keys at rest)")
	}

	dataDir := getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	configPath := getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "resources/config.yml"
	}

	port := defaultPort
	if v := getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 0 {
			return localConfig{}, fmt.Errorf("invalid PORT %q", v)
		}
		port = p
	}

	dsn := strings.TrimSpace(getenv("DATABASE_URL"))
	return localConfig{
		Options: runtime.Options{
			ConfigPath:  configPath,
			DataDir:     dataDir,
			Port:        port,
			APIKey:      apiKey,
			PostgresDSN: dsn,
			LazyMCP:           true,
			CORSOrigins:       corsOriginsFromEnv(getenv("CORS_ORIGINS")),
			EmbeddingsBaseURL: strings.TrimSpace(getenv("EMBEDDINGS_BASE_URL")),
			EmbeddingsSource:  strings.TrimSpace(getenv("EMBEDDINGS_SOURCE")),
			AllowedRoots:      allowedRootsFromEnv(getenv("ALLOWED_ROOTS")),
		},
		PostgresDSN:    dsn,
		PostgresBinDir: strings.TrimSpace(getenv("EMBEDDED_POSTGRES_CACHE_DIR")),
		ShutdownGrace:  shutdownGraceFromEnv(getenv),
	}, nil
}

func allowedRootsFromEnv(raw string) []string {
	var out []string
	splitter := func(r rune) bool {
		return r == ';' || r == ','
	}
	for _, part := range strings.FieldsFunc(raw, splitter) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func corsOriginsFromEnv(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return runtime.DefaultCORSOrigins
	}
	return out
}
