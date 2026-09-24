package projectmodel

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestB2BriefWithNoComponentsIsTheScanPrompt(t *testing.T) {
	svc, _, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := repos.put(domain.Repository{ID: uuid.New(), Name: "demo", Description: "A demo repo.", RootPath: t.TempDir()})

	brief, err := svc.Brief(ctx, repo.ID, BriefScope{})
	require.NoError(t, err)
	assert.Contains(t, brief, "# demo")
	assert.Contains(t, brief, "A demo repo.")
	assert.Contains(t, brief, "has not been scanned yet")
}

func TestB2BriefSingleComponentHeaderAndLayout(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")

	backend := "backend"
	build := "go build ./..."
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Name: domain.Fact[string]{Detected: &backend},
		Role: domain.Fact[domain.ComponentRole]{Detected: rolePtr(domain.ComponentRoleBackend)},
		Stack: domain.Fact[domain.ComponentStack]{Detected: &domain.ComponentStack{
			Languages: []domain.StackItem{{Name: "Go"}}, PackageManager: "go modules",
		}},
		Commands: []domain.ComponentCommand{{Purpose: domain.CommandBuild, Command: domain.Fact[string]{Detected: &build}}},
		Docs:     domain.RepositoryDocs{CodingStandards: ".ai/coding-standards.md"},
	})
	required := "required"
	_, err := store.SaveCheck(ctx, domain.ComponentCheck{
		RepositoryID: repo.ID, ComponentID: comp.ID, Source: domain.CheckSourceCI,
		Workflow: ".github/workflows/ci.yml", JobKey: "test", JobName: "test",
		Purpose:       domain.Fact[domain.CheckPurpose]{Detected: purposePtr(domain.CheckTest)},
		LocalCommands: domain.Fact[[]domain.LocalCommand]{Detected: &[]domain.LocalCommand{{Dir: ".", Argv: []string{"go", "test", "./..."}}}},
		Gate:          domain.Fact[domain.CheckGate]{Override: gatePtr(domain.CheckGate(required))},
		Status:        domain.ModelStatusActive,
	})
	require.NoError(t, err)
	_, err = store.SaveNote(ctx, domain.ProjectNote{RepositoryID: repo.ID, ComponentID: &comp.ID, Topic: domain.NotePurpose, BodyMD: "Serves the API.", Author: domain.NoteAuthorAgent, Stale: true})
	require.NoError(t, err)

	brief, err := svc.Brief(ctx, repo.ID, BriefScope{})
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(brief, "# demo (backend)"), "single in-scope component collapses the header: %q", brief)
	assert.Contains(t, brief, "Single repository.")
	assert.Contains(t, brief, "Stack: Go")
	assert.Contains(t, brief, "package manager go modules")
	assert.Contains(t, brief, "Commands (run at the repository root):")
	assert.Contains(t, brief, "- build: `go build ./...`")
	assert.Contains(t, brief, "Before handing off, run what CI runs:")
	assert.Contains(t, brief, "`go test ./...`")
	assert.Contains(t, brief, "## Notes")
	assert.Contains(t, brief, "**Purpose** Serves the API. _(may be outdated)_")
	assert.Contains(t, brief, "## Reference docs")
	assert.Contains(t, brief, "coding standards: .ai/coding-standards.md")
}

func rolePtr(r domain.ComponentRole) *domain.ComponentRole  { return &r }
func purposePtr(p domain.CheckPurpose) *domain.CheckPurpose { return &p }
func gatePtr(g domain.CheckGate) *domain.CheckGate          { return &g }

func TestB2BriefMonorepoHeaderListsEveryComponent(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "api", Status: domain.ComponentStatusActive, Role: domain.Fact[domain.ComponentRole]{Detected: rolePtr(domain.ComponentRoleBackend)}})
	b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "web", Status: domain.ComponentStatusActive, Role: domain.Fact[domain.ComponentRole]{Detected: rolePtr(domain.ComponentRoleFrontend)}})

	brief, err := svc.Brief(ctx, repo.ID, BriefScope{})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(brief, "# demo\n"))
	assert.Contains(t, brief, "Monorepo with 2 components:")
	assert.Contains(t, brief, "## api (backend)")
	assert.Contains(t, brief, "## web (frontend)")
}

func TestB2BriefAreaScopingFallsBackToAllWhenNothingMatches(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "api", Status: domain.ComponentStatusActive, Role: domain.Fact[domain.ComponentRole]{Detected: rolePtr(domain.ComponentRoleBackend)}})
	b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "web", Status: domain.ComponentStatusActive, Role: domain.Fact[domain.ComponentRole]{Detected: rolePtr(domain.ComponentRoleFrontend)}})

	brief, err := svc.Brief(ctx, repo.ID, BriefScope{Area: "backend"})
	require.NoError(t, err)
	assert.Contains(t, brief, "## api (backend)")
	assert.NotContains(t, brief, "## web (frontend)")

	brief, err = svc.Brief(ctx, repo.ID, BriefScope{Area: "worker"})
	require.NoError(t, err)
	assert.Contains(t, brief, "## api (backend)")
	assert.Contains(t, brief, "## web (frontend)", "an area nothing matches must fall back to every active component")
}

func TestB2BriefTruncatesComponentSectionsFromTheEndButKeepsTheHeader(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	longBody := strings.Repeat("x", 3000)
	for _, p := range []string{"aaa", "bbb", "ccc", "ddd"} {
		comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: p, Status: domain.ComponentStatusActive, Role: domain.Fact[domain.ComponentRole]{Detected: rolePtr(domain.ComponentRoleBackend)}})
		_, err := store.SaveNote(ctx, domain.ProjectNote{RepositoryID: repo.ID, ComponentID: &comp.ID, Topic: domain.NoteGotchas, BodyMD: longBody, Author: domain.NoteAuthorAgent})
		require.NoError(t, err)
	}

	brief, err := svc.Brief(ctx, repo.ID, BriefScope{})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(brief), briefBudget)
	assert.Contains(t, brief, "# demo", "the header must survive truncation")
	assert.Contains(t, brief, "## aaa (backend)", "earlier component sections are kept")
	assert.NotContains(t, brief, "## ddd (backend)", "later component sections are dropped first")
}

func TestB2RequiredCommandsDedupesAndDefaultsToActiveComponents(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	cmds := []domain.LocalCommand{{Dir: ".", Argv: []string{"go", "test", "./..."}}}
	required := domain.CheckGateRequired
	_, err := store.SaveCheck(ctx, domain.ComponentCheck{
		RepositoryID: repo.ID, ComponentID: comp.ID, JobKey: "a", Status: domain.ModelStatusActive,
		LocalCommands: domain.Fact[[]domain.LocalCommand]{Override: &cmds}, Gate: domain.Fact[domain.CheckGate]{Override: &required},
	})
	require.NoError(t, err)
	_, err = store.SaveCheck(ctx, domain.ComponentCheck{
		RepositoryID: repo.ID, ComponentID: comp.ID, JobKey: "b", Status: domain.ModelStatusActive,
		LocalCommands: domain.Fact[[]domain.LocalCommand]{Override: &cmds}, Gate: domain.Fact[domain.CheckGate]{Override: &required},
	})
	require.NoError(t, err)

	got, err := svc.RequiredCommands(ctx, repo.ID, nil)
	require.NoError(t, err)
	assert.Len(t, got, 1, "the same Dir+argv from two checks must dedupe to one command")
}

func TestB2ComponentsForPathsUsesOwningComponent(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	root := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})
	api := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "api", Status: domain.ComponentStatusActive})

	got, err := svc.ComponentsForPaths(ctx, repo.ID, []string{"api/handler.go", "api/handler.go", "README.md"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []uuid.UUID{api.ID, root.ID}, got)
}

func TestB2ComponentsForAreaMatchesRoleOrLegacyKind(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	desktop := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: "app", Status: domain.ComponentStatusActive, Role: domain.Fact[domain.ComponentRole]{Detected: rolePtr(domain.ComponentRoleDesktop)}})

	got, err := svc.ComponentsForArea(ctx, repo.ID, domain.RepoKindFrontend)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, desktop.ID, got[0], "desktop's legacy repo kind is frontend")
}
