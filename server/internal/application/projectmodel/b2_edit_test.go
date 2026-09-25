package projectmodel

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func b2SeedRepo(t *testing.T, repos *b2Repos, name string) domain.Repository {
	t.Helper()
	return repos.put(domain.Repository{ID: uuid.New(), Name: name, RootPath: t.TempDir()})
}

func b2SeedComponent(t *testing.T, store *b2Store, c domain.Component) domain.Component {
	t.Helper()
	saved, err := store.SaveComponent(context.Background(), c)
	require.NoError(t, err)
	return saved
}

func TestB2AddComponentRejectsAPathThatAlreadyHasAnActiveComponent(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo.RootPath, "api"), 0o755))
	b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "api", Status: domain.ComponentStatusActive})

	_, err := svc.AddComponent(ctx, repo.ID, domain.NewComponentRequest{Path: "api", Role: domain.ComponentRoleBackend})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConflict)
}

func TestB2AddComponentReactivatesADismissedComponent(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo.RootPath, "api"), 0o755))
	dismissed := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "api", Status: domain.ComponentStatusDismissed})

	got, err := svc.AddComponent(ctx, repo.ID, domain.NewComponentRequest{Path: "api", Name: "API", Role: domain.ComponentRoleBackend})
	require.NoError(t, err)
	assert.Equal(t, dismissed.ID, got.ID, "reactivation must keep the row's identity")
	assert.Equal(t, domain.ComponentStatusActive, got.Status)
	assert.True(t, got.ManuallyAdded)
	assert.Equal(t, domain.ComponentRoleBackend, got.Role.Get())
	assert.Equal(t, "API", got.Name.Get())
}

func TestB2AddComponentRejectsAMissingDirectory(t *testing.T) {
	svc, _, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")

	_, err := svc.AddComponent(ctx, repo.ID, domain.NewComponentRequest{Path: "does-not-exist", Role: domain.ComponentRoleBackend})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestB2AddComponentRejectsAnInvalidRole(t *testing.T) {
	svc, _, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")

	_, err := svc.AddComponent(ctx, repo.ID, domain.NewComponentRequest{Path: ".", Role: "not-a-role"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestB2AddComponentRunsProjectionAfterwards(t *testing.T) {
	svc, _, repos, pipelines, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")

	_, err := svc.AddComponent(ctx, repo.ID, domain.NewComponentRequest{Path: ".", Role: domain.ComponentRoleBackend})
	require.NoError(t, err)

	updated := repos.get(repo.ID)
	assert.Equal(t, domain.RepoKindBackend, updated.Kind, "adding the root component must project the legacy repo kind")
	_, err = pipelines.ListByRepository(ctx, repo.ID)
	require.NoError(t, err)
}

func TestB2UpdateComponentRoleValidation(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	badRole := domain.ComponentRole("not-a-role")
	_, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Role: domain.Patch[domain.ComponentRole]{Set: true, Value: &badRole}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)

	goodRole := domain.ComponentRoleWorker
	got, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Role: domain.Patch[domain.ComponentRole]{Set: true, Value: &goodRole}})
	require.NoError(t, err)
	assert.Equal(t, domain.ComponentRoleWorker, got.Role.Get())
	assert.True(t, got.Role.Overridden())
}

func TestB2UpdateComponentCommandsMapMergesSetsClearsAndDrops(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	detectedBuild := "go build ./..."
	overrideLint := "golangci-lint run"
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Commands: []domain.ComponentCommand{
			{Purpose: domain.CommandBuild, Command: domain.Fact[string]{Detected: &detectedBuild}},
			{Purpose: domain.CommandLint, Command: domain.Fact[string]{Override: &overrideLint}},
		},
	})

	overrideTest := "go test ./... -run TestFoo"
	got, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{
		Commands: map[domain.CommandPurpose]domain.Patch[string]{
			domain.CommandBuild: {Set: true, Value: nil},
			domain.CommandLint:  {Set: true, Value: nil},
			domain.CommandTest:  {Set: true, Value: &overrideTest},
		},
	})
	require.NoError(t, err)

	var build, lint, testCmd *domain.ComponentCommand
	for i := range got.Commands {
		switch got.Commands[i].Purpose {
		case domain.CommandBuild:
			build = &got.Commands[i]
		case domain.CommandLint:
			lint = &got.Commands[i]
		case domain.CommandTest:
			testCmd = &got.Commands[i]
		}
	}
	require.NotNil(t, build, "clearing an override with a detected value underneath must keep the entry")
	assert.Equal(t, detectedBuild, build.Command.Get())
	assert.False(t, build.Command.Overridden())
	assert.Nil(t, lint, "a purpose with neither detected nor override must be dropped")
	require.NotNil(t, testCmd, "a new purpose with an override must be created")
	assert.Equal(t, overrideTest, testCmd.Command.Get())

	_, err = svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{
		Commands: map[domain.CommandPurpose]domain.Patch[string]{"not-a-purpose": {Set: true, Value: &overrideTest}},
	})
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestB2UpdateComponentReplacesGatesDocsAndStatus(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	threshold := 80.0
	gates := domain.ComponentGates{CoverageThreshold: &threshold}
	docs := domain.RepositoryDocs{CodingStandards: "docs/standards.md"}
	dismissed := domain.ComponentStatusDismissed
	got, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Gates: &gates, Docs: &docs, Status: &dismissed})
	require.NoError(t, err)
	assert.Equal(t, gates, got.Gates)
	assert.Equal(t, docs, got.Docs)
	assert.Equal(t, domain.ComponentStatusDismissed, got.Status)

	badStatus := domain.ComponentStatus("bogus")
	_, err = svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Status: &badStatus})
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestB2UpdateComponentDeliverySetsValidatesAndNormalizes(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	invalid := domain.ComponentDelivery{Mode: "not-a-mode"}
	_, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{
		Delivery: domain.Patch[domain.ComponentDelivery]{Set: true, Value: &invalid},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)

	profile := domain.ComponentDelivery{Mode: domain.DeliveryOnMerge, Executor: domain.ExecutorGitHubActions}
	got, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{
		Delivery: domain.Patch[domain.ComponentDelivery]{Set: true, Value: &profile},
	})
	require.NoError(t, err)
	require.NotNil(t, got.Delivery.Override)
	assert.Equal(t, domain.DefaultSoakMinutes, got.Delivery.Override.Verify.SoakMinutes, "the stored override must be normalized")

	got, err = svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{
		Delivery: domain.Patch[domain.ComponentDelivery]{Set: true, Value: nil},
	})
	require.NoError(t, err)
	assert.Nil(t, got.Delivery.Override, "a null delivery patch clears the override")
}

func TestB2UpdateComponentDeliveryFiresConfirmedHookOnlyWhenAnOverrideIsSet(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	var calls int
	var gotRepoID, gotComponentID uuid.UUID
	svc.SetDeliveryConfirmedHook(func(_ context.Context, repositoryID, componentID uuid.UUID) {
		calls++
		gotRepoID, gotComponentID = repositoryID, componentID
	})

	name := "renamed"
	_, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Name: domain.Patch[string]{Set: true, Value: &name}})
	require.NoError(t, err)
	assert.Zero(t, calls, "an unrelated patch must not fire the hook")

	profile := domain.ComponentDelivery{Mode: domain.DeliveryNone}
	_, err = svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{
		Delivery: domain.Patch[domain.ComponentDelivery]{Set: true, Value: &profile},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "setting an override must fire the hook exactly once")
	assert.Equal(t, repo.ID, gotRepoID)
	assert.Equal(t, comp.ID, gotComponentID)

	_, err = svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{
		Delivery: domain.Patch[domain.ComponentDelivery]{Set: true, Value: nil},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "clearing the override to the detected profile must not fire the hook")
}

func TestB2UpdateComponentReviewedClearsNeedsReview(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive, NeedsReview: true})

	name := "still needs a look"
	got, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Name: domain.Patch[string]{Set: true, Value: &name}})
	require.NoError(t, err)
	assert.True(t, got.NeedsReview, "an unrelated patch must not clear the flag")

	got, err = svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Reviewed: ptr(true)})
	require.NoError(t, err)
	assert.False(t, got.NeedsReview, "reviewed:true must clear the flag")
}

func TestB2UpdateComponentDismissClearsNeedsReview(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive, NeedsReview: true})

	dismissed := domain.ComponentStatusDismissed
	got, err := svc.UpdateComponent(ctx, comp.ID, domain.ComponentPatch{Status: &dismissed})
	require.NoError(t, err)
	assert.False(t, got.NeedsReview, "dismissing must also clear the flag")
}

func TestB2AddCheckValidatesManualCheckInput(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	_, err := svc.AddCheck(ctx, comp.ID, domain.NewCheckRequest{Name: "", Purpose: domain.CheckTest})
	assert.ErrorIs(t, err, ErrInvalidInput, "an empty name must be rejected")

	_, err = svc.AddCheck(ctx, comp.ID, domain.NewCheckRequest{Name: "lint", Purpose: "not-a-purpose"})
	assert.ErrorIs(t, err, ErrInvalidInput, "an invalid purpose must be rejected")

	_, err = svc.AddCheck(ctx, comp.ID, domain.NewCheckRequest{
		Name: "build", Purpose: domain.CheckBuild,
		LocalCommands: []domain.LocalCommand{{Dir: ".", Argv: []string{}}},
	})
	assert.ErrorIs(t, err, ErrInvalidInput, "an empty argv must be rejected")

	_, err = svc.AddCheck(ctx, comp.ID, domain.NewCheckRequest{
		Name: "build", Purpose: domain.CheckBuild,
		LocalCommands: []domain.LocalCommand{{Dir: ".", Argv: []string{"go build && rm -rf /"}}},
	})
	assert.ErrorIs(t, err, ErrInvalidInput, "shell metacharacters in argv[0] must be rejected")
}

func TestB2AddCheckDefaultsGateFromWhetherItHasCommands(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	noCommands, err := svc.AddCheck(ctx, comp.ID, domain.NewCheckRequest{Name: "security scan", Purpose: domain.CheckSecurity})
	require.NoError(t, err)
	assert.Equal(t, domain.CheckGateInfo, noCommands.Gate.Get())
	assert.Equal(t, domain.CheckSourceManual, noCommands.Source)
	assert.True(t, len(noCommands.JobKey) > len("manual:") && noCommands.JobKey[:7] == "manual:")

	withCommands, err := svc.AddCheck(ctx, comp.ID, domain.NewCheckRequest{
		Name: "build", Purpose: domain.CheckBuild,
		LocalCommands: []domain.LocalCommand{{Dir: ".", Argv: []string{"go", "build", "./..."}}},
	})
	require.NoError(t, err)
	assert.Equal(t, domain.CheckGateRequired, withCommands.Gate.Get())
	assert.Equal(t, ".", withCommands.LocalCommands.Get()[0].Dir)
}

func TestB2DeleteCheckOnlyAllowsManualChecks(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	ciCheck := b2SeedCheck(t, store, domain.ComponentCheck{RepositoryID: repo.ID, ComponentID: comp.ID, Source: domain.CheckSourceCI, JobKey: "ci-1"})

	err := svc.DeleteCheck(ctx, ciCheck.ID)
	assert.ErrorIs(t, err, ErrInvalidInput)

	manual, err := svc.AddCheck(ctx, comp.ID, domain.NewCheckRequest{Name: "manual", Purpose: domain.CheckOther})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteCheck(ctx, manual.ID))

	_, err = store.GetCheck(ctx, manual.ID)
	assert.Error(t, err, "a deleted check must be gone from the store")
}

func TestB2UpdateCheckReviewedAndDismissClearNeedsReview(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	check := b2SeedCheck(t, store, domain.ComponentCheck{
		RepositoryID: repo.ID, ComponentID: comp.ID, Source: domain.CheckSourceCI, JobKey: "ci-1",
		Status: domain.ModelStatusActive, NeedsReview: true,
	})

	got, err := svc.UpdateCheck(ctx, check.ID, domain.CheckPatch{Reviewed: ptr(true)})
	require.NoError(t, err)
	assert.False(t, got.NeedsReview, "reviewed:true must clear the flag")

	reflagged, err := store.SaveCheck(ctx, domain.ComponentCheck{
		ID: check.ID, RepositoryID: repo.ID, ComponentID: comp.ID, Source: domain.CheckSourceCI, JobKey: "ci-1",
		Status: domain.ModelStatusActive, NeedsReview: true,
	})
	require.NoError(t, err)
	require.True(t, reflagged.NeedsReview)

	dismissed := domain.ModelStatusDismissed
	got, err = svc.UpdateCheck(ctx, check.ID, domain.CheckPatch{Status: &dismissed})
	require.NoError(t, err)
	assert.False(t, got.NeedsReview, "dismissing must also clear the flag")
}

func b2SeedCheck(t *testing.T, store *b2Store, c domain.ComponentCheck) domain.ComponentCheck {
	t.Helper()
	saved, err := store.SaveCheck(context.Background(), c)
	require.NoError(t, err)
	return saved
}

func TestB2AddLinkRequiresExactlyOneTarget(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	to := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "worker", Status: domain.ComponentStatusActive})

	_, err := svc.AddLink(ctx, domain.NewLinkRequest{FromComponentID: from.ID})
	assert.ErrorIs(t, err, ErrInvalidInput, "neither target must be rejected")

	_, err = svc.AddLink(ctx, domain.NewLinkRequest{
		FromComponentID: from.ID, ToComponentID: &to.ID,
		ToResource: &domain.ResourceRef{Kind: domain.ResourceDatabase, Name: "db"},
	})
	assert.ErrorIs(t, err, ErrInvalidInput, "both targets must be rejected")

	_, err = svc.AddLink(ctx, domain.NewLinkRequest{FromComponentID: from.ID, ToComponentID: &from.ID})
	assert.ErrorIs(t, err, ErrInvalidInput, "a link to its own component must be rejected")

	link, err := svc.AddLink(ctx, domain.NewLinkRequest{FromComponentID: from.ID, ToComponentID: &to.ID})
	require.NoError(t, err)
	assert.Equal(t, domain.LinkOther, link.Protocol, "an empty protocol defaults to other")
	assert.Equal(t, domain.LinkConfirmed, link.Status)
	assert.Equal(t, domain.LinkSourceUser, link.Source)
	assert.Equal(t, domain.ConfidenceExact, link.Confidence)
}

func TestB2AddLinkToAResourceUsesAUserScopedIdentityKey(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	link, err := svc.AddLink(ctx, domain.NewLinkRequest{
		FromComponentID: from.ID,
		ToResource:      &domain.ResourceRef{Kind: domain.ResourceDatabase, Vendor: "Postgres", Name: "Primary DB"},
		Protocol:        domain.LinkSQL,
	})
	require.NoError(t, err)
	require.NotNil(t, link.ToResourceID)

	res, err := store.GetResource(ctx, *link.ToResourceID)
	require.NoError(t, err)
	assert.Equal(t, "user:database:postgres", res.IdentityKey)

	link2, err := svc.AddLink(ctx, domain.NewLinkRequest{
		FromComponentID: from.ID,
		ToResource:      &domain.ResourceRef{Kind: domain.ResourceDatabase, Vendor: "Postgres", Name: "A different label"},
	})
	require.NoError(t, err)
	assert.Equal(t, *link.ToResourceID, *link2.ToResourceID, "the same kind+vendor must dedupe to one resource")
}

func TestB2UpdateLinkRetargetResetsAutoConfirmedAndConfirms(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	oldTarget := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "old", Status: domain.ComponentStatusActive})
	newTarget := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "new", Status: domain.ComponentStatusActive})

	link, err := store.SaveLink(ctx, domain.ComponentLink{
		RepositoryID: repo.ID, FromComponentID: from.ID, ToComponentID: &oldTarget.ID,
		Status: domain.LinkSuggested, AutoConfirmed: true, Reason: "package name match", Source: domain.LinkSourceScan,
	})
	require.NoError(t, err)

	got, err := svc.UpdateLink(ctx, link.ID, domain.LinkPatch{ToComponentID: &newTarget.ID})
	require.NoError(t, err)
	assert.Equal(t, newTarget.ID, *got.ToComponentID)
	assert.Nil(t, got.ToResourceID)
	assert.False(t, got.AutoConfirmed)
	assert.Empty(t, got.Reason)
	assert.Equal(t, domain.LinkConfirmed, got.Status, "a retarget without an explicit status confirms")

	dismissed := domain.LinkDismissed
	got2, err := svc.UpdateLink(ctx, link.ID, domain.LinkPatch{ToComponentID: &oldTarget.ID, Status: &dismissed})
	require.NoError(t, err)
	assert.Equal(t, domain.LinkDismissed, got2.Status, "an explicit status on a retarget wins over the auto-confirm")

	_, err = svc.UpdateLink(ctx, link.ID, domain.LinkPatch{ToComponentID: &from.ID})
	assert.ErrorIs(t, err, ErrInvalidInput, "retargeting to its own component must be rejected")
}

func TestB2DeleteLinkOnlyAllowsUserLinks(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	to := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "other", Status: domain.ComponentStatusActive})

	scanLink, err := store.SaveLink(ctx, domain.ComponentLink{
		RepositoryID: repo.ID, FromComponentID: from.ID, ToComponentID: &to.ID, Source: domain.LinkSourceScan,
	})
	require.NoError(t, err)
	assert.ErrorIs(t, svc.DeleteLink(ctx, scanLink.ID), ErrInvalidInput)

	userLink, err := svc.AddLink(ctx, domain.NewLinkRequest{FromComponentID: from.ID, ToComponentID: &to.ID})
	require.NoError(t, err)
	require.NoError(t, svc.DeleteLink(ctx, userLink.ID))
	_, err = store.GetLink(ctx, userLink.ID)
	assert.Error(t, err)
}
