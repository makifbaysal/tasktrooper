// Command executor is the headless executor: agent runs and one-shot model
// calls on this computer, with the user's own keys and tools. Its parent
// writes one JSON config line on stdin and keeps stdin open for as long as it
// wants the executor alive; see .ai/executor.md.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	_ "go.uber.org/automaxprocs"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/executor"
)

const shutdownGrace = 10 * time.Second

func main() {
	ignoreBrokenPipe()
	stdin := bufio.NewReader(os.Stdin)
	cfg, err := executor.ReadConfig(stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "executor:", err)
		os.Exit(2)
	}
	executor.ConfigureLogger(cfg.Debug)

	server, err := executor.Start(cfg, os.Stdout, executor.Options{})
	if err != nil {
		log.Fatal().Err(err).Msg("executor failed to start")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	select {
	case <-quit:
	case <-executor.StdinClosed(stdin):
		log.Info().Msg("stdin closed by the parent")
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Warn().Err(err).Msg("executor shutdown did not drain in time")
	}
}
