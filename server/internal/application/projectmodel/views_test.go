package projectmodel

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ViewsSuite struct {
	suite.Suite

	store    *fakeStore
	repos    *fakeRepos
	projects *fakeProjects
	svc      *Service
}

func TestViewsSuite(t *testing.T) {
	suite.Run(t, new(ViewsSuite))
}

func (s *ViewsSuite) SetupTest() {
	s.store = newFakeStore()
	s.repos = newFakeRepos()
	s.projects = newFakeProjects()
	s.svc = NewService(Deps{
		Store:    s.store,
		Repos:    s.repos,
		Projects: s.projects,
	})
}

func (s *ViewsSuite) newRepo(name string, projectIDs ...uuid.UUID) domain.Repository {
	r := domain.Repository{ID: uuid.New(), Name: name, ProjectIDs: projectIDs, UpdatedAt: s.svc.now()}
	s.repos.set(r)
	return r
}

func (s *ViewsSuite) TestRepositoryModelNeverReturnsNilSlices() {
	repo := s.newRepo("solo")
	model, err := s.svc.RepositoryModel(context.Background(), repo.ID)
	s.Require().NoError(err)
	s.NotNil(model.Components)
	s.NotNil(model.Checks)
	s.NotNil(model.Links)
	s.NotNil(model.IncomingLinks)
	s.NotNil(model.Resources)
	s.NotNil(model.LinkedComponents)
	s.NotNil(model.Review)
	s.Nil(model.LatestScan)
}

func (s *ViewsSuite) TestRepositoryModelShapeSingleVsMonorepo() {
	repo := s.newRepo("solo")
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	model, err := s.svc.RepositoryModel(context.Background(), repo.ID)
	s.Require().NoError(err)
	s.Equal(domain.RepoShapeSingle, model.Shape)

	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "a", Status: domain.ComponentStatusActive})
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "b", Status: domain.ComponentStatusActive})
	model, err = s.svc.RepositoryModel(context.Background(), repo.ID)
	s.Require().NoError(err)
	s.Equal(domain.RepoShapeMonorepo, model.Shape)
}

func (s *ViewsSuite) TestRepositoryModelLinkedComponents() {
	repoA := s.newRepo("repo-a")
	repoB := s.newRepo("repo-b")

	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})

	s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToComponentID: &compB.ID,
		Status: domain.LinkConfirmed, Source: domain.LinkSourceScan,
	})

	model, err := s.svc.RepositoryModel(context.Background(), repoA.ID)
	s.Require().NoError(err)
	s.Require().Len(model.LinkedComponents, 1)
	s.Equal(compB.ID, model.LinkedComponents[0].ID)
	s.Equal("repo-b", model.LinkedComponents[0].RepositoryName)
}

func (s *ViewsSuite) TestProjectsOverviewUnassignedRepos() {
	s.newRepo("orphan")
	overview, err := s.svc.ProjectsOverview(context.Background())
	s.Require().NoError(err)
	s.Require().Len(overview.Unassigned, 1)
	s.Equal("orphan", overview.Unassigned[0].Name)
	s.Empty(overview.Projects)
}

func (s *ViewsSuite) TestProjectsOverviewCrossProjectOnlyWhenLinksExist() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	projB := domain.InitiativeProject{ID: uuid.New(), Name: "proj-b"}
	s.projects = newFakeProjects(projA, projB)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	repoB := s.newRepo("repo-b", projB.ID)

	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})

	overview, err := s.svc.ProjectsOverview(context.Background())
	s.Require().NoError(err)
	byName := map[string]domain.ProjectOverview{}
	for _, p := range overview.Projects {
		byName[p.Name] = p
	}
	s.Empty(byName["proj-a"].CrossProjects, "no links yet, so no cross-project references")
	s.Zero(byName["proj-a"].CrossLinks)

	s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToComponentID: &compB.ID,
		Status: domain.LinkConfirmed, Source: domain.LinkSourceScan,
	})

	overview, err = s.svc.ProjectsOverview(context.Background())
	s.Require().NoError(err)
	byName = map[string]domain.ProjectOverview{}
	for _, p := range overview.Projects {
		byName[p.Name] = p
	}
	s.Require().Len(byName["proj-a"].CrossProjects, 1)
	s.Equal("proj-b", byName["proj-a"].CrossProjects[0].Name)
	s.Equal(1, byName["proj-a"].CrossLinks)
	s.Require().Len(byName["proj-b"].CrossProjects, 1)
	s.Equal("proj-a", byName["proj-b"].CrossProjects[0].Name)
}

func (s *ViewsSuite) TestProjectsOverviewSharedResources() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	s.projects = newFakeProjects(projA)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	repoOutside := s.newRepo("repo-outside")

	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	compOutside := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoOutside.ID, Path: ".", Status: domain.ComponentStatusActive})

	resource := s.store.seedResource(domain.SystemResource{ID: uuid.New(), Kind: domain.ResourceDatabase, Name: "primary-db", IdentityKey: "vendor:postgres"})

	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToResourceID: &resource.ID, Status: domain.LinkConfirmed, Source: domain.LinkSourceScan})

	overview, err := s.svc.ProjectsOverview(context.Background())
	s.Require().NoError(err)
	var projAOverview domain.ProjectOverview
	for _, p := range overview.Projects {
		if p.Name == "proj-a" {
			projAOverview = p
		}
	}
	s.Empty(projAOverview.SharedResources, "only proj-a links this resource so far")

	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repoOutside.ID, FromComponentID: compOutside.ID, ToResourceID: &resource.ID, Status: domain.LinkConfirmed, Source: domain.LinkSourceScan})

	overview, err = s.svc.ProjectsOverview(context.Background())
	s.Require().NoError(err)
	for _, p := range overview.Projects {
		if p.Name == "proj-a" {
			projAOverview = p
		}
	}
	s.Require().Len(projAOverview.SharedResources, 1)
	s.Equal("primary-db", projAOverview.SharedResources[0].Name)
}

func (s *ViewsSuite) TestProjectOverviewType() {
	projSingle := domain.InitiativeProject{ID: uuid.New(), Name: "single"}
	projMono := domain.InitiativeProject{ID: uuid.New(), Name: "mono"}
	projMulti := domain.InitiativeProject{ID: uuid.New(), Name: "multi"}
	s.projects = newFakeProjects(projSingle, projMono, projMulti)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	singleRepo := s.newRepo("single-repo", projSingle.ID)
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: singleRepo.ID, Path: ".", Status: domain.ComponentStatusActive})

	monoRepo := s.newRepo("mono-repo", projMono.ID)
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: monoRepo.ID, Path: "a", Status: domain.ComponentStatusActive})
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: monoRepo.ID, Path: "b", Status: domain.ComponentStatusActive})

	multiRepoOne := s.newRepo("multi-repo-1", projMulti.ID)
	multiRepoTwo := s.newRepo("multi-repo-2", projMulti.ID)
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: multiRepoOne.ID, Path: ".", Status: domain.ComponentStatusActive})
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: multiRepoTwo.ID, Path: ".", Status: domain.ComponentStatusActive})

	overview, err := s.svc.ProjectsOverview(context.Background())
	s.Require().NoError(err)
	byName := map[string]domain.ProjectType{}
	for _, p := range overview.Projects {
		byName[p.Name] = p.Type
	}
	s.Equal(domain.ProjectTypeSingle, byName["single"])
	s.Equal(domain.ProjectTypeMonorepo, byName["mono"])
	s.Equal(domain.ProjectTypeMultiRepo, byName["multi"])
}

func (s *ViewsSuite) TestProjectOverviewDetailReviewAcrossRepositories() {
	proj := domain.InitiativeProject{ID: uuid.New(), Name: "proj"}
	s.projects = newFakeProjects(proj)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repo := s.newRepo("repo", proj.ID)
	mediumRole := domain.ComponentRoleWorker
	s.store.seedComponent(domain.Component{
		ID: uuid.New(), RepositoryID: repo.ID, Path: ".",
		Role:   domain.Fact[domain.ComponentRole]{Detected: &mediumRole, Confidence: domain.ConfidenceMedium},
		Status: domain.ComponentStatusActive,
	})

	detail, err := s.svc.ProjectOverview(context.Background(), proj.ID)
	s.Require().NoError(err)
	s.Require().Len(detail.Review, 1)
	s.Equal(domain.ReviewRole, detail.Review[0].Kind)
	s.Equal(1, detail.ReviewCount)
}
