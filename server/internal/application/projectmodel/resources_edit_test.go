package projectmodel

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (s *ViewsSuite) TestMergeResourcesRepointsLinksAndDeletesSource() {
	repo := s.newRepo("demo")
	comp := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	source := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "Database", IdentityKey: "repo:x:database:database_url"})
	target := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "PostgreSQL", IdentityKey: "repo:x:postgres:postgres"})
	link := s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: comp.ID, ToResourceID: &source.ID,
		Status: domain.LinkConfirmed, Source: domain.LinkSourceScan,
	})

	merged, err := s.svc.MergeResources(context.Background(), source.ID, domain.MergeResourceRequest{IntoResourceID: target.ID})
	s.Require().NoError(err)
	s.Equal(target.ID, merged.ID)
	s.Equal("PostgreSQL", merged.Name)

	_, err = s.store.GetResource(context.Background(), source.ID)
	s.ErrorIs(err, port.ErrNotFound, "the source resource must be deleted")

	updated, err := s.store.GetLink(context.Background(), link.ID)
	s.Require().NoError(err)
	s.Require().NotNil(updated.ToResourceID)
	s.Equal(target.ID, *updated.ToResourceID)
}

func (s *ViewsSuite) TestMergeResourcesEnsureResourceAfterMergeResolvesToTarget() {
	repo := s.newRepo("demo")
	identityKey := fmt.Sprintf("repo:%s:postgres:database_url", repo.ID)
	source := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Vendor: "postgres", Name: "Database", IdentityKey: identityKey})
	target := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Vendor: "postgres", Name: "PostgreSQL", IdentityKey: "repo:x:postgres:postgres"})

	_, err := s.svc.MergeResources(context.Background(), source.ID, domain.MergeResourceRequest{IntoResourceID: target.ID})
	s.Require().NoError(err)

	link := domain.DetectedLink{
		ComponentPath: ".",
		SignalKey:     "env:DATABASE_URL",
		Target: domain.LinkTarget{
			Kind:         domain.LinkTargetResource,
			ResourceKind: domain.ResourceDatabase,
			Vendor:       "postgres",
			Name:         "Database",
		},
		EnvVars: []string{"DATABASE_URL"},
	}
	resolved, err := s.svc.ensureResources(context.Background(), repo.ID, []domain.DetectedLink{link})
	s.Require().NoError(err)
	s.Equal(target.ID, resolved[".|env:DATABASE_URL"], "a rescan emitting the merged-away identity key must resolve to the merge target")
}

func (s *ViewsSuite) TestMergeResourcesSelfMergeReturnsInvalidInput() {
	res := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "Database", IdentityKey: "repo:x:database:database_url"})

	_, err := s.svc.MergeResources(context.Background(), res.ID, domain.MergeResourceRequest{IntoResourceID: res.ID})
	s.Require().Error(err)
	s.ErrorIs(err, ErrInvalidInput)
}

func (s *ViewsSuite) TestMergeResourcesUnknownSourceOrTargetReturnsNotFound() {
	target := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "PostgreSQL", IdentityKey: "repo:x:postgres:postgres"})

	_, err := s.svc.MergeResources(context.Background(), uuid.New(), domain.MergeResourceRequest{IntoResourceID: target.ID})
	s.Require().Error(err)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *ViewsSuite) TestRenameResourceLocksNameAgainstLaterEnsureResource() {
	res := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Vendor: "postgres", Name: "Database", IdentityKey: "repo:x:postgres:database_url"})

	renamed, err := s.svc.RenameResource(context.Background(), res.ID, domain.ResourcePatch{Name: ptr("Primary DB")})
	s.Require().NoError(err)
	s.Equal("Primary DB", renamed.Name)
	s.True(renamed.NameLocked)

	again, err := s.store.EnsureResource(context.Background(), domain.SystemResource{
		Kind: domain.ResourceDatabase, Vendor: "postgres", Name: "Database (detected)", IdentityKey: res.IdentityKey,
	})
	s.Require().NoError(err)
	s.Equal("Primary DB", again.Name, "a locked name must survive a rescan's upsert")
}

func (s *ViewsSuite) TestRenameResourceRejectsBlankName() {
	res := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "Database", IdentityKey: "repo:x:database:database_url"})

	_, err := s.svc.RenameResource(context.Background(), res.ID, domain.ResourcePatch{Name: ptr("   ")})
	s.Require().Error(err)
	s.ErrorIs(err, ErrInvalidInput)
}

func (s *ViewsSuite) TestSplitResourceMovesOnlyGivenLinksAndConfirmsThem() {
	repo := s.newRepo("demo")
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "api", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "worker", Status: domain.ComponentStatusActive})
	source := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "Database", IdentityKey: "repo:x:database:database_url"})
	moved := s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: compA.ID, ToResourceID: &source.ID,
		Status: domain.LinkSuggested, Source: domain.LinkSourceScan, AutoConfirmed: true, Reason: "matched env var",
	})
	stays := s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: compB.ID, ToResourceID: &source.ID,
		Status: domain.LinkConfirmed, Source: domain.LinkSourceScan,
	})

	newResource, err := s.svc.SplitResource(context.Background(), source.ID, domain.SplitResourceRequest{LinkIDs: []uuid.UUID{moved.ID}})
	s.Require().NoError(err)
	s.NotEqual(source.ID, newResource.ID)
	s.Equal(source.Kind, newResource.Kind)
	s.Equal(source.Name, newResource.Name)

	movedLink, err := s.store.GetLink(context.Background(), moved.ID)
	s.Require().NoError(err)
	s.Require().NotNil(movedLink.ToResourceID)
	s.Equal(newResource.ID, *movedLink.ToResourceID)
	s.Equal(domain.LinkConfirmed, movedLink.Status)
	s.False(movedLink.AutoConfirmed)
	s.Empty(movedLink.Reason)
	s.Nil(movedLink.ToComponentID)

	untouchedLink, err := s.store.GetLink(context.Background(), stays.ID)
	s.Require().NoError(err)
	s.Require().NotNil(untouchedLink.ToResourceID)
	s.Equal(source.ID, *untouchedLink.ToResourceID, "a link not named in the split must stay on the source resource")
}

func (s *ViewsSuite) TestSplitResourceRejectsALinkNotOnThatResource() {
	repo := s.newRepo("demo")
	comp := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	source := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "Database", IdentityKey: "repo:x:database:database_url"})
	other := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceCache, Name: "Redis", IdentityKey: "repo:x:redis:redis_url"})
	elsewhereLink := s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: comp.ID, ToResourceID: &other.ID,
		Status: domain.LinkConfirmed, Source: domain.LinkSourceScan,
	})

	_, err := s.svc.SplitResource(context.Background(), source.ID, domain.SplitResourceRequest{LinkIDs: []uuid.UUID{elsewhereLink.ID}})
	s.Require().Error(err)
	s.ErrorIs(err, ErrInvalidInput)
}

func (s *ViewsSuite) TestSplitResourceRejectsEmptyLinkIDs() {
	source := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "Database", IdentityKey: "repo:x:database:database_url"})

	_, err := s.svc.SplitResource(context.Background(), source.ID, domain.SplitResourceRequest{})
	s.Require().Error(err)
	s.ErrorIs(err, ErrInvalidInput)
}

func (s *ViewsSuite) TestUpdateLinkWithToResourceIDRetargets() {
	repo := s.newRepo("demo")
	comp := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	resource := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceCache, Name: "Redis", IdentityKey: "repo:x:redis:redis_url"})
	link := s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: comp.ID,
		Status: domain.LinkSuggested, Source: domain.LinkSourceScan, AutoConfirmed: true, Reason: "guess",
	})

	updated, err := s.svc.UpdateLink(context.Background(), link.ID, domain.LinkPatch{ToResourceID: &resource.ID})
	s.Require().NoError(err)
	s.Require().NotNil(updated.ToResourceID)
	s.Equal(resource.ID, *updated.ToResourceID)
	s.Nil(updated.ToComponentID)
	s.False(updated.AutoConfirmed)
	s.Empty(updated.Reason)
	s.Equal(domain.LinkConfirmed, updated.Status)
}

func (s *ViewsSuite) TestUpdateLinkRejectsMoreThanOneTargetField() {
	repo := s.newRepo("demo")
	comp := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	other := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "other", Status: domain.ComponentStatusActive})
	resource := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceCache, Name: "Redis", IdentityKey: "repo:x:redis:redis_url"})
	link := s.store.seedLink(domain.ComponentLink{
		ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: comp.ID, Status: domain.LinkSuggested, Source: domain.LinkSourceScan,
	})

	_, err := s.svc.UpdateLink(context.Background(), link.ID, domain.LinkPatch{ToComponentID: &other.ID, ToResourceID: &resource.ID})
	s.Require().Error(err)
	s.ErrorIs(err, ErrInvalidInput)
}

func (s *ViewsSuite) TestListWorkspaceResourcesUsersProjectsAndLinkCount() {
	projA := domain.InitiativeProject{ID: uuid.New(), Name: "proj-a"}
	s.projects = newFakeProjects(projA)
	s.svc = NewService(Deps{Store: s.store, Repos: s.repos, Projects: s.projects})

	repo := s.newRepo("demo", projA.ID)
	compA := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "api", Status: domain.ComponentStatusActive})
	compB := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "worker", Status: domain.ComponentStatusActive})
	resource := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "PostgreSQL", IdentityKey: "repo:x:postgres:postgres"})
	unrelated := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceCache, Name: "Redis", IdentityKey: "repo:x:redis:redis_url"})

	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: compA.ID, ToResourceID: &resource.ID, Status: domain.LinkConfirmed, Source: domain.LinkSourceScan})
	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: compA.ID, ToResourceID: &resource.ID, Status: domain.LinkSuggested, Source: domain.LinkSourceScan})
	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: compB.ID, ToResourceID: &resource.ID, Status: domain.LinkConfirmed, Source: domain.LinkSourceScan})
	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: compB.ID, ToResourceID: &resource.ID, Status: domain.LinkDismissed, Source: domain.LinkSourceScan})
	s.store.seedLink(domain.ComponentLink{ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: compA.ID, ToResourceID: &unrelated.ID, Status: domain.LinkDismissed, Source: domain.LinkSourceScan})

	resources, err := s.svc.ListWorkspaceResources(context.Background())
	s.Require().NoError(err)
	s.Require().Len(resources, 1, "a resource with only dismissed links must not appear")

	got := resources[0]
	s.Equal(resource.ID, got.Resource.ID)
	s.Equal(3, got.LinkCount, "link_count counts every non-dismissed link, not deduped by component")
	s.Require().Len(got.Users, 2, "users are deduped by component")
	s.Equal("api", got.Users[0].ComponentPath)
	s.Equal("worker", got.Users[1].ComponentPath)
	s.Require().Len(got.Projects, 1)
	s.Equal(projA.ID, got.Projects[0].ID)
}

func (s *ViewsSuite) TestBriefTalksToDedupesTargetsAfterAMerge() {
	repo := s.newRepo("demo")
	comp := s.store.seedComponent(domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	resource := s.store.seedResource(domain.SystemResource{Kind: domain.ResourceDatabase, Name: "PostgreSQL", IdentityKey: "repo:x:postgres:postgres"})
	links := []domain.ComponentLink{
		{ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: comp.ID, ToResourceID: &resource.ID, Status: domain.LinkConfirmed, Protocol: domain.LinkSQL, EnvVars: []string{"DATABASE_URL"}},
		{ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: comp.ID, ToResourceID: &resource.ID, Status: domain.LinkConfirmed, Protocol: domain.LinkSQL, EnvVars: []string{"POSTGRES_URL"}},
		{ID: uuid.New(), RepositoryID: repo.ID, FromComponentID: comp.ID, ToResourceID: &resource.ID, Status: domain.LinkConfirmed, Protocol: domain.LinkOther},
	}

	out := s.svc.briefTalksTo(context.Background(), links, comp.ID)
	s.Equal(1, countLines(out), "several confirmed links to the same resource must render as one line")
	s.Contains(out, "PostgreSQL")
	s.Contains(out, "sql, other")
	s.Contains(out, "DATABASE_URL, POSTGRES_URL")
}

func countLines(s string) int {
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}
