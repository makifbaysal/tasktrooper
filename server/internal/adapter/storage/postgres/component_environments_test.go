package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type EnvironmentStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	envs   *postgres.EnvironmentStore
	models *postgres.ProjectModelStore
	repos  *postgres.RepositoryStore
	clouds *postgres.CloudAccountStore
	repoA  uuid.UUID
	repoB  uuid.UUID
}

func TestEnvironmentStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(EnvironmentStoreSuite))
}

func (s *EnvironmentStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.envs = postgres.NewEnvironmentStore(db)
	s.models = postgres.NewProjectModelStore(db)
	s.repos = postgres.NewRepositoryStore(db)
	s.clouds = postgres.NewCloudAccountStore(db)
	cipher, err := secrets.NewCipher(make([]byte, 32))
	s.Require().NoError(err)
	s.clouds.SetCipher(cipher, nil)
}

func (s *EnvironmentStoreSuite) SetupTest() {
	suffix := uuid.NewString()
	repoA, err := s.repos.Create(s.ctx, "env-repo-a-"+suffix, "", "/tmp/env-repo-a-"+suffix, "", "")
	s.Require().NoError(err)
	repoB, err := s.repos.Create(s.ctx, "env-repo-b-"+suffix, "", "/tmp/env-repo-b-"+suffix, "", "")
	s.Require().NoError(err)
	s.repoA = repoA.ID
	s.repoB = repoB.ID
}

func (s *EnvironmentStoreSuite) TearDownSuite() {
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

func (s *EnvironmentStoreSuite) createComponent(repositoryID uuid.UUID, path string) domain.Component {
	c, err := s.models.SaveComponent(s.ctx, domain.Component{RepositoryID: repositoryID, Path: path})
	s.Require().NoError(err)
	return c
}

func (s *EnvironmentStoreSuite) TestSaveEnvironmentAppliesDefaults() {
	comp := s.createComponent(s.repoA, "api")

	saved, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentProduction,
	})
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, saved.ID)
	s.Equal(domain.LinkSuggested, saved.Status)
	s.Equal(domain.LinkSourceScan, saved.Source)
	s.Equal(domain.ConfidenceLow, saved.Confidence)
	s.NotNil(saved.Candidates)
	s.Empty(saved.Candidates)
	s.Nil(saved.Resource)
	s.Nil(saved.Health)
	s.False(saved.CreatedAt.IsZero())
}

func (s *EnvironmentStoreSuite) TestSaveEnvironmentUpsertsByComponentAndEnvironmentKeepingIDAndCreatedAt() {
	comp := s.createComponent(s.repoA, "web")

	first, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentStaging,
		URL:          "https://staging.example.com",
	})
	s.Require().NoError(err)

	second, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		ID:           uuid.New(),
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentStaging,
		URL:          "https://staging2.example.com",
		Status:       domain.LinkConfirmed,
	})
	s.Require().NoError(err)

	s.Equal(first.ID, second.ID)
	s.True(first.CreatedAt.Equal(second.CreatedAt))
	s.Equal("https://staging2.example.com", second.URL)
	s.Equal(domain.LinkConfirmed, second.Status)

	list, err := s.envs.ListEnvironments(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Len(list, 1)
}

func (s *EnvironmentStoreSuite) TestSaveEnvironmentRoundTripsNullableResourceAndHealth() {
	comp := s.createComponent(s.repoA, "worker")

	withoutExtras, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentDevelopment,
	})
	s.Require().NoError(err)
	s.Nil(withoutExtras.Resource)
	s.Nil(withoutExtras.Health)

	checkedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	lastDeploy := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	withExtras, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentDevelopment,
		Resource: &domain.CloudResourceRef{
			Kind:   domain.CloudResourceCloudRunService,
			ID:     "svc-1",
			Name:   "worker",
			Region: "us-central1",
		},
		Health: &domain.EnvironmentHealth{
			Status:        domain.CloudStatusHealthy,
			ErrorCount24h: 2,
			LastDeployAt:  &lastDeploy,
			CheckedAt:     checkedAt,
			Detail:        "ok",
		},
	})
	s.Require().NoError(err)
	s.Require().NotNil(withExtras.Resource)
	s.Equal(domain.CloudResourceCloudRunService, withExtras.Resource.Kind)
	s.Equal("svc-1", withExtras.Resource.ID)
	s.Equal("us-central1", withExtras.Resource.Region)
	s.Require().NotNil(withExtras.Health)
	s.Equal(domain.CloudStatusHealthy, withExtras.Health.Status)
	s.Equal(2, withExtras.Health.ErrorCount24h)
	s.Require().NotNil(withExtras.Health.LastDeployAt)
	s.True(lastDeploy.Equal(*withExtras.Health.LastDeployAt))
	s.True(checkedAt.Equal(withExtras.Health.CheckedAt))
}

func (s *EnvironmentStoreSuite) TestSaveEnvironmentRoundTripsCandidates() {
	comp := s.createComponent(s.repoA, "candidates-app")

	candidates := []domain.CloudResource{
		{Provider: domain.CloudVercel, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1", Name: "one"}},
		{Provider: domain.CloudVercel, Ref: domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_2", Name: "two"}},
	}
	saved, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentPreview,
		Candidates:   candidates,
	})
	s.Require().NoError(err)
	s.Equal(candidates, saved.Candidates)
}

func (s *EnvironmentStoreSuite) TestListEnvironmentsOrdersByComponentPathThenEnvironmentPriority() {
	compZ := s.createComponent(s.repoA, "zzz-app")
	compA := s.createComponent(s.repoA, "aaa-app")

	envKinds := []domain.DeployEnvironment{
		domain.EnvironmentDevelopment,
		domain.EnvironmentProduction,
		domain.DeployEnvironment("canary"),
		domain.EnvironmentPreview,
		domain.EnvironmentStaging,
	}
	for _, comp := range []domain.Component{compZ, compA} {
		for _, env := range envKinds {
			_, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
				RepositoryID: s.repoA,
				ComponentID:  comp.ID,
				Environment:  env,
			})
			s.Require().NoError(err)
		}
	}

	list, err := s.envs.ListEnvironments(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Require().Len(list, 10)

	var got [][2]string
	for _, e := range list {
		got = append(got, [2]string{e.ComponentID.String(), string(e.Environment)})
	}
	want := [][2]string{
		{compA.ID.String(), "production"},
		{compA.ID.String(), "staging"},
		{compA.ID.String(), "preview"},
		{compA.ID.String(), "development"},
		{compA.ID.String(), "canary"},
		{compZ.ID.String(), "production"},
		{compZ.ID.String(), "staging"},
		{compZ.ID.String(), "preview"},
		{compZ.ID.String(), "development"},
		{compZ.ID.String(), "canary"},
	}
	s.Equal(want, got)
}

func (s *EnvironmentStoreSuite) TestListAllEnvironmentsGroupsContiguouslyByRepository() {
	compA := s.createComponent(s.repoA, "svc")
	compB := s.createComponent(s.repoB, "svc")

	_, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{RepositoryID: s.repoA, ComponentID: compA.ID, Environment: domain.EnvironmentStaging})
	s.Require().NoError(err)
	_, err = s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{RepositoryID: s.repoA, ComponentID: compA.ID, Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	_, err = s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{RepositoryID: s.repoB, ComponentID: compB.ID, Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)

	all, err := s.envs.ListAllEnvironments(s.ctx)
	s.Require().NoError(err)

	seenRepos := map[uuid.UUID]bool{}
	var order []uuid.UUID
	for _, e := range all {
		if !seenRepos[e.RepositoryID] {
			seenRepos[e.RepositoryID] = true
			order = append(order, e.RepositoryID)
		} else if order[len(order)-1] != e.RepositoryID {
			s.Fail("repository rows are not contiguous", "repository %s reappeared after %s", e.RepositoryID, order[len(order)-1])
		}
	}

	var repoAEnvs []string
	for _, e := range all {
		if e.RepositoryID == s.repoA {
			repoAEnvs = append(repoAEnvs, string(e.Environment))
		}
	}
	s.Equal([]string{"production", "staging"}, repoAEnvs)
}

func (s *EnvironmentStoreSuite) TestGetEnvironmentNotFound() {
	_, err := s.envs.GetEnvironment(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *EnvironmentStoreSuite) TestDeleteEnvironment() {
	comp := s.createComponent(s.repoA, "deletable")
	saved, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentProduction,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.envs.DeleteEnvironment(s.ctx, saved.ID))

	_, err = s.envs.GetEnvironment(s.ctx, saved.ID)
	s.ErrorIs(err, port.ErrNotFound)

	err = s.envs.DeleteEnvironment(s.ctx, saved.ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *EnvironmentStoreSuite) TestCascadeDeleteOnComponentRemoval() {
	comp := s.createComponent(s.repoA, "cascading")
	saved, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentProduction,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.models.ApplyReconcile(s.ctx, port.ModelReconcile{
		RepositoryID:     s.repoA,
		DeleteComponents: []uuid.UUID{comp.ID},
	}))

	_, err = s.envs.GetEnvironment(s.ctx, saved.ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *EnvironmentStoreSuite) TestDeletingCloudAccountSetsEnvironmentAccountIDNull() {
	comp := s.createComponent(s.repoA, "bound")
	acct, err := s.clouds.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel}, map[string]string{"token": "x"})
	s.Require().NoError(err)

	saved, err := s.envs.SaveEnvironment(s.ctx, domain.ComponentEnvironment{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Environment:  domain.EnvironmentProduction,
		Provider:     domain.CloudVercel,
		AccountID:    &acct.ID,
	})
	s.Require().NoError(err)
	s.Require().NotNil(saved.AccountID)

	s.Require().NoError(s.clouds.DeleteCloudAccount(s.ctx, acct.ID))

	after, err := s.envs.GetEnvironment(s.ctx, saved.ID)
	s.Require().NoError(err)
	s.Nil(after.AccountID)
}
