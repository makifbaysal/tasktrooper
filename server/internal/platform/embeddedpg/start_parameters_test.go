package embeddedpg

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	embedded "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestStartParametersIdleTheBackgroundWorkersAndKeepDurability(t *testing.T) {
	params := startParameters()

	assert.Equal(t, map[string]string{
		"autovacuum_naptime":    "10min",
		"checkpoint_timeout":    "30min",
		"wal_writer_delay":      "1s",
		"bgwriter_delay":        "10s",
		"bgwriter_lru_maxpages": "0",
		"log_checkpoints":       "off",
		"jit":                   "off",
	}, params)
	for _, durable := range []string{"fsync", "synchronous_commit", "full_page_writes", "max_connections"} {
		assert.NotContains(t, params, durable)
	}
}

// StartParametersSuite runs a real cluster on the shared 16.x archive the other
// embedded-postgres suites already use (production pins 17, which a test would
// have to download): the parameter names and units are the same in both.
type StartParametersSuite struct {
	suite.Suite
	dir string
}

func TestStartParametersSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(StartParametersSuite))
}

func (s *StartParametersSuite) SetupTest() {
	s.dir = s.T().TempDir()
}

func (s *StartParametersSuite) config(port uint32) embedded.Config {
	return clusterConfig(port, filepath.Join(s.dir, "postgres"), s.dir).
		Version(embedded.V16).
		CachePath("").
		BinariesPath(filepath.Join(s.dir, "bin")).
		RuntimePath(filepath.Join(s.dir, "runtime"))
}

func (s *StartParametersSuite) start(cfg embedded.Config) *embedded.EmbeddedPostgres {
	pg := embedded.NewDatabase(cfg)
	s.Require().NoError(pg.Start())
	return pg
}

func (s *StartParametersSuite) setting(port uint32, name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn(port))
	s.Require().NoError(err)
	defer conn.Close(ctx)
	var value string
	s.Require().NoError(conn.QueryRow(ctx, "SELECT current_setting($1)", name).Scan(&value))
	return value
}

func (s *StartParametersSuite) TestAClusterInitialisedOnStockSettingsPicksThemUpOnItsNextStart() {
	port, err := freePort()
	s.Require().NoError(err)
	stock := s.start(s.config(port).StartParameters(nil))
	s.Equal("1min", s.setting(port, "autovacuum_naptime"))
	s.Require().NoError(stock.Stop())

	port, err = freePort()
	s.Require().NoError(err)
	tuned := s.start(s.config(port))
	defer func() { require.NoError(s.T(), tuned.Stop()) }()

	for name, want := range startParameters() {
		s.Equal(want, s.setting(port, name), name)
	}
	s.Equal("on", s.setting(port, "fsync"))
	s.Equal("on", s.setting(port, "synchronous_commit"))
	s.Equal("100", s.setting(port, "max_connections"))
}
