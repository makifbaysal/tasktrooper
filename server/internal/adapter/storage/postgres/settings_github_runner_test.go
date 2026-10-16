package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestGitHubRunnerConfigRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pg, err := newTestDatabase(ctx)
	if err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	defer pg.Stop()
	pool, err := pgxpool.New(ctx, pg.DSN())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	store := postgres.NewSettingsStore(postgres.NewDB(pool), "./data/workspaces", "en", false)

	got, err := store.GitHubRunnerConfig(ctx)
	if err != nil || got.Enabled || got.Repos == nil || len(got.Repos) != 0 {
		t.Fatalf("default = %+v, %v", got, err)
	}
	want := domain.GitHubRunnerConfig{Enabled: true, Repos: []string{"acme/app", "acme/api"}}
	if err := store.SetGitHubRunnerConfig(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err = store.GitHubRunnerConfig(ctx)
	if err != nil || !got.Enabled || len(got.Repos) != 2 || got.Repos[1] != "acme/api" {
		t.Fatalf("after save = %+v, %v", got, err)
	}
	if _, err := store.Get(ctx); err != nil {
		t.Fatalf("app settings no longer read with the runner row present: %v", err)
	}
}
