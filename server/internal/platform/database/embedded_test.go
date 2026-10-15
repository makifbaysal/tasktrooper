package database_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

type EmbeddedSuite struct {
	suite.Suite
}

func (s *EmbeddedSuite) TestStartEmbeddedAndMigrate() {
	if testing.Short() {
		s.T().Skip("skipping embedded postgres integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	defer func() { _ = pg.Stop() }()

	pool, err := pgxpool.New(ctx, pg.DSN())
	s.Require().NoError(err)
	defer pool.Close()

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count)
	s.Require().NoError(err)
	s.GreaterOrEqual(count, 13)

	var sessionsExists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables WHERE table_name = 'sessions'
		)
	`).Scan(&sessionsExists)
	s.Require().NoError(err)
	s.True(sessionsExists)
}

func TestEmbeddedSuite(t *testing.T) {
	suite.Run(t, new(EmbeddedSuite))
}
