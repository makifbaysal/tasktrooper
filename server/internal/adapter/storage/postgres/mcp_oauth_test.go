package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/migrations"
)

// MCPAccessOAuthSuite covers migration 179: the per-server access mode and
// the mcp_server_oauth store.
type MCPAccessOAuthSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.MCPStore
}

func TestMCPAccessOAuthSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(MCPAccessOAuthSuite))
}

func (s *MCPAccessOAuthSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	s.pool, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.store = postgres.NewMCPStore(postgres.NewDB(s.pool))
}

func (s *MCPAccessOAuthSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *MCPAccessOAuthSuite) SetupTest() {
	_, err := s.pool.Exec(s.ctx, `DELETE FROM mcp_servers`)
	s.Require().NoError(err)
}

func (s *MCPAccessOAuthSuite) TestAccessRoundTripsAndAnUnsetModeIsListed() {
	created, err := s.store.Create(s.ctx, domain.MCPServer{ID: "browser_tools", Transport: "stdio", Command: "npx", Access: domain.MCPAccessAll})
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessAll, created.Access)

	created, err = s.store.Create(s.ctx, domain.MCPServer{ID: "figma", Transport: "http", URL: "https://mcp.figma.example/mcp"})
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessListed, created.Access)

	updated, err := s.store.Update(s.ctx, domain.MCPServer{ID: "figma", Transport: "http", URL: "https://mcp.figma.example/mcp", Access: domain.MCPAccessAll})
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessAll, updated.Access)

	got, err := s.store.Get(s.ctx, "browser_tools")
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessAll, got.Access)

	listed, err := s.store.List(s.ctx)
	s.Require().NoError(err)
	s.Len(listed, 2)
}

func (s *MCPAccessOAuthSuite) TestMigrationKeepsExistingServersOnAllAndDefaultsNewOnesToListed() {
	_, err := s.pool.Exec(s.ctx, `
		ALTER TABLE mcp_servers DROP CONSTRAINT mcp_servers_access_check;
		ALTER TABLE mcp_servers DROP COLUMN access;
		INSERT INTO mcp_servers (id, enabled, transport, command) VALUES ('legacy', true, 'stdio', 'npx');
	`)
	s.Require().NoError(err)

	body, err := migrations.Up.ReadFile("179_mcp_oauth_access.up.sql")
	s.Require().NoError(err)
	for run := 0; run < 2; run++ {
		_, err = s.pool.Exec(s.ctx, string(body))
		s.Require().NoError(err, "run %d", run)
	}

	legacy, err := s.store.Get(s.ctx, "legacy")
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessAll, legacy.Access, "an upgrade changes no agent's tools")

	_, err = s.pool.Exec(s.ctx, `INSERT INTO mcp_servers (id, transport, command) VALUES ('fresh', 'stdio', 'npx')`)
	s.Require().NoError(err)
	fresh, err := s.store.Get(s.ctx, "fresh")
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessListed, fresh.Access)

	_, err = s.pool.Exec(s.ctx, `UPDATE mcp_servers SET access = 'everyone' WHERE id = 'fresh'`)
	s.Error(err, "the check constraint holds the two modes")
}

func (s *MCPAccessOAuthSuite) TestOAuthRecordRoundTripsAndGoesWithItsServer() {
	_, err := s.store.Create(s.ctx, domain.MCPServer{ID: "figma", Transport: "http", URL: "https://mcp.figma.example/mcp"})
	s.Require().NoError(err)

	_, found, err := s.store.GetMCPOAuth(s.ctx, "figma")
	s.Require().NoError(err)
	s.False(found)

	expires := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)
	rec := port.MCPOAuthRecord{
		ServerID: "figma",
		Metadata: domain.MCPOAuthMetadata{
			Resource: "https://mcp.figma.example/mcp", Issuer: "https://auth.figma.example",
			AuthorizationEndpoint: "https://auth.figma.example/authorize", TokenEndpoint: "https://auth.figma.example/token",
			Scopes: []string{"files:read"},
		},
		ClientID:          "dyn-1",
		ClientSecret:      []byte{1, 2, 3},
		ClientAuthMethod:  "none",
		ClientRedirectURI: "http://127.0.0.1:43123/oauth/mcp/callback",
		ClientDynamic:     true,
		AccessToken:       []byte{4, 5, 6},
		RefreshToken:      []byte{7, 8, 9},
		TokenType:         "Bearer",
		Scope:             "files:read",
		ExpiresAt:         &expires,
	}
	s.Require().NoError(s.store.SaveMCPOAuth(s.ctx, rec))

	got, found, err := s.store.GetMCPOAuth(s.ctx, "figma")
	s.Require().NoError(err)
	s.Require().True(found)
	s.Equal(rec.Metadata, got.Metadata)
	s.Equal(rec.ClientSecret, got.ClientSecret)
	s.Equal(rec.AccessToken, got.AccessToken)
	s.Equal(rec.RefreshToken, got.RefreshToken)
	s.Equal("dyn-1", got.ClientID)
	s.True(got.ClientDynamic)
	s.Require().NotNil(got.ExpiresAt)
	s.True(expires.Equal(*got.ExpiresAt))
	s.False(got.Expired)

	rec.Expired = true
	rec.AccessToken = []byte{10}
	rec.RefreshToken = nil
	rec.ExpiresAt = nil
	s.Require().NoError(s.store.SaveMCPOAuth(s.ctx, rec))
	got, _, err = s.store.GetMCPOAuth(s.ctx, "figma")
	s.Require().NoError(err)
	s.True(got.Expired)
	s.Equal([]byte{10}, got.AccessToken)
	s.Empty(got.RefreshToken)
	s.Nil(got.ExpiresAt)

	s.Require().NoError(s.store.Delete(s.ctx, "figma"))
	_, found, err = s.store.GetMCPOAuth(s.ctx, "figma")
	s.Require().NoError(err)
	s.False(found, "deleting the server deletes its sign-in")
}

func (s *MCPAccessOAuthSuite) TestDeleteOAuthLeavesTheServer() {
	_, err := s.store.Create(s.ctx, domain.MCPServer{ID: "linear", Transport: "http", URL: "https://mcp.linear.example/mcp"})
	s.Require().NoError(err)
	s.Require().NoError(s.store.SaveMCPOAuth(s.ctx, port.MCPOAuthRecord{ServerID: "linear", ClientID: "c"}))

	s.Require().NoError(s.store.DeleteMCPOAuth(s.ctx, "linear"))
	_, found, err := s.store.GetMCPOAuth(s.ctx, "linear")
	s.Require().NoError(err)
	s.False(found)
	_, err = s.store.Get(s.ctx, "linear")
	s.NoError(err)
}
