package postgres_test

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

type LegacyCloudSourceSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	source *postgres.LegacyCloudSourceStore
	repos  *postgres.RepositoryStore
	cipher *secrets.Cipher
	repoA  uuid.UUID
}

func TestLegacyCloudSourceSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(LegacyCloudSourceSuite))
}

func (s *LegacyCloudSourceSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.source = postgres.NewLegacyCloudSourceStore(db)
	s.repos = postgres.NewRepositoryStore(db)
	cipher, err := secrets.NewCipher(make([]byte, 32))
	s.Require().NoError(err)
	s.cipher = cipher
	s.source.SetCipher(cipher, nil)
}

func (s *LegacyCloudSourceSuite) SetupTest() {
	suffix := uuid.NewString()
	repoA, err := s.repos.Create(s.ctx, "legacy-cloud-"+suffix, "", "/tmp/legacy-cloud-"+suffix, "", "")
	s.Require().NoError(err)
	s.repoA = repoA.ID
}

func (s *LegacyCloudSourceSuite) TearDownTest() {
	_, err := s.pool.Exec(s.ctx, `DELETE FROM app_settings WHERE key IN ('vercel_token','vercel_team_id')`)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `DELETE FROM gcloud_credentials`)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `DELETE FROM repository_vercel_projects`)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `DELETE FROM repository_hosting_links`)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `DELETE FROM repository_gcloud_resources`)
	s.Require().NoError(err)
}

func (s *LegacyCloudSourceSuite) TearDownSuite() {
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

func (s *LegacyCloudSourceSuite) setEncryptedSetting(key, plaintext string) {
	ct, err := s.cipher.Encrypt(plaintext)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, base64.StdEncoding.EncodeToString(ct))
	s.Require().NoError(err)
}

func (s *LegacyCloudSourceSuite) setPlainSetting(key, value string) {
	_, err := s.pool.Exec(s.ctx, `
		INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	s.Require().NoError(err)
}

func (s *LegacyCloudSourceSuite) TestLegacyVercelCredentialAbsent() {
	cred, ok, err := s.source.LegacyVercelCredential(s.ctx)
	s.Require().NoError(err)
	s.False(ok)
	s.Equal(domain.LegacyVercelCredential{}, cred)
}

func (s *LegacyCloudSourceSuite) TestLegacyVercelCredentialDecryptsTokenAndReadsTeam() {
	s.setEncryptedSetting("vercel_token", "tkn-abc123")
	s.setPlainSetting("vercel_team_id", "team_xyz")

	cred, ok, err := s.source.LegacyVercelCredential(s.ctx)
	s.Require().NoError(err)
	s.True(ok)
	s.Equal("tkn-abc123", cred.Token)
	s.Equal("team_xyz", cred.TeamID)
}

func (s *LegacyCloudSourceSuite) TestLegacyGCloudCredentialAbsent() {
	cred, ok, err := s.source.LegacyGCloudCredential(s.ctx)
	s.Require().NoError(err)
	s.False(ok)
	s.Equal(domain.LegacyGCloudCredential{}, cred)
}

func (s *LegacyCloudSourceSuite) TestLegacyGCloudCredentialDecryptsFields() {
	plain := `{"service_account_json":"{\"type\":\"service_account\"}"}`
	ct, err := s.cipher.Encrypt(plain)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO gcloud_credentials (project_id, client_email, data)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET project_id = EXCLUDED.project_id, client_email = EXCLUDED.client_email, data = EXCLUDED.data
	`, "proj-1", "svc@proj-1.iam.gserviceaccount.com", ct)
	s.Require().NoError(err)

	cred, ok, err := s.source.LegacyGCloudCredential(s.ctx)
	s.Require().NoError(err)
	s.True(ok)
	s.Equal("proj-1", cred.ProjectID)
	s.Equal(`{"type":"service_account"}`, cred.Fields["service_account_json"])
}

func (s *LegacyCloudSourceSuite) TestListLegacyVercelProjectLinks() {
	_, err := s.pool.Exec(s.ctx, `
		INSERT INTO repository_vercel_projects (repository_id, sub_project_path, project_id, project_name, production_url)
		VALUES ($1, $2, $3, $4, $5)
	`, s.repoA, "web", "prj_1", "web-app", "https://web.example.com")
	s.Require().NoError(err)

	links, err := s.source.ListLegacyVercelProjectLinks(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(links, 1)
	s.Equal(s.repoA, links[0].RepositoryID)
	s.Equal("web", links[0].SubProjectPath)
	s.Equal("prj_1", links[0].ProjectID)
	s.Equal("web-app", links[0].ProjectName)
	s.Equal("https://web.example.com", links[0].ProductionURL)
}

func (s *LegacyCloudSourceSuite) TestListLegacyVercelHostingLinksFiltersToVercelProvider() {
	_, err := s.pool.Exec(s.ctx, `
		INSERT INTO repository_hosting_links (repository_id, area, provider, external_id, external_name, production_url)
		VALUES ($1, '', 'vercel', 'prj_2', 'root-app', 'https://root.example.com')
	`, s.repoA)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO repository_hosting_links (repository_id, area, provider, external_id, external_name, production_url)
		VALUES ($1, 'backend', 'fly', 'fly-app', 'fly-app', 'https://fly.example.com')
	`, s.repoA)
	s.Require().NoError(err)

	links, err := s.source.ListLegacyVercelHostingLinks(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(links, 1)
	s.Equal("vercel", links[0].Provider)
	s.Equal("prj_2", links[0].ExternalID)
	s.Equal(domain.HostingAreaRoot, links[0].Area)
}

func (s *LegacyCloudSourceSuite) TestListLegacyGCloudResourceBindings() {
	_, err := s.pool.Exec(s.ctx, `
		INSERT INTO repository_gcloud_resources (repository_id, sub_project_path, resource_type, resource_name, display_name, project_id, location, source)
		VALUES ($1, '', 'cloud_run', 'projects/p/locations/us-central1/services/api', 'api', 'p', 'us-central1', 'user')
	`, s.repoA)
	s.Require().NoError(err)

	bindings, err := s.source.ListLegacyGCloudResourceBindings(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(bindings, 1)
	s.Equal(s.repoA, bindings[0].RepositoryID)
	s.Equal(domain.GCloudResourceCloudRun, bindings[0].ResourceType)
	s.Equal("projects/p/locations/us-central1/services/api", bindings[0].ResourceName)
	s.Equal("api", bindings[0].DisplayName)
	s.Equal("us-central1", bindings[0].Location)
}
