package database_test

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

const lastMigrationBeforeColumnInstructionProvenance = "164_task_document_annotations"

// catalogShippedTodoInstruction is catalog/agents/backend-developer/columns/todo.md's
// body verbatim (no front matter, so parsing is just a trim) — one of the
// texts migration 165's backfill recognizes by its sha256, standing in for
// "the catalog shipped this text at some point in history".
const catalogShippedTodoInstruction = "This task is brand new and sits in `todo` — the queue, not the workbench, and nobody expects work done while it is here. Claim it and move it to `in_progress` as the opening action of your first work step (never a step of its own); if the automatic move already put the task in `in_progress` by the time your run starts, skip the move and proceed straight into the work order your main prompt describes. In either case, test in the run you are in: dispatching here is the signal to start work, not to wait for a further hand-off."

// ColumnInstructionProvenanceMigrationSuite covers migration 165: the
// catalog_sha backfill on agent_column_instructions.
type ColumnInstructionProvenanceMigrationSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	seq    atomic.Uint64
}

func TestColumnInstructionProvenanceMigrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ColumnInstructionProvenanceMigrationSuite))
}

func (s *ColumnInstructionProvenanceMigrationSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	boot, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.boot = boot
}

func (s *ColumnInstructionProvenanceMigrationSuite) TearDownSuite() {
	if s.boot != nil {
		s.boot.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ColumnInstructionProvenanceMigrationSuite) freshDatabase() *pgxpool.Pool {
	name := fmt.Sprintf("column_instruction_provenance_%d", s.seq.Add(1))
	_, err := s.boot.Exec(s.ctx, `CREATE DATABASE `+name)
	s.Require().NoError(err)
	s.T().Cleanup(func() {
		_, _ = s.boot.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	})
	u, err := url.Parse(s.pg.DSN())
	s.Require().NoError(err)
	u.Path = "/" + name
	pool, err := pgxpool.New(s.ctx, u.String())
	s.Require().NoError(err)
	s.T().Cleanup(pool.Close)
	return pool
}

func (s *ColumnInstructionProvenanceMigrationSuite) seedRow(pool *pgxpool.Pool, agentName, columnSlug, instruction string) {
	var agentID string
	s.Require().NoError(pool.QueryRow(s.ctx,
		`INSERT INTO agents (name) VALUES ($1) RETURNING id`, agentName).Scan(&agentID))
	_, err := pool.Exec(s.ctx,
		`INSERT INTO agent_column_instructions (agent_id, column_slug, instruction) VALUES ($1, $2, $3)`,
		agentID, columnSlug, instruction)
	s.Require().NoError(err)
}

func (s *ColumnInstructionProvenanceMigrationSuite) catalogSHA(pool *pgxpool.Pool, agentName, columnSlug string) string {
	var sha string
	s.Require().NoError(pool.QueryRow(s.ctx, `
		SELECT c.catalog_sha FROM agent_column_instructions c
		JOIN agents a ON a.id = c.agent_id
		WHERE a.name = $1 AND c.column_slug = $2`, agentName, columnSlug).Scan(&sha))
	return sha
}

func (s *ColumnInstructionProvenanceMigrationSuite) TestBackfillRecognizesACatalogShippedTextAndLeavesAnOperatorEditUnowned() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeColumnInstructionProvenance))

	s.seedRow(pool, "catalog-owned-agent", "todo", catalogShippedTodoInstruction)
	s.seedRow(pool, "operator-owned-agent", "todo", "an operator typed this and it matches nothing upstream ever shipped")

	s.Require().NoError(database.RunMigrations(s.ctx, pool))
	s.Require().NoError(database.RunMigrations(s.ctx, pool), "re-running is a no-op")

	s.NotEmpty(s.catalogSHA(pool, "catalog-owned-agent", "todo"),
		"a row whose text matches a version the catalog shipped is recognized as catalog-owned")
	s.Empty(s.catalogSHA(pool, "operator-owned-agent", "todo"),
		"a row whose text never matched anything upstream shipped stays operator-owned/unknown")
}
