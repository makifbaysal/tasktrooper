// Package audience is TaskTrooper's own audience research. An audience is a
// set of synthetic personas an LLM invents from a project's brief and its
// current state; a study asks every persona the same questions (would you pay,
// at what price, what is missing) and a synthesis turns the answers into a
// report; a persona can also be talked to one to one. Every LLM call runs on
// the user's own agent runtime — a coding agent CLI or an API provider they
// configured — and everything is kept in the local database; nothing is sent
// to a TaskTrooper service. The people are invented: the report says so.
package audience

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
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var (
	// ErrNoRuntime: no enabled agent has an LLM runtime to run the calls on.
	ErrNoRuntime = errors.New("audience research needs an agent with a runtime — connect an agent CLI or an API provider and enable an agent")
	// ErrCallFailed: the model could not be reached or did not answer.
	ErrCallFailed = errors.New("the model call failed")
	// ErrScriptNotApproved: the study's current interview script has not been
	// approved by the person; nothing runs until it is.
	ErrScriptNotApproved = errors.New("the interview script has not been approved")
	// ErrScriptChanged: the script changed after the version being approved was shown.
	ErrScriptChanged = errors.New("the interview script changed since it was shown")
	// ErrBusy: the audience or study is already being worked on.
	ErrBusy = errors.New("this is already running")
)

const (
	DefaultConcurrency = 3
	DefaultCallTimeout = 8 * time.Minute

	DefaultPersonas = 12
	MaxPersonas     = 40
	// personasPerCall is how many people one generation call writes; more
	// than this and the later ones turn into copies of the first.
	personasPerCall = 8

	maxSegments       = 8
	maxExtraQuestions = 5
	maxPricePoints    = 6
	maxQuestionLen    = 400
	maxFieldLen       = 4000
	maxContextLen     = 6000
	maxThreadTurns    = 30

	// PreferredAgentSlug is the catalog agent the calls run on when it can:
	// the one the person already talks to.
	PreferredAgentSlug = "product-manager"
)

type AgentLister interface {
	ListAgents(ctx context.Context) ([]domain.Agent, error)
}

type AgentRunner interface {
	Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...agent.RunOption) (domain.AgentResponse, error)
}

type RepositoryLister interface {
	List(ctx context.Context) ([]domain.Repository, error)
}

// BriefFunc renders the repository overview (what it is, its components,
// what it talks to) the personas are grounded in.
type BriefFunc func(ctx context.Context, repositoryID uuid.UUID) (string, error)

type Deps struct {
	Store  port.AudienceStore
	Agents AgentLister
	// Runner runs a prompt on a host-executed agent CLI (and on API
	// providers through the agent loop).
	Runner AgentRunner
	// LLM answers a prompt on an API provider directly.
	LLM   port.LLMClient
	Repos RepositoryLister
	Brief BriefFunc
	// Pages reads a landing page from its URL; nil means only pasted text.
	Pages PageReader
	// Background is the process lifetime: a build or a study outlives the
	// request that started it, keeps its context values and stops with it.
	Background  context.Context
	Concurrency int
	CallTimeout time.Duration
	Now         func() time.Time
}

type Service struct {
	d   Deps
	sem chan struct{}

	mu   sync.Mutex
	runs map[uuid.UUID]context.CancelFunc
}

func NewService(d Deps) *Service {
	if d.Background == nil {
		d.Background = context.Background()
	}
	if d.Concurrency <= 0 {
		d.Concurrency = DefaultConcurrency
	}
	if d.CallTimeout <= 0 {
		d.CallTimeout = DefaultCallTimeout
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d, sem: make(chan struct{}, d.Concurrency), runs: map[uuid.UUID]context.CancelFunc{}}
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", domain.ErrAudienceInvalid, fmt.Sprintf(format, args...))
}

// RecoverInterrupted fails what a dead process left half done. Run it once at
// boot, before anything new can start.
func (s *Service) RecoverInterrupted(ctx context.Context) {
	a, st, err := s.d.Store.FailInterrupted(ctx, "TaskTrooper stopped while this was running. Start it again.")
	if err != nil {
		log.Warn().Err(err).Msg("audience: could not fail interrupted work")
		return
	}
	if a+st > 0 {
		log.Info().Int("audiences", a).Int("studies", st).Msg("audience: failed work a stopped process left running")
	}
}

// ---- runtime

// Runtime is the agent the calls run on and whether one exists.
type Runtime struct {
	Ready    bool   `json:"ready"`
	Agent    string `json:"agent,omitempty"`
	Provider string `json:"provider,omitempty"`
}

func (s *Service) agentFor(ctx context.Context) (domain.Agent, error) {
	agents, err := s.d.Agents.ListAgents(ctx)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("list agents: %w", err)
	}
	var first *domain.Agent
	for i := range agents {
		a := agents[i]
		if !a.Enabled || a.ProviderType == "" {
			continue
		}
		if a.CatalogSlug == PreferredAgentSlug {
			return a, nil
		}
		if first == nil {
			first = &agents[i]
		}
	}
	if first != nil {
		return *first, nil
	}
	return domain.Agent{}, ErrNoRuntime
}

func (s *Service) Runtime(ctx context.Context) (Runtime, error) {
	a, err := s.agentFor(ctx)
	if errors.Is(err, ErrNoRuntime) {
		return Runtime{}, nil
	}
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{Ready: true, Agent: a.Name, Provider: string(a.ProviderType)}, nil
}

// ---- runs

// begin registers a cancellable background run for id and answers its context;
// a second run for the same id is refused.
func (s *Service) begin(ctx context.Context, id uuid.UUID) (context.Context, func(), error) {
	s.mu.Lock()
	if _, busy := s.runs[id]; busy {
		s.mu.Unlock()
		return nil, nil, ErrBusy
	}
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(s.d.Background, cancelRun)
	s.runs[id] = cancelRun
	s.mu.Unlock()
	return runCtx, func() {
		stop()
		cancelRun()
		s.mu.Lock()
		delete(s.runs, id)
		s.mu.Unlock()
	}, nil
}

func (s *Service) cancelRun(id uuid.UUID) bool {
	s.mu.Lock()
	cancel, ok := s.runs[id]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func (s *Service) running(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.runs[id]
	return ok
}

// ---- brief handling

// NormalizeBrief trims every field, caps what a prompt would otherwise carry
// unbounded and drops empty segments.
func NormalizeBrief(b domain.AudienceBrief) (domain.AudienceBrief, error) {
	b.Product = clip(strings.TrimSpace(b.Product), maxFieldLen)
	if b.Product == "" {
		return b, invalid("the brief needs a description of the product and its current state")
	}
	b.Locations = clip(strings.TrimSpace(b.Locations), 400)
	b.CurrentSolutions = clip(strings.TrimSpace(b.CurrentSolutions), 800)
	b.Pricing = clip(strings.TrimSpace(b.Pricing), 400)
	b.Topic = clip(strings.TrimSpace(b.Topic), 600)
	b.Language = clip(strings.TrimSpace(b.Language), 40)
	var segments []domain.AudienceSegment
	for _, seg := range b.Segments {
		seg.Name = clip(strings.TrimSpace(seg.Name), 100)
		seg.Description = clip(strings.TrimSpace(seg.Description), 400)
		if seg.Name == "" {
			continue
		}
		if seg.Count < 0 {
			seg.Count = 0
		}
		segments = append(segments, seg)
	}
	if len(segments) > maxSegments {
		return b, invalid("at most %d segments", maxSegments)
	}
	b.Segments = segments
	return b, nil
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func clampTarget(n int) int {
	switch {
	case n <= 0:
		return DefaultPersonas
	case n > MaxPersonas:
		return MaxPersonas
	default:
		return n
	}
}

// resolveRepository turns a name or an id into the repository's id.
func (s *Service) resolveRepository(ctx context.Context, ref string) (*uuid.UUID, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if id, err := uuid.Parse(ref); err == nil {
		return &id, nil
	}
	if s.d.Repos == nil {
		return nil, invalid("repository %q cannot be resolved here", ref)
	}
	repos, err := s.d.Repos.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		if strings.EqualFold(r.Name, ref) {
			id := r.ID
			return &id, nil
		}
		names = append(names, r.Name)
	}
	return nil, invalid("unknown repository %q; registered repositories: %s", ref, strings.Join(names, ", "))
}

// projectContext is what the project says about itself right now.
func (s *Service) projectContext(ctx context.Context, repositoryID *uuid.UUID) string {
	if repositoryID == nil || s.d.Brief == nil {
		return ""
	}
	text, err := s.d.Brief(ctx, *repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("audience: project brief unavailable; grounding on the description alone")
		return ""
	}
	return clip(strings.TrimSpace(text), maxContextLen)
}

// ---- audiences

type CreateAudienceInput struct {
	Name          string
	Repository    string
	Brief         domain.AudienceBrief
	PersonaTarget int
}

func (s *Service) CreateAudience(ctx context.Context, in CreateAudienceInput) (domain.Audience, error) {
	brief, err := NormalizeBrief(in.Brief)
	if err != nil {
		return domain.Audience{}, err
	}
	name := clip(strings.TrimSpace(in.Name), 120)
	if name == "" {
		return domain.Audience{}, invalid("an audience needs a name")
	}
	repoID, err := s.resolveRepository(ctx, in.Repository)
	if err != nil {
		return domain.Audience{}, err
	}
	return s.d.Store.CreateAudience(ctx, domain.Audience{
		Name: name, RepositoryID: repoID, Status: domain.AudienceDraft, Brief: brief, PersonaTarget: clampTarget(in.PersonaTarget),
	})
}

// UpdateAudienceInput changes the brief of an audience; nil leaves a field.
type UpdateAudienceInput struct {
	Name          *string
	Repository    *string
	Brief         *domain.AudienceBrief
	PersonaTarget *int
}

// UpdateAudience edits the brief. The personas already written stay until the
// audience is built again, so a change is never half applied.
func (s *Service) UpdateAudience(ctx context.Context, id uuid.UUID, in UpdateAudienceInput) (domain.Audience, error) {
	a, err := s.d.Store.GetAudience(ctx, id)
	if err != nil {
		return domain.Audience{}, err
	}
	if a.Status == domain.AudienceBuilding {
		return domain.Audience{}, invalid("the audience is being built; wait for it or cancel the build")
	}
	if in.Name != nil {
		name := clip(strings.TrimSpace(*in.Name), 120)
		if name == "" {
			return domain.Audience{}, invalid("an audience needs a name")
		}
		a.Name = name
	}
	if in.Repository != nil {
		repoID, err := s.resolveRepository(ctx, *in.Repository)
		if err != nil {
			return domain.Audience{}, err
		}
		a.RepositoryID = repoID
	}
	if in.Brief != nil {
		brief, err := NormalizeBrief(*in.Brief)
		if err != nil {
			return domain.Audience{}, err
		}
		a.Brief = brief
	}
	if in.PersonaTarget != nil {
		a.PersonaTarget = clampTarget(*in.PersonaTarget)
	}
	if err := s.d.Store.SaveAudience(ctx, a); err != nil {
		return domain.Audience{}, err
	}
	return s.d.Store.GetAudience(ctx, id)
}

type AudienceDetail struct {
	domain.Audience
	Personas []domain.Persona `json:"personas"`
	Studies  []domain.Study   `json:"studies"`
	// BuildCalls is how many LLM calls building the audience again costs.
	BuildCalls int `json:"build_calls"`
}

func (s *Service) ListAudiences(ctx context.Context) ([]domain.Audience, error) {
	return s.d.Store.ListAudiences(ctx)
}

func (s *Service) Audience(ctx context.Context, id uuid.UUID) (AudienceDetail, error) {
	a, err := s.d.Store.GetAudience(ctx, id)
	if err != nil {
		return AudienceDetail{}, err
	}
	personas, err := s.d.Store.ListPersonas(ctx, id)
	if err != nil {
		return AudienceDetail{}, err
	}
	studies, err := s.d.Store.ListStudies(ctx, &id)
	if err != nil {
		return AudienceDetail{}, err
	}
	for i := range studies {
		studies[i].Report = nil // the list carries status, the study carries the report
	}
	return AudienceDetail{Audience: a, Personas: personas, Studies: studies, BuildCalls: BuildCalls(a.PersonaTarget)}, nil
}

// BuildCalls is the LLM calls it takes to write n people.
func BuildCalls(n int) int {
	n = clampTarget(n)
	return (n + personasPerCall - 1) / personasPerCall
}

func (s *Service) DeleteAudience(ctx context.Context, id uuid.UUID) error {
	if s.running(id) {
		return ErrBusy
	}
	studies, err := s.d.Store.ListStudies(ctx, &id)
	if err != nil {
		return err
	}
	for _, st := range studies {
		if s.running(st.ID) {
			return ErrBusy
		}
	}
	return s.d.Store.DeleteAudience(ctx, id)
}
