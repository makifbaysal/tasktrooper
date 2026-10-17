// Package opsai is the release engineer's observability work: suggestion
// jobs (error definitions from recent error groups or from the code's own
// error types, signal bindings from the data sources' inventories), the
// "enable observability" board task, and the readiness summary behind it.
// A job is one ordinary chat turn with the agent holding the release role
// (session.Service.SendMessage, so an agent CLI runs it on this machine); the
// propose_* tools it calls store their suggestions and record them on the
// job through the chat session on the tool call's context.
package opsai

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultConcurrency = 2
	jobTimeout         = 30 * time.Minute
	// jobQueueLimit is how long a job may wait for a free slot before it is
	// failed instead of started.
	jobQueueLimit = 30 * time.Minute
	// staleGrace is how far past its own limits a running row must be before
	// a new request fails it as stale: a stuck job never blocks its kind.
	staleGrace    = 5 * time.Minute
	maxSummaryLen = 2000
	maxErrorLen   = 500
	maxTitleLen   = 120
)

// jobTools is each kind's tool ceiling: the job's turn gets only these of the
// release engineer's own tools, whatever its role grants elsewhere — reads,
// the code, skills and the job's own propose_* tools; no shell, no network
// fetch, no writes, no merge, deploy or rollback, no memory or skill edits.
var jobTools = map[domain.AIJobKind][]string{
	domain.AIJobDefineErrors: append([]string{"list_error_groups", "propose_error_definitions"}, codeReadTools...),
	domain.AIJobErrorCatalog: append([]string{"propose_error_definitions"}, codeReadTools...),
	domain.AIJobMapSignals:   append([]string{"list_data_source_metrics", "propose_signal_binding", "propose_alert_rule"}, codeReadTools...),
}

var codeReadTools = []string{"codebase_search", "grep_code", "read_file", "get_repo_tree", "get_project_brief", "load_skill"}

// JobTools is the tool ceiling of a job kind.
func JobTools(kind domain.AIJobKind) []string { return slices.Clone(jobTools[kind]) }

// Sessions is session.Service.
type Sessions interface {
	Create(ctx context.Context, req domain.CreateSessionRequest) (domain.Session, error)
	SendMessage(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy) (domain.AgentResponse, error)
	CancelSession(ctx context.Context, sessionID uuid.UUID, reason string) (bool, error)
}

type Roles interface {
	RoleByKey(ctx context.Context, key string) (domain.AgentRole, error)
	AgentForRole(ctx context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error)
}

type Agents interface {
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
}

type Components interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
	ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error)
}

type Repositories interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// Signals is signals.Service.
type Signals interface {
	List(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ComponentSignals, error)
}

type Environments interface {
	ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error)
}

// Links is where the readiness summary learns which observability vendors a
// component is linked to.
type Links interface {
	ListLinks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error)
	ListResources(ctx context.Context, ids []uuid.UUID) ([]domain.SystemResource, error)
}

type TaskBoard interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	ListTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error)
}

// Deps: without Sessions the jobs are refused (ErrUnavailable); without
// Tasks so is enable. Signals, Environments, Links and Agents are optional.
type Deps struct {
	Jobs         port.AIJobStore
	Sessions     Sessions
	Roles        Roles
	Agents       Agents
	Components   Components
	Repositories Repositories
	Signals      Signals
	Environments Environments
	Links        Links
	Tasks        TaskBoard
	// Policy is the server's default tool policy, the base an HTTP chat turn
	// starts from before the agent's own policy is merged in.
	Policy domain.ToolPolicy
}

type Service struct {
	jobs         port.AIJobStore
	sessions     Sessions
	roles        Roles
	agents       Agents
	components   Components
	repos        Repositories
	signals      Signals
	environments Environments
	links        Links
	tasks        TaskBoard
	policy       domain.ToolPolicy

	now  func() time.Time
	base context.Context
	wg   sync.WaitGroup
	// sync, when set, runs a job inside the call that started it (tests).
	sync bool

	// owner marks the jobs this process created, so Start fails only the
	// ones another process left running.
	owner            string
	interruptedAfter time.Duration
	timeout          time.Duration
	queueLimit       time.Duration

	scope func(ctx context.Context) string
	semMu sync.Mutex
	sems  map[string]chan struct{}
}

type Option func(*Service)

func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithSynchronousJobs runs each job to its end before the call that started
// it returns.
func WithSynchronousJobs() Option { return func(s *Service) { s.sync = true } }

// WithInterruptedAfter: Start fails another process's running jobs only once
// they are this old, for a deployment where that process may still be alive.
// Zero (the default) fails every one: on this machine there is no other.
func WithInterruptedAfter(d time.Duration) Option {
	return func(s *Service) { s.interruptedAfter = max(d, 0) }
}

// WithJobLimits replaces the 30-minute run and queue limits (tests).
func WithJobLimits(run, queue time.Duration) Option {
	return func(s *Service) {
		if run > 0 {
			s.timeout = run
		}
		if queue > 0 {
			s.queueLimit = queue
		}
	}
}

// SetScope bounds the jobs running at once per scope(ctx) of the request that
// started them rather than per process. Without it there is one scope. Call
// it before serving.
func (s *Service) SetScope(scope func(ctx context.Context) string) { s.scope = scope }

func NewService(d Deps, opts ...Option) (*Service, error) {
	if d.Jobs == nil || d.Components == nil || d.Repositories == nil || d.Roles == nil {
		return nil, errors.New("opsai: jobs, components, repositories and roles are required")
	}
	s := &Service{
		jobs:         d.Jobs,
		sessions:     d.Sessions,
		roles:        d.Roles,
		agents:       d.Agents,
		components:   d.Components,
		repos:        d.Repositories,
		signals:      d.Signals,
		environments: d.Environments,
		links:        d.Links,
		tasks:        d.Tasks,
		policy:       d.Policy,
		now:          func() time.Time { return time.Now().UTC() },
		base:         context.Background(),
		owner:        uuid.NewString(),
		timeout:      jobTimeout,
		queueLimit:   jobQueueLimit,
		sems:         map[string]chan struct{}{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Start fails the jobs a previous process left running: their chat turn
// died with it. Jobs started afterwards run under ctx.
func (s *Service) Start(ctx context.Context) {
	s.base = ctx
	if n, err := s.jobs.FailRunningAIJobs(ctx, s.owner, s.now().Add(-s.interruptedAfter), interruptedReason); err != nil {
		log.Warn().Err(err).Msg("opsai: failing interrupted jobs failed")
	} else if n > 0 {
		log.Info().Int64("jobs", n).Msg("opsai: failed jobs interrupted by a restart")
	}
}

// Wait returns once every job started so far has finished.
func (s *Service) Wait() { s.wg.Wait() }

const (
	interruptedReason = "interrupted by a restart"
	staleReason       = "stopped answering; failed so a new job can start"
	shutdownReason    = "the server stopped before the job could start"
	cancelReason      = "the AI job ran past its time limit"
)

func (s *Service) Job(ctx context.Context, id uuid.UUID) (domain.AIJob, error) {
	return s.jobs.GetAIJob(ctx, id)
}

// SessionJob is the job whose chat the tool call's context carries.
func (s *Service) SessionJob(ctx context.Context) (domain.AIJob, bool) {
	sid := registry.SessionIDFromContext(ctx)
	if sid == uuid.Nil {
		return domain.AIJob{}, false
	}
	job, err := s.jobs.GetAIJobBySession(ctx, sid)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			log.Warn().Err(err).Str("session_id", sid.String()).Msg("opsai: reading the chat's job failed")
		}
		return domain.AIJob{}, false
	}
	return job, true
}

// Record adds what a tool stored to the job whose chat this is, once and
// only while the job runs: outside a job, or in a job's chat after it ended,
// it is no change.
func (s *Service) Record(ctx context.Context, item domain.AIJobItem) {
	job, ok := s.SessionJob(ctx)
	if !ok || job.Status != domain.AIJobRunning {
		return
	}
	if err := s.jobs.AppendAIJobItem(ctx, job.ID, item); err != nil {
		log.Warn().Err(err).Str("job_id", job.ID.String()).Msg("opsai: recording a job item failed")
	}
}

// SuggestErrorsRequest is the body of POST /v1/error-definitions/suggest.
// Mode "code" proposes definitions from the code's own error types instead
// of from recent error groups.
type SuggestErrorsRequest struct {
	RepositoryID string `json:"repository_id"`
	ComponentID  string `json:"component_id"`
	Environment  string `json:"environment"`
	Mode         string `json:"mode"`
}

const (
	ModeErrors = "errors"
	ModeCode   = "code"
)

// SuggestErrors starts (or answers the running) job of its kind for the
// repository or component.
func (s *Service) SuggestErrors(ctx context.Context, req SuggestErrorsRequest) (domain.AIJob, error) {
	fe := FieldErrors{}
	repoID, err := uuid.Parse(strings.TrimSpace(req.RepositoryID))
	if err != nil {
		fe["repository_id"] = datasource.FieldRequired
		if strings.TrimSpace(req.RepositoryID) != "" {
			fe["repository_id"] = datasource.FieldInvalid
		}
	}
	var compID *uuid.UUID
	if raw := strings.TrimSpace(req.ComponentID); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			fe["component_id"] = datasource.FieldInvalid
		} else {
			compID = &id
		}
	}
	env := environmentOf(req.Environment, fe)
	kind := domain.AIJobDefineErrors
	switch strings.TrimSpace(req.Mode) {
	case "", ModeErrors:
	case ModeCode:
		kind = domain.AIJobErrorCatalog
	default:
		fe["mode"] = datasource.FieldInvalidOption
	}
	if len(fe) > 0 {
		return domain.AIJob{}, fe
	}
	return s.start(ctx, kind, repoID, compID, env)
}

// SuggestSignals starts (or answers the running) signal mapping job for one
// component environment.
func (s *Service) SuggestSignals(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.AIJob, error) {
	if !env.Valid() {
		return domain.AIJob{}, FieldErrors{"env": datasource.FieldInvalid}
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return domain.AIJob{}, err
	}
	return s.start(ctx, domain.AIJobMapSignals, comp.RepositoryID, &comp.ID, env)
}

func environmentOf(raw string, fe FieldErrors) domain.DeployEnvironment {
	env := domain.DeployEnvironment(strings.TrimSpace(raw))
	if env == "" {
		return domain.EnvironmentProduction
	}
	if !env.Valid() {
		fe["environment"] = datasource.FieldInvalid
	}
	return env
}

type jobContext struct {
	job       domain.AIJob
	repo      domain.Repository
	component *domain.Component
	agentID   uuid.UUID
	actor     string
	scope     string
}

func (s *Service) start(ctx context.Context, kind domain.AIJobKind, repoID uuid.UUID, compID *uuid.UUID, env domain.DeployEnvironment) (domain.AIJob, error) {
	if s.sessions == nil {
		return domain.AIJob{}, ErrUnavailable
	}
	repo, err := s.repos.Get(ctx, repoID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return domain.AIJob{}, FieldErrors{"repository_id": datasource.FieldInvalid}
		}
		return domain.AIJob{}, err
	}
	var comp *domain.Component
	if compID != nil {
		c, err := s.components.GetComponent(ctx, *compID)
		switch {
		case errors.Is(err, port.ErrNotFound):
			return domain.AIJob{}, FieldErrors{"component_id": datasource.FieldInvalid}
		case err != nil:
			return domain.AIJob{}, err
		case c.RepositoryID != repo.ID:
			return domain.AIJob{}, FieldErrors{"component_id": datasource.FieldInvalidOption}
		}
		comp = &c
	}
	area := domain.RepoArea(repo.Kind, repo.SubProjects)
	if comp != nil {
		area = domain.ComponentArea(comp.Role.Get())
	}
	agentID, err := s.releaseEngineer(ctx, area)
	if err != nil {
		return domain.AIJob{}, err
	}
	now := s.now()
	if n, err := s.jobs.FailStaleAIJobs(ctx, now.Add(-(s.timeout + staleGrace)), now.Add(-(s.queueLimit + staleGrace)), staleReason); err != nil {
		return domain.AIJob{}, err
	} else if n > 0 {
		log.Warn().Int64("jobs", n).Msg("opsai: failed jobs that stopped answering")
	}
	job, created, err := s.jobs.CreateAIJob(ctx, domain.AIJob{Kind: kind, RepositoryID: repo.ID, ComponentID: compID, Environment: env, Owner: s.owner})
	if err != nil {
		return domain.AIJob{}, err
	}
	if !created {
		return job, nil
	}
	jc := jobContext{job: job, repo: repo, component: comp, agentID: agentID, actor: registry.ActorUserIDFromContext(ctx)}
	if s.scope != nil {
		jc.scope = s.scope(ctx)
	}
	if s.sync {
		s.run(jc)
		return s.jobs.GetAIJob(ctx, job.ID)
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(jc)
	}()
	return job, nil
}

// releaseEngineer is the enabled agent holding the release role for area.
func (s *Service) releaseEngineer(ctx context.Context, area string) (uuid.UUID, error) {
	role, err := s.roles.RoleByKey(ctx, domain.RoleKeyRelease)
	if err != nil {
		return uuid.Nil, ErrNoReleaseEngineer
	}
	for _, a := range append([]string{area}, domain.AreaFallbacks(area)...) {
		id, err := s.roles.AgentForRole(ctx, role.ID, a)
		if err != nil {
			return uuid.Nil, fmt.Errorf("resolve the release engineer: %w", err)
		}
		if id == nil {
			continue
		}
		if s.agents != nil {
			agent, err := s.agents.GetAgent(ctx, *id)
			if err != nil || !agent.Enabled {
				continue
			}
		}
		return *id, nil
	}
	return uuid.Nil, ErrNoReleaseEngineer
}

// slots is the scope's job semaphore.
func (s *Service) slots(scope string) chan struct{} {
	s.semMu.Lock()
	defer s.semMu.Unlock()
	sem, ok := s.sems[scope]
	if !ok {
		sem = make(chan struct{}, defaultConcurrency)
		s.sems[scope] = sem
	}
	return sem
}

// takeSlot waits for a free slot; a job that waits past the queue limit, or
// for a server that stops, is failed instead.
func (s *Service) takeSlot(jc jobContext) (release func(), ok bool) {
	sem := s.slots(jc.scope)
	wait := time.NewTimer(s.queueLimit)
	defer wait.Stop()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true
	case <-s.base.Done():
		s.finish(context.WithoutCancel(s.base), jc.job.ID, domain.AIJobFailed, "", shutdownReason)
	case <-wait.C:
		s.finish(s.base, jc.job.ID, domain.AIJobFailed, "", errQueuedTooLong.Error())
	}
	return nil, false
}

func (s *Service) run(jc jobContext) {
	release, ok := s.takeSlot(jc)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(s.base, s.timeout)
	defer cancel()
	finishCtx := context.WithoutCancel(ctx)
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("job_id", jc.job.ID.String()).Msg("opsai: job panicked")
			s.finish(finishCtx, jc.job.ID, domain.AIJobFailed, "", "the job failed unexpectedly")
		}
	}()

	started, err := s.jobs.StartAIJob(ctx, jc.job.ID)
	if err != nil {
		s.finish(finishCtx, jc.job.ID, domain.AIJobFailed, "", "start the job: "+err.Error())
		return
	}
	if !started {
		return
	}
	sess, err := s.sessions.Create(ctx, domain.CreateSessionRequest{
		Title:     domain.TruncateHead(s.jobTitle(jc), maxTitleLen),
		ProjectID: &jc.repo.ID,
		AgentID:   &jc.agentID,
	})
	if err != nil {
		s.finish(finishCtx, jc.job.ID, domain.AIJobFailed, "", "open the release engineer chat: "+err.Error())
		return
	}
	if err := s.jobs.SetAIJobSession(ctx, jc.job.ID, sess.ID); err != nil {
		s.finish(finishCtx, jc.job.ID, domain.AIJobFailed, "", "record the chat: "+err.Error())
		return
	}
	message, err := s.jobMessage(ctx, jc)
	if err != nil {
		s.finish(finishCtx, jc.job.ID, domain.AIJobFailed, "", err.Error())
		return
	}
	runCtx := ctx
	if jc.actor != "" {
		runCtx = registry.ContextWithActorUserID(runCtx, jc.actor)
	}
	// The chat turn outlives a cancelled context by design (a dropped
	// connection must not lose an answer), so only stopping the session ends
	// it: the time limit does that, then fails the job itself.
	var timedOut atomic.Bool
	limit := time.AfterFunc(s.timeout, func() {
		timedOut.Store(true)
		s.finish(finishCtx, jc.job.ID, domain.AIJobFailed, "", errTimedOut.Error())
		if _, err := s.sessions.CancelSession(finishCtx, sess.ID, cancelReason); err != nil {
			log.Warn().Err(err).Str("job_id", jc.job.ID.String()).Msg("opsai: stopping a job past its time limit failed")
		}
	})
	defer limit.Stop()
	noOrchestration := false
	resp, err := s.sessions.SendMessage(runCtx, sess.ID, domain.SessionMessageRequest{
		Role:        domain.RoleUser,
		Content:     message,
		Orchestrate: &noOrchestration,
		ToolCeiling: jobTools[jc.job.Kind],
		NoQuotaPark: true,
	}, s.policy)
	switch {
	case timedOut.Load():
	case err != nil:
		s.finish(finishCtx, jc.job.ID, domain.AIJobFailed, "", err.Error())
	case resp.Clarification != nil:
		s.finish(finishCtx, jc.job.ID, domain.AIJobFailed, strings.TrimSpace(resp.Message.Content), errStoppedOnQuestion.Error())
	default:
		s.finish(finishCtx, jc.job.ID, domain.AIJobDone, strings.TrimSpace(resp.Message.Content), "")
	}
}

// finish ends a running job; one already ended (failed past its time limit,
// or as stale) keeps that outcome.
func (s *Service) finish(ctx context.Context, id uuid.UUID, status domain.AIJobStatus, summary, errMsg string) {
	if _, err := s.jobs.FinishAIJob(ctx, id, status,
		domain.TruncateHead(summary, maxSummaryLen), domain.TruncateHead(strings.TrimSpace(errMsg), maxErrorLen)); err != nil && !errors.Is(err, port.ErrNotFound) {
		log.Warn().Err(err).Str("job_id", id.String()).Msg("opsai: finishing the job failed")
	}
}

func (s *Service) jobTitle(jc jobContext) string {
	scope := jc.repo.Name
	if jc.component != nil {
		scope += " · " + jc.component.DisplayName()
	}
	return jobTitleKey.Render(jobTitleInput{Kind: string(jc.job.Kind), Scope: scope, Environment: string(jc.job.Environment)})
}

func (s *Service) jobMessage(ctx context.Context, jc jobContext) (string, error) {
	comps, err := s.components.ListComponents(ctx, jc.repo.ID)
	if err != nil {
		return "", fmt.Errorf("list components: %w", err)
	}
	var paths []string
	for _, c := range comps {
		if c.Status != domain.ComponentStatusDismissed {
			paths = append(paths, fmt.Sprintf("%s (%s)", c.Path, c.Role.Get()))
		}
	}
	in := jobPromptInput{
		Repository:  jc.repo.Name,
		Environment: string(jc.job.Environment),
		Components:  paths,
		FromCode:    jc.job.Kind == domain.AIJobErrorCatalog,
	}
	if jc.component != nil {
		in.Component = jc.component.DisplayName()
		in.Path = jc.component.Path
		in.Role = string(jc.component.Role.Get())
	}
	if jc.job.Kind != domain.AIJobMapSignals {
		return strings.TrimSpace(defineErrorsKey.Render(in)), nil
	}
	if s.signals != nil && jc.component != nil {
		if list, err := s.signals.List(ctx, jc.component.ID, jc.job.Environment); err == nil {
			for _, sig := range list.Signals {
				if sig.Status == domain.SignalStateBound {
					continue
				}
				if sig.Golden {
					in.UnboundGolden = append(in.UnboundGolden, sig.Signal)
				} else {
					in.Unbound = append(in.Unbound, sig.Signal)
				}
			}
		}
	}
	return strings.TrimSpace(mapSignalsKey.Render(in)), nil
}
