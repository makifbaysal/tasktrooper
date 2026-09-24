package projectmodel

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *ViewsSuite) TestWorkspaceMapIndependentProjectsHaveNoEdges() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	projB := domain.InitiativeProject{ID: uuid.New(), Name: "proj-b"}
	s.projects = newFakeProjects(projA, projB)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	repoB := s.newRepo("repo-b", projB.ID)
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})

	m, err := s.svc.WorkspaceMap(context.Background())
	s.Require().NoError(err)
	s.Require().Len(m.Projects, 2)
	s.Empty(m.Edges, "independent projects must carry no edges")
	s.Empty(m.SharedResources)
	s.NotNil(m.Unassigned)
}

func (s *ViewsSuite) TestWorkspaceMapCrossProjectLinkProducesOneAggregatedEdge() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	projB := domain.InitiativeProject{ID: uuid.New(), Name: "proj-b"}
	s.projects = newFakeProjects(projA, projB)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	repoB := s.newRepo("repo-b", projB.ID)
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: "services/api", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})

	s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToComponentID: &compB.ID,
		Protocol: domain.LinkHTTP, Status: domain.LinkConfirmed, Source: domain.LinkSourceScan,
	})
	s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToComponentID: &compB.ID,
		Protocol: domain.LinkHTTP, Status: domain.LinkSuggested, Source: domain.LinkSourceScan,
	})

	m, err := s.svc.WorkspaceMap(context.Background())
	s.Require().NoError(err)
	s.Require().Len(m.Edges, 1, "both links between the same project pair aggregate into one edge")
	edge := m.Edges[0]
	s.Equal(projA.ID, edge.FromProjectID)
	s.Equal(projB.ID, edge.ToProjectID)
	s.Equal(2, edge.Links)
	s.Equal(1, edge.Suggested)
	s.Equal([]domain.LinkProtocol{domain.LinkHTTP}, edge.Protocols)
	s.Require().Len(edge.Examples, 1)
	s.Contains(edge.Examples[0], "services/api")
}

func (s *ViewsSuite) TestWorkspaceMapDismissedLinksExcludedFromEdges() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	projB := domain.InitiativeProject{ID: uuid.New(), Name: "proj-b"}
	s.projects = newFakeProjects(projA, projB)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	repoB := s.newRepo("repo-b", projB.ID)
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})

	s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToComponentID: &compB.ID,
		Protocol: domain.LinkHTTP, Status: domain.LinkDismissed, Source: domain.LinkSourceScan,
	})

	m, err := s.svc.WorkspaceMap(context.Background())
	s.Require().NoError(err)
	s.Empty(m.Edges, "a dismissed link must never be counted")
}

func (s *ViewsSuite) TestWorkspaceMapSharedResourceRequiresAtLeastTwoProjects() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	projB := domain.InitiativeProject{ID: uuid.New(), Name: "proj-b"}
	s.projects = newFakeProjects(projA, projB)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	stripe := s.store.seedResource(domain.SystemResource{ID: uuid.New(), Kind: domain.ResourcePayments, Vendor: "stripe", Name: "Stripe", IdentityKey: "stripe:payments"})
	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToResourceID: &stripe.ID, Status: domain.LinkConfirmed})

	m, err := s.svc.WorkspaceMap(context.Background())
	s.Require().NoError(err)
	s.Empty(m.SharedResources, "only one project links Stripe so far")

	repoB := s.newRepo("repo-b", projB.ID)
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})
	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repoB.ID, FromComponentID: compB.ID, ToResourceID: &stripe.ID, Status: domain.LinkConfirmed})

	m, err = s.svc.WorkspaceMap(context.Background())
	s.Require().NoError(err)
	s.Require().Len(m.SharedResources, 1)
	s.Equal("Stripe", m.SharedResources[0].Resource.Name)
	s.ElementsMatch([]uuid.UUID{projA.ID, projB.ID}, m.SharedResources[0].ProjectIDs)
}

func (s *ViewsSuite) TestProjectMapIndependentProjectHasNoForeignNodes() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	s.projects = newFakeProjects(projA)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})

	m, err := s.svc.ProjectMap(context.Background(), projA.ID)
	s.Require().NoError(err)
	s.Require().Len(m.Nodes, 1)
	s.False(m.Nodes[0].Foreign)
	s.Empty(m.Edges)
}

func (s *ViewsSuite) TestProjectMapCrossProjectLinkAddsForeignNodeAndEdge() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	projB := domain.InitiativeProject{ID: uuid.New(), Name: "proj-b"}
	s.projects = newFakeProjects(projA, projB)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	repoB := s.newRepo("repo-b", projB.ID)
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})

	s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToComponentID: &compB.ID,
		Protocol: domain.LinkHTTP, Status: domain.LinkConfirmed, Source: domain.LinkSourceScan,
	})

	m, err := s.svc.ProjectMap(context.Background(), projA.ID)
	s.Require().NoError(err)
	s.Require().Len(m.Nodes, 2)
	s.Require().Len(m.Edges, 1)
	s.True(m.Edges[0].CrossProject)

	byID := map[string]domain.MapNode{}
	for _, n := range m.Nodes {
		byID[n.ID] = n
	}
	s.False(byID["c:"+compA.ID.String()].Foreign)
	s.True(byID["c:"+compB.ID.String()].Foreign)
}

func (s *ViewsSuite) TestProjectMapDismissedLinksExcluded() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	projB := domain.InitiativeProject{ID: uuid.New(), Name: "proj-b"}
	s.projects = newFakeProjects(projA, projB)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	repoB := s.newRepo("repo-b", projB.ID)
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})

	s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToComponentID: &compB.ID,
		Protocol: domain.LinkHTTP, Status: domain.LinkDismissed, Source: domain.LinkSourceScan,
	})

	m, err := s.svc.ProjectMap(context.Background(), projA.ID)
	s.Require().NoError(err)
	s.Require().Len(m.Nodes, 1, "the dismissed link's foreign endpoint must not appear")
	s.Empty(m.Edges)
}

func (s *ViewsSuite) TestProjectMapResourceNodeSharedWith() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	projB := domain.InitiativeProject{ID: uuid.New(), Name: "proj-b"}
	s.projects = newFakeProjects(projA, projB)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	repoB := s.newRepo("repo-b", projB.ID)
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive})

	stripe := s.store.seedResource(domain.SystemResource{ID: uuid.New(), Kind: domain.ResourcePayments, Vendor: "stripe", Name: "Stripe", IdentityKey: "stripe:payments"})
	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repoA.ID, FromComponentID: compA.ID, ToResourceID: &stripe.ID, Status: domain.LinkConfirmed})
	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repoB.ID, FromComponentID: compB.ID, ToResourceID: &stripe.ID, Status: domain.LinkConfirmed})

	m, err := s.svc.ProjectMap(context.Background(), projA.ID)
	s.Require().NoError(err)
	var resourceNode *domain.MapNode
	for i := range m.Nodes {
		if m.Nodes[i].Kind == domain.MapNodeResource {
			resourceNode = &m.Nodes[i]
		}
	}
	s.Require().NotNil(resourceNode)
	s.Equal(domain.TierExternal, resourceNode.Tier)
	s.Require().Len(resourceNode.SharedWith, 1)
	s.Equal(projB.Name, resourceNode.SharedWith[0].Name)
}

func (s *ViewsSuite) TestProjectMapHealthAndProviderFromProductionEnvironment() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	s.projects = newFakeProjects(projA)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})
	envs := newFakeEnvironmentStore()
	s.svc.SetEnvironmentReader(envs)

	repoA := s.newRepo("repo-a", projA.ID)
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	envs.seed(domain.ComponentEnvironment{
		RepositoryID: repoA.ID, ComponentID: compA.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel,
		Health: &domain.EnvironmentHealth{Status: domain.CloudStatusHealthy, ErrorCount24h: 3},
	})
	envs.seed(domain.ComponentEnvironment{
		RepositoryID: repoA.ID, ComponentID: compA.ID, Environment: domain.EnvironmentStaging,
		Status: domain.LinkConfirmed, Provider: domain.CloudVercel,
		Health: &domain.EnvironmentHealth{Status: domain.CloudStatusDegraded, ErrorCount24h: 99},
	})

	m, err := s.svc.ProjectMap(context.Background(), projA.ID)
	s.Require().NoError(err)
	s.Require().Len(m.Nodes, 1)
	s.Equal(domain.CloudVercel, m.Nodes[0].Provider)
	s.Equal(domain.CloudStatusHealthy, m.Nodes[0].Health)
	s.Equal(3, m.Nodes[0].ErrorCount24h)
}

func (s *ViewsSuite) TestProjectMapTierByRoleAndLibrary() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	s.projects = newFakeProjects(projA)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repoA := s.newRepo("repo-a", projA.ID)
	frontend := domain.ComponentRoleFrontend
	library := domain.ComponentRoleLibrary
	s.store.seedComponent(domain.Component{
		ID: uuid.New(), RepositoryID: repoA.ID, Path: "web", Status: domain.ComponentStatusActive,
		Role: domain.Fact[domain.ComponentRole]{Detected: &frontend},
	})
	s.store.seedComponent(domain.Component{
		ID: uuid.New(), RepositoryID: repoA.ID, Path: "lib", Status: domain.ComponentStatusActive,
		Role: domain.Fact[domain.ComponentRole]{Detected: &library},
	})

	m, err := s.svc.ProjectMap(context.Background(), projA.ID)
	s.Require().NoError(err)
	tierByPath := map[string]domain.MapTier{}
	for _, n := range m.Nodes {
		tierByPath[n.Path] = n.Tier
	}
	s.Equal(domain.TierClient, tierByPath["web"])
	s.Equal(domain.TierLibrary, tierByPath["lib"])
}
