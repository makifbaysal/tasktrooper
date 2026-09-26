package projectmodel

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestB2BackfillMonorepoRoleOverridesOnlyWhenDifferentAndCreatesManualComponentForAMissingSubProject(t *testing.T) {
	svc, store, repos, _, _, scanner := newB2Service(t)
	ctx := context.Background()

	repo := b2SeedRepo(t, repos, "demo")
	repo.Kind = domain.RepoKindMonorepo
	repo.SubProjects = []domain.RepoSubProject{
		{Path: "api", Kind: domain.RepoKindBackend, Docs: domain.RepositoryDocs{CodingStandards: "api/AGENTS.md"}},
		{Path: "web", Kind: domain.RepoKindFrontend},
		{Path: "missing-svc", Kind: domain.RepoKindWorker, Docs: domain.RepositoryDocs{CodingStandards: "missing-svc/AGENTS.md"}, CoverageEnabled: boolPtr(true)},
	}
	repo = repos.put(repo)

	scanner.result = domain.ScanResult{
		Components: []domain.DetectedComponent{
			// The scan disagrees with the legacy kind for "api" (worker vs the
			// legacy backend), so the backfill must override it back to backend.
			{Path: "api", Name: "api", Role: domain.ComponentRoleWorker, RoleConfidence: domain.ConfidenceExact},
			// The scan agrees with the legacy kind for "web", so no override.
			{Path: "web", Name: "web", Role: domain.ComponentRoleFrontend, RoleConfidence: domain.ConfidenceExact},
		},
		Git: domain.ScanGit{HeadSHA: "abc123"},
	}

	svc.migrateLegacy(ctx)

	components, err := store.ListComponents(ctx, repo.ID)
	require.NoError(t, err)
	byPath := map[string]domain.Component{}
	for _, c := range components {
		byPath[c.Path] = c
	}

	api, ok := byPath["api"]
	require.True(t, ok)
	assert.True(t, api.Role.Overridden(), "api's scanned role disagreed with the legacy kind and must be overridden")
	assert.Equal(t, domain.ComponentRoleBackend, api.Role.Get())
	assert.Equal(t, "api/AGENTS.md", api.Docs.CodingStandards)

	web, ok := byPath["web"]
	require.True(t, ok)
	assert.False(t, web.Role.Overridden(), "web's scanned role already matched the legacy kind")

	missing, ok := byPath["missing-svc"]
	require.True(t, ok, "a legacy sub-project the scan did not find must become a manual component")
	assert.True(t, missing.ManuallyAdded)
	assert.Equal(t, domain.ComponentStatusActive, missing.Status)
	assert.Equal(t, domain.ComponentRoleWorker, missing.Role.Get())
	assert.Equal(t, "missing-svc/AGENTS.md", missing.Docs.CodingStandards)
	require.NotNil(t, missing.Gates.CoverageEnabled)
	assert.True(t, *missing.Gates.CoverageEnabled)
}

func boolPtr(b bool) *bool { return &b }

func TestB2BackfillSingleRepoMovesRepositoryDocsOntoTheRootComponent(t *testing.T) {
	svc, store, repos, _, _, scanner := newB2Service(t)
	ctx := context.Background()

	repo := b2SeedRepo(t, repos, "demo")
	repo.Kind = domain.RepoKindBackend
	repo.Docs = domain.RepositoryDocs{CodingStandards: ".ai/coding-standards.md", LocalRun: "scripts/dev.sh"}
	repo = repos.put(repo)

	scanner.result = domain.ScanResult{
		Components: []domain.DetectedComponent{{Path: ".", Name: "demo", Role: domain.ComponentRoleBackend, RoleConfidence: domain.ConfidenceExact}},
	}

	svc.migrateLegacy(ctx)

	components, err := store.ListComponents(ctx, repo.ID)
	require.NoError(t, err)
	root, ok := componentAtPath(components, ".")
	require.True(t, ok)
	assert.Equal(t, repo.Docs, root.Docs)
	assert.False(t, root.Role.Overridden(), "the scanned role already matched the legacy kind")
}

func TestB2BackfillPipelineMappingSetsGateAndDeployOverrides(t *testing.T) {
	svc, store, repos, pipelines, _, scanner := newB2Service(t)
	ctx := context.Background()

	repo := b2SeedRepo(t, repos, "demo")
	repo.Kind = domain.RepoKindMonorepo
	repo.SubProjects = []domain.RepoSubProject{
		{Path: "api", Kind: domain.RepoKindBackend},
		{Path: "web", Kind: domain.RepoKindFrontend},
	}
	repo = repos.put(repo)

	pipelines.seed(repo.ID, []domain.RepositoryPipelineJob{
		{RepositoryID: repo.ID, SubProjectPath: "api", Category: domain.PipelineCategoryValidate, TargetKind: domain.PipelineTargetJob, TargetRef: "lint-job"},
		{RepositoryID: repo.ID, SubProjectPath: "web", Category: domain.PipelineCategoryProdDeploy, TargetKind: domain.PipelineTargetWorkflow, TargetRef: "deploy-prod.yml"},
	})

	scanner.result = domain.ScanResult{
		Components: []domain.DetectedComponent{
			{Path: "api", Name: "api", Role: domain.ComponentRoleBackend, RoleConfidence: domain.ConfidenceExact},
			{Path: "web", Name: "web", Role: domain.ComponentRoleFrontend, RoleConfidence: domain.ConfidenceExact},
		},
		Checks: []domain.DetectedCheck{
			{ComponentPath: "api", Workflow: ".github/workflows/ci.yml", JobKey: "lint-job", JobName: "lint-job", Purpose: domain.CheckLint, Confidence: domain.ConfidenceExact},
			{ComponentPath: "web", Workflow: ".github/workflows/deploy-prod.yml", JobKey: "deploy", JobName: "deploy", Purpose: domain.CheckOther, Confidence: domain.ConfidenceExact},
		},
		Git: domain.ScanGit{HeadSHA: "abc123"},
	}

	svc.migrateLegacy(ctx)

	checks, err := store.ListChecks(ctx, repo.ID)
	require.NoError(t, err)
	var lint, deploy domain.ComponentCheck
	for _, c := range checks {
		switch c.JobKey {
		case "lint-job":
			lint = c
		case "deploy":
			deploy = c
		}
	}

	require.NotEqual(t, uuid.Nil, lint.ID, "the lint check must have been reconciled from the scan")
	assert.Equal(t, domain.CheckGateRequired, lint.Gate.Get(), "a validate pipeline mapping must force the gate to required")
	assert.True(t, lint.Gate.Overridden())

	require.NotEqual(t, uuid.Nil, deploy.ID)
	assert.Equal(t, domain.CheckDeploy, deploy.Purpose.Get(), "a *_deploy workflow mapping must set the purpose override to deploy")
	assert.True(t, deploy.Purpose.Overridden())
}

func TestB2BackfillDependencyMigrationIsIdempotent(t *testing.T) {
	svc, store, repos, _, legacy, _ := newB2Service(t)
	ctx := context.Background()

	repo := b2SeedRepo(t, repos, "demo")
	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	dep := legacy.seedDependency(repo.ID, domain.LegacyDependency{
		TargetKind: domain.DependencyTargetDatabase, DatabaseLabel: "Primary DB", DatabaseEngine: "postgres",
		DatabaseHost: "db.internal", DatabasePort: 5432, Note: "the main store",
	})

	svc.migrateLegacyDependencies(ctx, repo, []domain.Component{from})

	links, err := store.ListLinks(ctx, repo.ID)
	require.NoError(t, err)
	require.Len(t, links, 1)
	link := links[0]
	assert.Equal(t, from.ID, link.FromComponentID)
	assert.Equal(t, domain.LinkSQL, link.Protocol)
	assert.Contains(t, link.Detail, "legacy:"+dep.ID.String())
	require.NotNil(t, link.ToResourceID)

	res, err := store.GetResource(ctx, *link.ToResourceID)
	require.NoError(t, err)
	assert.Equal(t, "legacy-db:"+dep.ID.String(), res.IdentityKey)
	assert.Equal(t, "db.internal", res.Details["host"])

	svc.migrateLegacyDependencies(ctx, repo, []domain.Component{from})
	links, err = store.ListLinks(ctx, repo.ID)
	require.NoError(t, err)
	assert.Len(t, links, 1, "a dependency already carrying its legacy marker must not be migrated twice")
}

func TestB2BootFailsInterruptedScansAndMigratesZeroComponentRepositoriesOldestFirst(t *testing.T) {
	svc, store, repos, _, _, scanner := newB2Service(t)
	ctx := context.Background()

	stuck, err := store.CreateScan(ctx, domain.ProjectScan{RepositoryID: uuid.New(), Trigger: domain.ScanTriggerManual, Status: domain.ScanRunning, StartedAt: time.Now()})
	require.NoError(t, err)

	older := repos.put(domain.Repository{ID: uuid.New(), Name: "older", RootPath: t.TempDir(), CreatedAt: time.Now().Add(-time.Hour)})
	newer := repos.put(domain.Repository{ID: uuid.New(), Name: "newer", RootPath: t.TempDir(), CreatedAt: time.Now()})
	scanner.setResultFor(older.RootPath, domain.ScanResult{Components: []domain.DetectedComponent{{Path: ".", Name: "older", Role: domain.ComponentRoleBackend, RoleConfidence: domain.ConfidenceExact}}})
	scanner.setResultFor(newer.RootPath, domain.ScanResult{Components: []domain.DetectedComponent{{Path: ".", Name: "newer", Role: domain.ComponentRoleBackend, RoleConfidence: domain.ConfidenceExact}}})

	svc.SetBackgroundContext(ctx)
	svc.Boot(ctx)

	require.Eventually(t, func() bool {
		oc, _ := store.ListComponents(ctx, older.ID)
		nc, _ := store.ListComponents(ctx, newer.ID)
		return len(oc) > 0 && len(nc) > 0
	}, 5*time.Second, 20*time.Millisecond, "boot must migrate both zero-component repositories in the background")

	failed, err := store.GetScan(ctx, stuck.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScanFailed, failed.Status, "a scan left running by a killed process must be failed on boot")

	order := scanner.callOrder()
	require.GreaterOrEqual(t, len(order), 2)
	oldIdx, newIdx := indexOf(order, older.RootPath), indexOf(order, newer.RootPath)
	require.NotEqual(t, -1, oldIdx)
	require.NotEqual(t, -1, newIdx)
	assert.Less(t, oldIdx, newIdx, "the older repository must be scanned before the newer one")
}

func indexOf(list []string, target string) int {
	for i, v := range list {
		if v == target {
			return i
		}
	}
	return -1
}
