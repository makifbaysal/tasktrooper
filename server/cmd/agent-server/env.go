package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/runtime"
)

// defaultShutdownGrace is how long the process keeps working after SIGTERM
// before in-flight agent runs are cancelled. The desktop supervisor gives up
// sooner than this, so the number that governs a quit is there; what this one
// buys is the terminal case (Ctrl-C in `make dev`), where a cancelled run still
// gets to write its terminal status.
const defaultShutdownGrace = 9 * time.Minute

const (
	defaultPort    = 8085
	defaultDataDir = "./data"
)

// shutdownGraceFromEnv reads SHUTDOWN_GRACE (a Go duration, e.g. "5m").
// Unset or unparseable falls back to the default rather than failing boot — a
// typo in an env var must not keep the server from starting.
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

// localConfig is everything this process reads out of the environment. The
// fields beside Options are the ones consumed before the runtime exists: a
// database has to be running before migrations, and the embedded cluster is
// what makes that true when DATABASE_URL is empty.
type localConfig struct {
	Options runtime.Options
	// PostgresDSN empty means: start the embedded cluster under DataDir.
	PostgresDSN    string
	PostgresBinDir string
	ShutdownGrace  time.Duration
}

// optionsFromEnv reads the server's configuration out of the environment. It is
// the last place DATABASE_URL, SERVER_API_KEY and MCP_SECRETS_KEY are needed:
// everything downstream takes them from the returned config, and runtime.Run
// drops them from the process environment once it has them (see
// internal/platform/runtime/envscrub.go) so an agent's child process cannot
// reach them.
//
// getenv is a parameter rather than os.Getenv so tests can supply an
// environment; keep it side-effect free for that reason.
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

	// 0 is a real value here and not "unset": it asks the kernel for a free
	// port, which is how the desktop starts a server without colliding with
	// whatever else the user is running. The port it got goes to stdout.
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
			// Connect MCP servers after the listener is up: a slow or hanging
			// stdio server would otherwise hold the desktop on its splash
			// screen for as long as it takes to time out.
			LazyMCP:           true,
			CORSOrigins:       corsOriginsFromEnv(getenv("CORS_ORIGINS")),
			EmbeddingsBaseURL: strings.TrimSpace(getenv("EMBEDDINGS_BASE_URL")),
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
