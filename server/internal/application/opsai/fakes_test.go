package opsai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// memJobs is a stateful fake: the job the service creates is the one the
// tool calls of its chat find and append to.
type memJobs struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]domain.AIJob
}

var _ port.AIJobStore = (*memJobs)(nil)

func newMemJobs() *memJobs { return &memJobs{jobs: map[uuid.UUID]domain.AIJob{}} }

func (m *memJobs) CreateAIJob(_ context.Context, j domain.AIJob) (domain.AIJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j.ID, j.Status, j.Created, j.CreatedAt = uuid.New(), domain.AIJobRunning, []domain.AIJobItem{}, time.Now()
	m.jobs[j.ID] = j
	return j, nil
}

func (m *memJobs) GetAIJob(_ context.Context, id uuid.UUID) (domain.AIJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return domain.AIJob{}, port.ErrNotFound
	}
	return j, nil
}

func (m *memJobs) GetAIJobBySession(_ context.Context, sid uuid.UUID) (domain.AIJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.SessionID != nil && *j.SessionID == sid {
			return j, nil
		}
	}
	return domain.AIJob{}, port.ErrNotFound
}

func (m *memJobs) RunningAIJob(_ context.Context, kind domain.AIJobKind, repo uuid.UUID, comp *uuid.UUID, env domain.DeployEnvironment) (domain.AIJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		sameComp := (j.ComponentID == nil && comp == nil) || (j.ComponentID != nil && comp != nil && *j.ComponentID == *comp)
		if j.Status == domain.AIJobRunning && j.Kind == kind && j.RepositoryID == repo && sameComp && j.Environment == env {
			return j, nil
		}
	}
	return domain.AIJob{}, port.ErrNotFound
}

func (m *memJobs) SetAIJobSession(_ context.Context, id, sid uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	j.SessionID = &sid
	m.jobs[id] = j
	return nil
}

func (m *memJobs) AppendAIJobItem(_ context.Context, id uuid.UUID, item domain.AIJobItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	j.Created = append(j.Created, item)
	m.jobs[id] = j
	return nil
}

func (m *memJobs) FinishAIJob(_ context.Context, id uuid.UUID, status domain.AIJobStatus, summary, errMsg string) (domain.AIJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	now := time.Now()
	j.Status, j.ResultSummary, j.Error, j.FinishedAt = status, summary, errMsg, &now
	m.jobs[id] = j
	return j, nil
}

func (m *memJobs) FailRunningAIJobs(_ context.Context, reason string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, j := range m.jobs {
		if j.Status == domain.AIJobRunning {
			j.Status, j.Error = domain.AIJobFailed, reason
			m.jobs[id] = j
			n++
		}
	}
	return n, nil
}

// fakeSessions plays the agent's turn: turn runs with the chat's id on the
// context, the way a tool call made inside that turn sees it.
type fakeSessions struct {
	created []domain.CreateSessionRequest
	sent    []domain.SessionMessageRequest
	turn    func(ctx context.Context, sessionID uuid.UUID) (domain.AgentResponse, error)
}

func (f *fakeSessions) Create(_ context.Context, req domain.CreateSessionRequest) (domain.Session, error) {
	f.created = append(f.created, req)
	return domain.Session{ID: uuid.New()}, nil
}

func (f *fakeSessions) SendMessage(ctx context.Context, sid uuid.UUID, req domain.SessionMessageRequest, _ domain.ToolPolicy) (domain.AgentResponse, error) {
	f.sent = append(f.sent, req)
	if f.turn == nil {
		return domain.AgentResponse{Message: domain.Message{Content: "done"}}, nil
	}
	return f.turn(ctx, sid)
}

type fakeRoles struct {
	releaseRole uuid.UUID
	byArea      map[string]uuid.UUID
}

func (f *fakeRoles) RoleByKey(_ context.Context, key string) (domain.AgentRole, error) {
	if key != domain.RoleKeyRelease {
		return domain.AgentRole{}, port.ErrNotFound
	}
	return domain.AgentRole{ID: f.releaseRole}, nil
}

func (f *fakeRoles) AgentForRole(_ context.Context, _ uuid.UUID, area string) (*uuid.UUID, error) {
	if id, ok := f.byArea[area]; ok {
		return &id, nil
	}
	return nil, nil
}

type fakeAgents struct{ disabled map[uuid.UUID]bool }

func (f *fakeAgents) GetAgent(_ context.Context, id uuid.UUID) (domain.Agent, error) {
	return domain.Agent{ID: id, Enabled: !f.disabled[id]}, nil
}

type fakeComponents struct{ comps []domain.Component }

func (f *fakeComponents) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	for _, c := range f.comps {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Component{}, fmt.Errorf("component: %w", port.ErrNotFound)
}

func (f *fakeComponents) ListComponents(_ context.Context, repo uuid.UUID) ([]domain.Component, error) {
	var out []domain.Component
	for _, c := range f.comps {
		if c.RepositoryID == repo {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakeRepos struct{ repos []domain.Repository }

func (f *fakeRepos) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	for _, r := range f.repos {
		if r.ID == id {
			return r, nil
		}
	}
	return domain.Repository{}, port.ErrNotFound
}

type fakeSignals struct{ signals []domain.ComponentSignal }

func (f *fakeSignals) List(_ context.Context, comp uuid.UUID, env domain.DeployEnvironment) (domain.ComponentSignals, error) {
	return domain.ComponentSignals{ComponentID: comp, Environment: env, Signals: f.signals}, nil
}

type fakeEnvironments struct{ envs []domain.ComponentEnvironment }

func (f *fakeEnvironments) ListEnvironments(context.Context, uuid.UUID) ([]domain.ComponentEnvironment, error) {
	return f.envs, nil
}

type fakeLinks struct {
	links     []domain.ComponentLink
	resources []domain.SystemResource
}

func (f *fakeLinks) ListLinks(context.Context, uuid.UUID) ([]domain.ComponentLink, error) {
	return f.links, nil
}
func (f *fakeLinks) ListResources(context.Context, []uuid.UUID) ([]domain.SystemResource, error) {
	return f.resources, nil
}

type fakeTasks struct {
	repo    uuid.UUID
	created []domain.CreateBoardTaskRequest
}

func (f *fakeTasks) CreateTask(_ context.Context, repo uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.repo = repo
	f.created = append(f.created, req)
	return domain.BoardTask{ID: uuid.New(), Key: "TC-7"}, nil
}
