package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	_ "go.uber.org/automaxprocs"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/embeddedpg"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/runtime"
)

func main() {
	ignoreBrokenPipe()
	cfg, err := optionsFromEnv(os.Getenv)
	if err != nil {
		log.Fatal().Err(err).Msg("agent-server configuration error")
	}

	runtime.ConfigureLogger(cfg.Options.Debug)

	if abs, absErr := filepath.Abs(cfg.Options.DataDir); absErr == nil {
		cfg.Options.DataDir = abs
	}
	if cfg.PostgresBinDir != "" {
		if abs, absErr := filepath.Abs(cfg.PostgresBinDir); absErr == nil {
			cfg.PostgresBinDir = abs
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopPostgres := func() {}
	if cfg.PostgresDSN == "" {
		dsn, stop, pgErr := embeddedpg.Start(ctx, cfg.Options.DataDir, cfg.PostgresBinDir)
		if pgErr != nil {
			log.Fatal().Err(pgErr).Msg("embedded postgres failed to start")
		}
		stopPostgres = stop
		cfg.Options.PostgresDSN = dsn
	} else {
		log.Info().Msg("DATABASE_URL is set: no copy of the database is taken before migrations; backing it up is the operator's job")
	}

	pool, err := pgxpool.New(ctx, cfg.Options.PostgresDSN)
	if err != nil {
		stopPostgres()
		log.Fatal().Err(err).Msg("db connect failed")
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		stopPostgres()
		log.Fatal().Err(err).Msg("db migration failed")
	}
	pool.Close()

	server, err := runtime.Run(ctx, cfg.Options)
	if err != nil {
		stopPostgres()
		log.Fatal().Err(err).Msg("failed to start agent-server")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	select {
	case <-quit:
	case <-stdinClosed(os.Getenv("SHUTDOWN_ON_STDIN_CLOSE") == "1", os.Stdin):
		log.Info().Msg("stdin closed by the supervisor")
	}

	log.Info().Dur("grace", cfg.ShutdownGrace).Msg("shutting down, draining in-flight work")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("shutdown error")
	}
	shutdownCancel()
	cancel()
	stopPostgres()
}
