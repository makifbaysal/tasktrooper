package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
)

// A repository left over from long ago — no project, its working copy gone,
// tasks and history still attached — must still be removable: the delete is
// rows only, so nothing about the disk or GitHub can block it, and every
// table that points at it either goes with it or lets go of it.
func TestRepositoryDeleteRemovesStaleRepositoryWithHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pg, err := newTestDatabase(ctx)
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, pg.DSN())
	require.NoError(t, err)
	defer pool.Close()
	store := postgres.NewRepositoryStore(postgres.NewDB(pool))

	stale, err := store.Create(ctx, "stale-repo", "", "/nonexistent/tasktrooper/stale-repo", "https://github.com/acme/gone.git", "")
	require.NoError(t, err)
	neighbour, err := store.Create(ctx, "neighbour", "", "/nonexistent/tasktrooper/neighbour", "", "")
	require.NoError(t, err)

	var agentID, taskID, componentID, neighbourComponentID, sessionID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO agents (name) VALUES ('delete-test-agent') RETURNING id`).Scan(&agentID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO project_components (repository_id, path) VALUES ($1, '.') RETURNING id`, stale.ID).Scan(&componentID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO project_components (repository_id, path) VALUES ($1, '.') RETURNING id`, neighbour.ID).Scan(&neighbourComponentID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO board_tasks (repository_id, task_number, title, component_id) VALUES ($1, 1, 'old work', $2) RETURNING id`,
		stale.ID, componentID).Scan(&taskID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO sessions (repository_id, task_id) VALUES ($1, $2) RETURNING id`, stale.ID, taskID).Scan(&sessionID))
	_, err = pool.Exec(ctx, `INSERT INTO agent_score_events (agent_id, task_id, delta, event_type, score_after) VALUES ($1, $2, 1, 'done', 1)`, agentID, taskID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO ops_audit_log (action, repository_id) VALUES ('deploy', $1)`, stale.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO component_links (repository_id, from_component_id, to_component_id) VALUES ($1, $2, $3)`,
		neighbour.ID, neighbourComponentID, componentID)
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, stale.ID))

	_, err = store.Get(ctx, stale.ID)
	require.Error(t, err)
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, pool.QueryRow(ctx, query, args...).Scan(&n))
		return n
	}
	require.Zero(t, count(`SELECT count(*) FROM board_tasks WHERE repository_id = $1`, stale.ID))
	require.Zero(t, count(`SELECT count(*) FROM sessions WHERE id = $1`, sessionID))
	require.Equal(t, 1, count(`SELECT count(*) FROM agent_score_events WHERE agent_id = $1 AND task_id IS NULL`, agentID))
	require.Equal(t, 1, count(`SELECT count(*) FROM ops_audit_log WHERE action = 'deploy' AND repository_id IS NULL`))
	require.Equal(t, 1, count(`SELECT count(*) FROM component_links WHERE repository_id = $1 AND to_component_id IS NULL`, neighbour.ID))

	_, err = store.Get(ctx, neighbour.ID)
	require.NoError(t, err)
	require.Error(t, store.Delete(ctx, stale.ID), "a second delete reports the repository as not found")
}
