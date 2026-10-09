package database_test

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

const lastMigrationBeforeIssueSync = "182_assignee_only_queues"

// An install that already had issue import from an earlier schema (one link
// per issue, unique on provider + key) must come out of migration 183 with
// that key dropped and one issue_imports row per imported issue, its links
// untouched.
func TestIssueSyncMigrationConvergesAnEarlierIssueLinksTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dir := t.TempDir()
	pg, err := database.StartEmbedded(ctx, database.EmbeddedConfig{
		DataDir: filepath.Join(dir, "postgres"),
		// Its own runtime path: test binaries extracting into the shared cache
		// race each other.
		RuntimePath: filepath.Join(dir, "runtime"),
	})
	require.NoError(t, err)
	defer func() { _ = pg.Stop() }()
	boot, err := pgxpool.New(ctx, pg.DSN())
	require.NoError(t, err)
	defer boot.Close()
	// StartEmbedded already migrated its own database all the way; this test
	// needs one stopped at 182.
	_, err = boot.Exec(ctx, `CREATE DATABASE issue_sync_migration`)
	require.NoError(t, err)
	u, err := url.Parse(pg.DSN())
	require.NoError(t, err)
	u.Path = "/issue_sync_migration"
	pool, err := pgxpool.New(ctx, u.String())
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, database.RunMigrationsUpTo(ctx, pool, lastMigrationBeforeIssueSync))

	var repoID, taskID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO repositories (name, root_path) VALUES ('widget', '/tmp/widget-issue-sync') RETURNING id`).Scan(&repoID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO board_tasks (repository_id, task_number, title) VALUES ($1, 1, 'Crash on save') RETURNING id`,
		repoID).Scan(&taskID))
	_, err = pool.Exec(ctx, `
		CREATE TABLE issue_links (
		    id UUID PRIMARY KEY,
		    task_id UUID NOT NULL UNIQUE REFERENCES board_tasks(id) ON DELETE CASCADE,
		    task_key TEXT NOT NULL DEFAULT '',
		    imported_by TEXT NOT NULL DEFAULT '',
		    repository_id UUID NOT NULL,
		    provider TEXT NOT NULL CHECK (provider IN ('github', 'jira')),
		    external_key TEXT NOT NULL,
		    url TEXT NOT NULL,
		    title TEXT NOT NULL DEFAULT '',
		    last_column TEXT NOT NULL DEFAULT '',
		    closed_at TIMESTAMPTZ,
		    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		    UNIQUE (provider, external_key)
		)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO issue_links (id, task_id, task_key, imported_by, repository_id, provider, external_key, url, title)
		VALUES ($1, $2, 'T-1', 'user-1', $3, 'github', 'acme/widget#12', 'https://github.com/acme/widget/issues/12', 'Crash on save')`,
		uuid.New(), taskID, repoID)
	require.NoError(t, err)

	require.NoError(t, database.RunMigrations(ctx, pool))

	var provider, key, importedBy, status string
	var intake *uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT provider, external_key, imported_by, intake_task_id, conversion_status FROM issue_imports`).
		Scan(&provider, &key, &importedBy, &intake, &status))
	require.Equal(t, "github", provider)
	require.Equal(t, "acme/widget#12", key)
	require.Equal(t, "user-1", importedBy)
	require.NotNil(t, intake)
	require.Equal(t, taskID, *intake)
	require.Equal(t, "", status, "an import from before the conversion existed is not converted")

	var uniqueOnIssue bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'issue_links_provider_external_key_key')`).Scan(&uniqueOnIssue))
	require.False(t, uniqueOnIssue, "one issue must be able to hold several tasks")
}
