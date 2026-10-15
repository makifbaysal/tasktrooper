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

const lastMigrationBeforeDocumentAnnotations = "163_component_reference_docs"

// DocumentAnnotationsMigrationSuite covers migration 164: the task_documents
// format column, the annotations table, and the annotation tools backfilled to
// the agents that write analyses.
type DocumentAnnotationsMigrationSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	seq    atomic.Uint64
}

func TestDocumentAnnotationsMigrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(DocumentAnnotationsMigrationSuite))
}

func (s *DocumentAnnotationsMigrationSuite) SetupSuite() {
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

func (s *DocumentAnnotationsMigrationSuite) TearDownSuite() {
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

func (s *DocumentAnnotationsMigrationSuite) freshDatabase() *pgxpool.Pool {
	name := fmt.Sprintf("document_annotations_%d", s.seq.Add(1))
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

func (s *DocumentAnnotationsMigrationSuite) seedAgent(pool *pgxpool.Pool, name, policy, role string) {
	var id string
	s.Require().NoError(pool.QueryRow(s.ctx,
		`INSERT INTO agents (name, tool_policy) VALUES ($1, $2::jsonb) RETURNING id`, name, policy).Scan(&id))
	if role == "" {
		return
	}
	_, err := pool.Exec(s.ctx, `INSERT INTO agent_role_assignments (role_id, agent_id)
		SELECT id, $1 FROM roles WHERE key = $2`, id, role)
	s.Require().NoError(err)
}

func (s *DocumentAnnotationsMigrationSuite) allowTools(pool *pgxpool.Pool, name string) []string {
	var tools []string
	s.Require().NoError(pool.QueryRow(s.ctx, `
		SELECT COALESCE(array_agg(t ORDER BY t), '{}') FROM agents,
			jsonb_array_elements_text(COALESCE(tool_policy->'allow_tools', '[]'::jsonb)) AS t
		WHERE name = $1`, name).Scan(&tools))
	return tools
}

func (s *DocumentAnnotationsMigrationSuite) TestAnalystAgentsGainTheAnnotationTools() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeDocumentAnnotations))

	s.seedAgent(pool, "architect-under-test",
		`{"allow_tools":["add_task_document","update_task_document","list_task_documents"]}`, "analyst")
	s.seedAgent(pool, "pm-under-test",
		`{"allow_tools":["add_task_document","update_task_document"]}`, "product_manager")
	s.seedAgent(pool, "reader-analyst",
		`{"allow_tools":["list_task_documents"]}`, "analyst")
	s.seedAgent(pool, "unrestricted", `{}`, "analyst")

	s.Require().NoError(database.RunMigrations(s.ctx, pool))
	s.Require().NoError(database.RunMigrations(s.ctx, pool), "re-running is a no-op")

	architect := s.allowTools(pool, "architect-under-test")
	s.Contains(architect, "list_document_annotations")
	s.Contains(architect, "resolve_document_annotations")
	// migration 169 backfills record_open_questions/list_open_questions onto
	// the same analyst-role, add_task_document-holding agents.
	s.Contains(architect, "record_open_questions")
	s.Contains(architect, "list_open_questions")
	s.Len(architect, 7, "the backfill appends each tool once")

	s.NotContains(s.allowTools(pool, "pm-under-test"), "list_document_annotations",
		"an agent that holds no analyst role is not the one revising analyses")
	s.NotContains(s.allowTools(pool, "pm-under-test"), "record_open_questions",
		"an agent that holds no analyst role does not record open questions either")
	s.NotContains(s.allowTools(pool, "reader-analyst"), "resolve_document_annotations",
		"an agent that cannot rewrite the document gets nothing to answer comments with")
	s.NotContains(s.allowTools(pool, "reader-analyst"), "record_open_questions",
		"an agent that holds no add_task_document gets nothing to ask questions with either")
	s.Empty(s.allowTools(pool, "unrestricted"), "an empty allow list stays unrestricted")
}

func (s *DocumentAnnotationsMigrationSuite) TestExistingDocumentsBecomeMarkdownAndAnnotationsCascade() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeDocumentAnnotations))

	var repoID, taskID, docID string
	s.Require().NoError(pool.QueryRow(s.ctx,
		`INSERT INTO repositories (name, root_path) VALUES ('annotations-migration', '/tmp/annotations-migration') RETURNING id`).Scan(&repoID))
	s.Require().NoError(pool.QueryRow(s.ctx, `
		INSERT INTO board_tasks (repository_id, task_number, title, task_type, board_column, priority, created_by)
		VALUES ($1, 1, 'analysis', 'analiz', 'analiz_review', 'medium', 'test') RETURNING id`, repoID).Scan(&taskID))
	s.Require().NoError(pool.QueryRow(s.ctx,
		`INSERT INTO task_documents (task_id, title, content) VALUES ($1, 'spec', 'body') RETURNING id`, taskID).Scan(&docID))

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	var format string
	s.Require().NoError(pool.QueryRow(s.ctx, `SELECT format FROM task_documents WHERE id = $1`, docID).Scan(&format))
	s.Equal("markdown", format)

	_, err := pool.Exec(s.ctx, `UPDATE task_documents SET format = 'pdf' WHERE id = $1`, docID)
	s.Error(err, "the CHECK allows markdown and html only")

	_, err = pool.Exec(s.ctx, `INSERT INTO task_document_annotations (task_id, document_id, quote, body)
		VALUES ($1, $2, 'a passage', 'a comment')`, taskID, docID)
	s.Require().NoError(err)

	var status string
	s.Require().NoError(pool.QueryRow(s.ctx, `SELECT status FROM task_document_annotations WHERE document_id = $1`, docID).Scan(&status))
	s.Equal("open", status)

	_, err = pool.Exec(s.ctx, `DELETE FROM task_documents WHERE id = $1`, docID)
	s.Require().NoError(err)
	var left int
	s.Require().NoError(pool.QueryRow(s.ctx, `SELECT COUNT(*) FROM task_document_annotations`).Scan(&left))
	s.Zero(left, "deleting the document deletes its annotations")
}
