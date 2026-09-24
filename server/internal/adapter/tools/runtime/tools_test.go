package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cloudapp "github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type testFixture struct {
	kit        *ToolKit
	components *fakeComponentStore
	accounts   *fakeCloudAccounts
	envs       *fakeEnvironmentStore
	provider   *fakeCloudProvider
}

func newTestKit(t *testing.T) testFixture {
	t.Helper()
	components := newFakeComponentStore()
	accounts := newFakeCloudAccounts()
	envs := newFakeEnvironmentStore()
	provider := &fakeCloudProvider{kind: domain.CloudVercel}

	cloudSvc := cloudapp.NewService(cloudapp.Deps{
		Accounts:     accounts,
		Environments: envs,
		Providers:    []port.CloudProvider{provider},
		Components:   components,
	})
	return testFixture{
		kit:        &ToolKit{Components: components, Cloud: cloudSvc},
		components: components,
		accounts:   accounts,
		envs:       envs,
		provider:   provider,
	}
}

func repoCtx(id uuid.UUID) context.Context {
	return registry.ContextWithRepositoryID(context.Background(), id)
}

func TestNewExecutors(t *testing.T) {
	assert.Nil(t, NewExecutors(nil))
	assert.Nil(t, NewExecutors(&ToolKit{}))

	fx := newTestKit(t)
	execs := NewExecutors(fx.kit)
	require.Len(t, execs, 4)
	names := make(map[string]bool, len(execs))
	for _, e := range execs {
		names[e.Name()] = true
	}
	assert.True(t, names[getEnvironmentToolName])
	assert.True(t, names[queryRuntimeLogsToolName])
	assert.True(t, names[listRuntimeErrorsToolName])
	assert.True(t, names[listDeploymentsToolName])
}

// boundEnvironment seeds one active component with one confirmed, bound
// environment and returns both plus the repository id.
func (fx testFixture) boundEnvironment(env domain.DeployEnvironment, path string) (uuid.UUID, domain.Component, domain.ComponentEnvironment) {
	repoID := uuid.New()
	comp := fx.components.put(domain.Component{RepositoryID: repoID, Path: path})
	acct := fx.accounts.put(domain.CloudAccount{Provider: domain.CloudVercel})
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1", Name: "web"}
	e := fx.envs.put(domain.ComponentEnvironment{
		RepositoryID: repoID, ComponentID: comp.ID, Environment: env,
		Provider: domain.CloudVercel, AccountID: &acct.ID, Resource: &ref,
		URL: "https://web.example.com", Status: domain.LinkConfirmed,
	})
	return repoID, comp, e
}

func TestGetEnvironmentListsEveryEnvironmentOfTheOnlyComponent(t *testing.T) {
	fx := newTestKit(t)
	repoID, _, _ := fx.boundEnvironment(domain.EnvironmentProduction, "apps/web")

	tool := &getEnvironmentTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "\"provider\":\"vercel\"")
	assert.Contains(t, res.Content, "\"bound\":true")
}

func TestGetEnvironmentFiltersByEnvironment(t *testing.T) {
	fx := newTestKit(t)
	repoID, comp, _ := fx.boundEnvironment(domain.EnvironmentProduction, "apps/web")
	fx.envs.put(domain.ComponentEnvironment{
		RepositoryID: repoID, ComponentID: comp.ID, Environment: domain.EnvironmentStaging, Status: domain.LinkSuggested,
	})

	tool := &getEnvironmentTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{"environment":"staging"}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "staging")
	assert.NotContains(t, res.Content, "\"environment\":\"production\"")
}

func TestGetEnvironmentAmbiguousComponentListsPaths(t *testing.T) {
	fx := newTestKit(t)
	repoID := uuid.New()
	fx.components.put(domain.Component{RepositoryID: repoID, Path: "apps/api"})
	fx.components.put(domain.Component{RepositoryID: repoID, Path: "apps/web"})

	tool := &getEnvironmentTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "apps/api")
	assert.Contains(t, res.Content, "apps/web")
}

func TestQueryRuntimeLogsOnAnUnboundEnvironmentNamesTheDeployTab(t *testing.T) {
	fx := newTestKit(t)
	repoID := uuid.New()
	fx.components.put(domain.Component{RepositoryID: repoID, Path: "."})

	tool := &queryRuntimeLogsTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "no production environment is bound")
	assert.Contains(t, res.Content, "Deploy tab")
}

func TestQueryRuntimeLogsReturnsEntriesNewestFirst(t *testing.T) {
	fx := newTestKit(t)
	repoID, _, _ := fx.boundEnvironment(domain.EnvironmentProduction, ".")
	now := time.Now().UTC()
	fx.provider.logsPage = domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{
		{Timestamp: now.Add(-time.Hour), Severity: domain.LogError, Message: "older"},
		{Timestamp: now, Severity: domain.LogError, Message: "newest"},
	}}

	tool := &queryRuntimeLogsTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{}`)
	require.False(t, res.IsError, res.Content)
	newestIdx := indexOf(res.Content, "newest")
	olderIdx := indexOf(res.Content, "older")
	require.NotEqual(t, -1, newestIdx)
	require.NotEqual(t, -1, olderIdx)
	assert.Less(t, newestIdx, olderIdx)
}

func TestQueryRuntimeLogsRejectsAnInvalidSince(t *testing.T) {
	fx := newTestKit(t)
	repoID, _, _ := fx.boundEnvironment(domain.EnvironmentProduction, ".")

	tool := &queryRuntimeLogsTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{"since":"not-a-duration"}`)
	assert.True(t, res.IsError)
}

func TestQueryRuntimeLogsRejectsAnUnknownSeverity(t *testing.T) {
	fx := newTestKit(t)
	repoID, _, _ := fx.boundEnvironment(domain.EnvironmentProduction, ".")

	tool := &queryRuntimeLogsTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{"min_severity":"catastrophic"}`)
	assert.True(t, res.IsError)
}

// A caller-supplied limit past maxLogLimit must not reach the provider
// unclamped; the fake returns an empty page regardless, so this only pins
// that an oversized limit is accepted (clamped) rather than refused.
func TestQueryRuntimeLogsClampsLimitToTheMax(t *testing.T) {
	fx := newTestKit(t)
	repoID, _, _ := fx.boundEnvironment(domain.EnvironmentProduction, ".")

	tool := &queryRuntimeLogsTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{"limit":10000}`)
	require.False(t, res.IsError, res.Content)
}

func TestListRuntimeErrorsTruncatesTheSampleToTenLines(t *testing.T) {
	fx := newTestKit(t)
	repoID, _, _ := fx.boundEnvironment(domain.EnvironmentProduction, ".")
	longSample := ""
	for i := 0; i < 20; i++ {
		if i > 0 {
			longSample += "\n"
		}
		longSample += "line"
	}
	fx.provider.errGroups = []domain.RuntimeErrorGroup{
		{Message: "boom", Count: 5, Sample: longSample, New: true},
	}

	tool := &listRuntimeErrorsTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "\"sample_truncated\":true")
	assert.Contains(t, res.Content, "\"new\":true")
}

func TestListDeploymentsDefaultsLimitToTen(t *testing.T) {
	fx := newTestKit(t)
	repoID, _, _ := fx.boundEnvironment(domain.EnvironmentProduction, ".")
	fx.provider.deploys = []domain.CloudDeployment{{ID: "dpl_1", Status: domain.CloudDeployReady}}

	tool := &listDeploymentsTool{kit: fx.kit}
	res := tool.Execute(repoCtx(repoID), `{}`)
	require.False(t, res.IsError, res.Content)
	assert.Contains(t, res.Content, "dpl_1")
	assert.Contains(t, res.Content, "\"count\":1")
}

func TestToolsRequireARepositoryOrRepositoryIDArg(t *testing.T) {
	fx := newTestKit(t)
	tool := &getEnvironmentTool{kit: fx.kit}
	res := tool.Execute(context.Background(), `{}`)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "repository")
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
