package smokegen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	// ReleaseEngineerSlug is the catalog agent that owns delivery and
	// verification, so it is the one asked what production should answer.
	ReleaseEngineerSlug = "release-engineer"

	DefaultRunTimeout = 5 * time.Minute
	DefaultJobTTL     = 30 * time.Minute
	maxSuggestions    = 8
	maxSessionTurns   = 40
)

var (
	ErrNoAgent     = errors.New("the release-engineer agent has no runnable runtime — connect an agent CLI or an API provider and enable the agent")
	ErrJobNotFound = errors.New("smoke-check generation not found")
)

type ComponentReader interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
}

type RepositoryReader interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type AgentLister interface {
	ListAgents(ctx context.Context) ([]domain.Agent, error)
}

type AgentRunner interface {
	Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...agent.RunOption) (domain.AgentResponse, error)
}

type SmokeTester interface {
	TestSmoke(ctx context.Context, componentID uuid.UUID, checks []domain.SmokeCheck) (string, []domain.SmokeResult, error)
}

// BriefFunc renders the component-scoped project brief; a func rather than an
// interface so this package need not import projectmodel for its scope type.
type BriefFunc func(ctx context.Context, repositoryID, componentID uuid.UUID) (string, error)

type Deps struct {
	Components ComponentReader
	Repos      RepositoryReader
	Agents     AgentLister
	Runner     AgentRunner
	Smoke      SmokeTester
	Brief      BriefFunc
	// Background is the process-lifetime context runs hang off: a run
	// outlives the POST that started it, and must still stop on shutdown.
	Background context.Context
	Now        func() time.Time
	RunTimeout time.Duration
	JobTTL     time.Duration
}

type entry struct {
	job    Job
	cancel context.CancelFunc
}

// Service runs smoke-check generations and keeps their jobs in memory: one
// local process, and a job is only worth anything to the dialog that started
// it, so nothing is persisted.
type Service struct {
	deps Deps

	mu          sync.Mutex
	jobs        map[uuid.UUID]*entry
	byComponent map[uuid.UUID]uuid.UUID
}

func New(deps Deps) *Service {
	if deps.Background == nil {
		deps.Background = context.Background()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.RunTimeout <= 0 {
		deps.RunTimeout = DefaultRunTimeout
	}
	if deps.JobTTL <= 0 {
		deps.JobTTL = DefaultJobTTL
	}
	return &Service{deps: deps, jobs: map[uuid.UUID]*entry{}, byComponent: map[uuid.UUID]uuid.UUID{}}
}

// Generate starts a run for componentID, or returns the one already running
// for it — two clicks must not become two agent sessions.
func (s *Service) Generate(ctx context.Context, componentID uuid.UUID, existing []domain.SmokeCheck) (Job, error) {
	s.mu.Lock()
	s.evictLocked()
	if id, ok := s.byComponent[componentID]; ok {
		if e, ok := s.jobs[id]; ok && e.job.Status == StatusRunning {
			job := e.job
			s.mu.Unlock()
			return job, nil
		}
	}
	s.mu.Unlock()

	comp, err := s.deps.Components.GetComponent(ctx, componentID)
	if err != nil {
		return Job{}, err
	}
	repo, err := s.deps.Repos.Get(ctx, comp.RepositoryID)
	if err != nil {
		return Job{}, err
	}
	agentRec, err := s.releaseEngineer(ctx)
	if err != nil {
		return Job{}, err
	}

	normalized := make([]domain.SmokeCheck, 0, len(existing))
	for _, c := range existing {
		normalized = append(normalized, c.Normalized())
	}

	runCtx, cancel := context.WithTimeout(s.deps.Background, s.deps.RunTimeout)
	job := Job{
		JobID:       uuid.New(),
		ComponentID: componentID,
		Status:      StatusRunning,
		AgentName:   agentRec.Name,
		StartedAt:   s.deps.Now(),
		Checks:      []domain.SmokeCheck{},
		Results:     []domain.SmokeResult{},
	}

	s.mu.Lock()
	if id, ok := s.byComponent[componentID]; ok {
		if e, ok := s.jobs[id]; ok && e.job.Status == StatusRunning {
			running := e.job
			s.mu.Unlock()
			cancel()
			return running, nil
		}
	}
	s.jobs[job.JobID] = &entry{job: job, cancel: cancel}
	s.byComponent[componentID] = job.JobID
	s.mu.Unlock()

	go s.run(runCtx, cancel, job.JobID, comp, repo, agentRec, normalized)
	return job, nil
}

func (s *Service) Get(jobID uuid.UUID) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked()
	e, ok := s.jobs[jobID]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	return e.job, nil
}

// Cancel stops a running job; cancelling a finished or unknown one is a no-op
// so a dialog closing late never errors.
func (s *Service) Cancel(jobID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.jobs[jobID]
	if !ok || e.job.Status != StatusRunning {
		return
	}
	now := s.deps.Now()
	e.job.Status = StatusCancelled
	e.job.FinishedAt = &now
	e.cancel()
}

func (s *Service) evictLocked() {
	cutoff := s.deps.Now().Add(-s.deps.JobTTL)
	for id, e := range s.jobs {
		if e.job.FinishedAt != nil && e.job.FinishedAt.Before(cutoff) {
			delete(s.jobs, id)
			if s.byComponent[e.job.ComponentID] == id {
				delete(s.byComponent, e.job.ComponentID)
			}
		}
	}
}

func (s *Service) releaseEngineer(ctx context.Context) (domain.Agent, error) {
	agents, err := s.deps.Agents.ListAgents(ctx)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("list agents: %w", err)
	}
	var byName *domain.Agent
	for i := range agents {
		a := agents[i]
		if !a.Enabled || a.ProviderType == "" {
			continue
		}
		if a.CatalogSlug == ReleaseEngineerSlug {
			return a, nil
		}
		if byName == nil && strings.EqualFold(a.Name, ReleaseEngineerSlug) {
			byName = &agents[i]
		}
	}
	if byName != nil {
		return *byName, nil
	}
	return domain.Agent{}, ErrNoAgent
}

// finish records the outcome unless the job was cancelled first: a DELETE
// answers the dialog immediately, and the run's late result must not undo it.
func (s *Service) finish(jobID uuid.UUID, update func(*Job)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.jobs[jobID]
	if !ok || e.job.Status != StatusRunning {
		return
	}
	update(&e.job)
	now := s.deps.Now()
	e.job.FinishedAt = &now
}

func (s *Service) fail(jobID uuid.UUID, msg string) {
	s.finish(jobID, func(j *Job) {
		j.Status = StatusFailed
		j.Error = msg
	})
}

func (s *Service) run(ctx context.Context, cancel context.CancelFunc, jobID uuid.UUID, comp domain.Component, repo domain.Repository, agentRec domain.Agent, existing []domain.SmokeCheck) {
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("job_id", jobID.String()).Msg("smokegen: run panicked")
			s.fail(jobID, "smoke-check generation crashed")
		}
	}()

	brief := ""
	if s.deps.Brief != nil {
		b, err := s.deps.Brief(ctx, repo.ID, comp.ID)
		if err != nil {
			log.Warn().Err(err).Str("component_id", comp.ID.String()).Msg("smokegen: brief unavailable; prompting without it")
		} else {
			brief = b
		}
	}

	proposed, err := s.propose(ctx, comp, repo, agentRec, brief, existing)
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			s.fail(jobID, fmt.Sprintf("the agent did not finish within %s", s.deps.RunTimeout))
		case ctx.Err() != nil:
		default:
			s.fail(jobID, err.Error())
		}
		return
	}

	limit := maxSuggestions
	if room := domain.MaxSmokeChecks - len(existing); room < limit {
		limit = room
	}
	kept, dropped := filterChecks(proposed, existing, limit)
	if len(kept) == 0 {
		s.finish(jobID, func(j *Job) {
			j.Status = StatusDone
			j.Dropped = dropped
			j.Error = "the agent proposed no usable smoke checks"
		})
		return
	}

	baseURL, results, testErr := s.deps.Smoke.TestSmoke(ctx, comp.ID, kept)
	if testErr != nil {
		log.Warn().Err(testErr).Str("component_id", comp.ID.String()).Msg("smokegen: test-running the suggestions failed")
		results = nil
	}
	if len(results) != len(kept) {
		results = []domain.SmokeResult{}
	}
	s.finish(jobID, func(j *Job) {
		j.Status = StatusDone
		j.Checks = kept
		j.Results = results
		j.BaseURL = baseURL
		j.Dropped = dropped
		if testErr != nil {
			j.Error = "could not test the suggestions: " + testErr.Error()
		}
	})
}

// readOnlyTools keeps the session to reading the repository: the agent runs
// in the repository's own checkout, so anything that can write or run a
// command would be touching the user's working tree.
var readOnlyTools = []string{"read_file", "grep_code", "glob", "codebase_search"}

func (s *Service) propose(ctx context.Context, comp domain.Component, repo domain.Repository, agentRec domain.Agent, brief string, existing []domain.SmokeCheck) ([]domain.SmokeCheck, error) {
	runCtx := registry.ContextWithWorkspaceDir(ctx, repo.RootPath)
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: systemPrompt},
		{Role: domain.RoleUser, Content: userPrompt(comp, repo, brief, existing)},
	}
	label := comp.Path
	if label == "." {
		label = repo.Name
	}
	call := func(msgs []domain.Message) (string, error) {
		resp, err := s.deps.Runner.Run(runCtx, msgs, agentRec.Model, agentRec.ProviderType,
			domain.ToolPolicy{AllowTools: readOnlyTools},
			agent.WithSessionLimits(maxSessionTurns, agentRec.Effort),
			agent.WithCLILabel("smoke-checks:"+label, "draft smoke checks"))
		if err != nil {
			return "", err
		}
		return resp.Message.Content, nil
	}

	raw, err := call(messages)
	if err != nil {
		return nil, fmt.Errorf("the agent run failed: %w", err)
	}
	checks, parseErr := parseChecks(raw)
	if parseErr == nil {
		return checks, nil
	}
	retry := append(messages,
		domain.Message{Role: domain.RoleAssistant, Content: raw},
		domain.Message{Role: domain.RoleUser, Content: "That was not a valid JSON object (" + parseErr.Error() + "). Reply again with ONLY the JSON object {\"checks\":[...]}, no prose, no code fences."},
	)
	raw2, err := call(retry)
	if err != nil {
		return nil, fmt.Errorf("the agent run failed: %w", err)
	}
	checks, parseErr = parseChecks(raw2)
	if parseErr != nil {
		return nil, fmt.Errorf("the agent's answer was not valid JSON: %w", parseErr)
	}
	return checks, nil
}
