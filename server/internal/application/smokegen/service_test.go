package smokegen

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeComponents struct{ comp domain.Component }

func (f fakeComponents) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	if id != f.comp.ID {
		return domain.Component{}, port.ErrNotFound
	}
	return f.comp, nil
}

type fakeRepos struct{ repo domain.Repository }

func (f fakeRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) { return f.repo, nil }

type fakeAgents struct{ agents []domain.Agent }

func (f fakeAgents) ListAgents(context.Context) ([]domain.Agent, error) { return f.agents, nil }

type fakeRunner struct {
	mu        sync.Mutex
	answers   []string
	err       error
	block     chan struct{}
	calls     int
	workspace string
	policy    domain.ToolPolicy
}

func (f *fakeRunner) Run(ctx context.Context, _ []domain.Message, _ string, _ domain.LLMProviderType, policy domain.ToolPolicy, _ ...agent.RunOption) (domain.AgentResponse, error) {
	f.mu.Lock()
	f.calls++
	f.workspace = registry.EffectiveWorkspaceDir(ctx)
	f.policy = policy
	block := f.block
	var answer string
	if len(f.answers) > 0 {
		answer = f.answers[0]
		f.answers = f.answers[1:]
	}
	err := f.err
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return domain.AgentResponse{}, ctx.Err()
		}
	}
	if err != nil {
		return domain.AgentResponse{}, err
	}
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: answer}}, nil
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeSmoke struct {
	mu     sync.Mutex
	tested []domain.SmokeCheck
	err    error
}

func (f *fakeSmoke) TestSmoke(_ context.Context, _ uuid.UUID, checks []domain.SmokeCheck) (string, []domain.SmokeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tested = checks
	if f.err != nil {
		return "", nil, f.err
	}
	out := make([]domain.SmokeResult, 0, len(checks))
	for _, c := range checks {
		out = append(out, domain.SmokeResult{Check: c, URL: "https://example.com" + c.Path, Status: 200, OK: true})
	}
	return "https://example.com", out, nil
}

type fixture struct {
	svc    *Service
	comp   domain.Component
	runner *fakeRunner
	smoke  *fakeSmoke
	now    *time.Time
}

func newFixture(t *testing.T, runner *fakeRunner, agents []domain.Agent) fixture {
	t.Helper()
	repo := domain.Repository{ID: uuid.New(), Name: "demo", RootPath: "/tmp/demo"}
	comp := domain.Component{ID: uuid.New(), RepositoryID: repo.ID, Path: "apps/web", Status: domain.ComponentStatusActive}
	if agents == nil {
		agents = []domain.Agent{{ID: uuid.New(), Name: "Release engineer", CatalogSlug: ReleaseEngineerSlug, Enabled: true, ProviderType: "claude_code"}}
	}
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	smoke := &fakeSmoke{}
	svc := New(Deps{
		Components: fakeComponents{comp: comp},
		Repos:      fakeRepos{repo: repo},
		Agents:     fakeAgents{agents: agents},
		Runner:     runner,
		Smoke:      smoke,
		Brief: func(context.Context, uuid.UUID, uuid.UUID) (string, error) {
			return "# demo\nRuns on: production https://example.com", nil
		},
		Now: func() time.Time { return now },
	})
	return fixture{svc: svc, comp: comp, runner: runner, smoke: smoke, now: &now}
}

func waitFinished(t *testing.T, svc *Service, id uuid.UUID) Job {
	t.Helper()
	var job Job
	require.Eventually(t, func() bool {
		j, err := svc.Get(id)
		require.NoError(t, err)
		job = j
		return j.Status != StatusRunning
	}, 2*time.Second, 5*time.Millisecond)
	return job
}

func TestGenerateProposesFiltersAndTestsChecks(t *testing.T) {
	runner := &fakeRunner{answers: []string{"Here you go:\n```json\n" + `{"checks":[
		{"name":"Home","method":"get","path":"/","expect_status":200},
		{"name":"Health","method":"GET","path":"/api/health","expect_status":200,"contains":"ok","max_latency_ms":3000},
		{"name":"Dup of existing","method":"GET","path":"/health"},
		{"name":"Bad method","method":"POST","path":"/login"},
		{"name":"No slash","method":"GET","path":"status"},
		{"name":"Home again","method":"GET","path":"/"}
	]}` + "\n```"}}
	f := newFixture(t, runner, nil)

	job, err := f.svc.Generate(context.Background(), f.comp.ID, []domain.SmokeCheck{{Method: "GET", Path: "/health"}})
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, job.Status)
	assert.Equal(t, "Release engineer", job.AgentName)

	done := waitFinished(t, f.svc, job.JobID)
	require.Equal(t, StatusDone, done.Status, done.Error)
	require.Len(t, done.Checks, 2)
	assert.Equal(t, "GET", done.Checks[0].Method)
	assert.Equal(t, "/", done.Checks[0].Path)
	assert.Equal(t, "/api/health", done.Checks[1].Path)
	assert.Equal(t, 4, done.Dropped)
	require.Len(t, done.Results, 2)
	assert.Equal(t, "https://example.com", done.BaseURL)
	assert.Equal(t, done.Checks, f.smoke.tested)
	assert.NotNil(t, done.FinishedAt)

	assert.Equal(t, "/tmp/demo", runner.workspace, "the agent must run in the repository checkout")
	assert.ElementsMatch(t, readOnlyTools, runner.policy.AllowTools)
}

func TestGenerateRetriesOnceWhenTheAnswerIsNotJSON(t *testing.T) {
	runner := &fakeRunner{answers: []string{"I think /health is good.", `[{"method":"GET","path":"/health"}]`}}
	f := newFixture(t, runner, nil)

	job, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	done := waitFinished(t, f.svc, job.JobID)

	require.Equal(t, StatusDone, done.Status)
	require.Len(t, done.Checks, 1)
	assert.Equal(t, 2, runner.callCount())
}

func TestGenerateFailsWhenTheAnswerStaysUnparsable(t *testing.T) {
	runner := &fakeRunner{answers: []string{"no", "still no"}}
	f := newFixture(t, runner, nil)

	job, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	done := waitFinished(t, f.svc, job.JobID)

	assert.Equal(t, StatusFailed, done.Status)
	assert.Contains(t, done.Error, "not valid JSON")
}

func TestGenerateReportsARunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("claude: not signed in")}
	f := newFixture(t, runner, nil)

	job, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	done := waitFinished(t, f.svc, job.JobID)

	assert.Equal(t, StatusFailed, done.Status)
	assert.Contains(t, done.Error, "claude: not signed in")
}

func TestGenerateWithNothingUsableIsDoneWithAnExplanation(t *testing.T) {
	runner := &fakeRunner{answers: []string{`{"checks":[{"method":"DELETE","path":"/users"}]}`}}
	f := newFixture(t, runner, nil)

	job, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	done := waitFinished(t, f.svc, job.JobID)

	assert.Equal(t, StatusDone, done.Status)
	assert.Empty(t, done.Checks)
	assert.Empty(t, done.Results)
	assert.Equal(t, 1, done.Dropped)
	assert.NotEmpty(t, done.Error)
	assert.Nil(t, f.smoke.tested, "nothing to test")
}

func TestGenerateKeepsChecksWhenTheTestRunFails(t *testing.T) {
	runner := &fakeRunner{answers: []string{`{"checks":[{"method":"GET","path":"/health"}]}`}}
	f := newFixture(t, runner, nil)
	f.smoke.err = errors.New("no base URL")

	job, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	done := waitFinished(t, f.svc, job.JobID)

	assert.Equal(t, StatusDone, done.Status)
	require.Len(t, done.Checks, 1)
	assert.Empty(t, done.Results)
	assert.Contains(t, done.Error, "no base URL")
}

func TestGenerateReturnsTheRunningJobInsteadOfStartingAnother(t *testing.T) {
	runner := &fakeRunner{answers: []string{`{"checks":[]}`}, block: make(chan struct{})}
	f := newFixture(t, runner, nil)

	first, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	second, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)

	assert.Equal(t, first.JobID, second.JobID)
	close(runner.block)
	waitFinished(t, f.svc, first.JobID)
	assert.Equal(t, 1, runner.callCount())
}

func TestCancelStopsARunningJobAndKeepsItCancelled(t *testing.T) {
	runner := &fakeRunner{answers: []string{`{"checks":[{"method":"GET","path":"/health"}]}`}, block: make(chan struct{})}
	f := newFixture(t, runner, nil)

	job, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return runner.callCount() == 1 }, time.Second, 5*time.Millisecond)

	f.svc.Cancel(job.JobID)
	got, err := f.svc.Get(job.JobID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, got.Status)

	time.Sleep(20 * time.Millisecond)
	got, err = f.svc.Get(job.JobID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, got.Status, "the run's late result must not overwrite the cancel")

	f.svc.Cancel(job.JobID)
	f.svc.Cancel(uuid.New())
}

func TestFinishedJobsAreEvictedAfterTheTTL(t *testing.T) {
	runner := &fakeRunner{answers: []string{`{"checks":[{"method":"GET","path":"/health"}]}`}}
	f := newFixture(t, runner, nil)

	job, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	waitFinished(t, f.svc, job.JobID)

	*f.now = f.now.Add(DefaultJobTTL + time.Minute)
	_, err = f.svc.Get(job.JobID)
	assert.ErrorIs(t, err, ErrJobNotFound)
}

func TestGenerateRefusesWithoutARunnableReleaseEngineer(t *testing.T) {
	disabled := []domain.Agent{{ID: uuid.New(), Name: "release-engineer", CatalogSlug: ReleaseEngineerSlug, Enabled: false, ProviderType: "claude_code"}}
	f := newFixture(t, &fakeRunner{}, disabled)

	_, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	assert.ErrorIs(t, err, ErrNoAgent)
}

func TestGenerateUnknownComponentIsNotFound(t *testing.T) {
	f := newFixture(t, &fakeRunner{}, nil)
	_, err := f.svc.Generate(context.Background(), uuid.New(), nil)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestGenerateTimesOut(t *testing.T) {
	runner := &fakeRunner{block: make(chan struct{})}
	f := newFixture(t, runner, nil)
	f.svc.deps.RunTimeout = 20 * time.Millisecond

	job, err := f.svc.Generate(context.Background(), f.comp.ID, nil)
	require.NoError(t, err)
	done := waitFinished(t, f.svc, job.JobID)

	assert.Equal(t, StatusFailed, done.Status)
	assert.Contains(t, done.Error, "did not finish")
}

func TestParseChecksToleratesDrift(t *testing.T) {
	for name, raw := range map[string]string{
		"object":       `{"checks":[{"method":"GET","path":"/"}]}`,
		"fenced":       "```json\n{\"checks\":[{\"method\":\"GET\",\"path\":\"/\"}]}\n```",
		"prose around": "Sure. {\"checks\":[{\"method\":\"GET\",\"path\":\"/\"}]} Done.",
		"bare array":   `[{"method":"GET","path":"/"}]`,
	} {
		checks, err := parseChecks(raw)
		require.NoError(t, err, name)
		require.Len(t, checks, 1, name)
	}
	_, err := parseChecks(`{"suggestions":[]}`)
	assert.Error(t, err)
	_, err = parseChecks("")
	assert.Error(t, err)
}

func TestFilterChecksCapsAndStripsContainsFromHead(t *testing.T) {
	proposed := []domain.SmokeCheck{
		{Method: "HEAD", Path: "/", Contains: "x"},
		{Method: "GET", Path: "/a"},
		{Method: "GET", Path: "/b"},
	}
	kept, dropped := filterChecks(proposed, nil, 2)
	require.Len(t, kept, 2)
	assert.Empty(t, kept[0].Contains)
	assert.Equal(t, 1, dropped)
}
