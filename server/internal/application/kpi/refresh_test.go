package kpi_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type memoryKPIStore struct {
	mu      sync.Mutex
	kpis    map[uuid.UUID]domain.AgentKPI
	results map[uuid.UUID]domain.AgentKPIResult
	upserts int
	onList  func()
}

func newMemoryKPIStore() *memoryKPIStore {
	return &memoryKPIStore{kpis: map[uuid.UUID]domain.AgentKPI{}, results: map[uuid.UUID]domain.AgentKPIResult{}}
}

func (m *memoryKPIStore) CreateKPI(_ context.Context, k domain.AgentKPI) (domain.AgentKPI, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k.ID = uuid.New()
	m.kpis[k.ID] = k
	return k, nil
}

func (m *memoryKPIStore) UpdateKPI(_ context.Context, k domain.AgentKPI) (domain.AgentKPI, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kpis[k.ID] = k
	return k, nil
}

func (m *memoryKPIStore) DeleteKPI(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.kpis, id)
	delete(m.results, id)
	return nil
}

func (m *memoryKPIStore) GetKPI(_ context.Context, id uuid.UUID) (domain.AgentKPI, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.kpis[id]
	if !ok {
		return domain.AgentKPI{}, errors.New("kpi not found")
	}
	return k, nil
}

func (m *memoryKPIStore) ListByAgent(_ context.Context, agentID uuid.UUID) ([]domain.AgentKPI, error) {
	if m.onList != nil {
		m.onList()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.AgentKPI
	for _, k := range m.kpis {
		if k.AgentID == agentID {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memoryKPIStore) UpsertResult(_ context.Context, r domain.AgentKPIResult) (domain.AgentKPIResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upserts++
	m.results[r.KPIID] = r
	return r, nil
}

func (m *memoryKPIStore) ListResults(ctx context.Context, agentID uuid.UUID, _, _ time.Time) ([]domain.AgentKPIResult, error) {
	return m.LatestResults(ctx, agentID)
}

func (m *memoryKPIStore) LatestResults(_ context.Context, agentID uuid.UUID) ([]domain.AgentKPIResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.AgentKPIResult
	for _, r := range m.results {
		if r.AgentID == agentID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memoryKPIStore) upsertCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.upserts
}

const refreshMaxAge = time.Minute

type RefreshAgentSuite struct {
	suite.Suite
	ctx     context.Context
	store   *memoryKPIStore
	svc     *kpi.Service
	agentID uuid.UUID
	kpiA    domain.AgentKPI
	now     time.Time
}

func TestRefreshAgentSuite(t *testing.T) {
	suite.Run(t, new(RefreshAgentSuite))
}

func (s *RefreshAgentSuite) SetupTest() {
	s.ctx = context.Background()
	s.store = newMemoryKPIStore()
	s.svc = kpi.NewService(s.store, kpi.MetricDeps{Perf: &stubPerf{}})
	s.agentID = uuid.New()
	s.kpiA = s.createKPI("Revisions")
	// A second KPI keeps the delete case observable: something is left to evaluate.
	s.createKPI("Revisions again")
	s.now = time.Date(2026, 7, 8, 15, 30, 0, 0, time.UTC)
}

func (s *RefreshAgentSuite) SetupSubTest() {
	s.SetupTest()
}

func (s *RefreshAgentSuite) createKPI(name string) domain.AgentKPI {
	created, err := s.svc.CreateKPI(s.ctx, s.agentID, domain.CreateKPIRequest{
		MetricKey: "revisions_received", Name: name, Period: domain.KPIPeriodWeekly,
		TargetFull: 0, TargetHalf: 2, Weight: 1, Enabled: true,
	})
	s.Require().NoError(err)
	return created
}

func (s *RefreshAgentSuite) refreshAt(elapsed time.Duration) {
	s.Require().NoError(s.svc.RefreshAgent(s.ctx, s.agentID, s.now.Add(elapsed), refreshMaxAge))
}

func (s *RefreshAgentSuite) TestFirstReadEvaluatesEveryEnabledKPI() {
	s.refreshAt(0)

	s.Equal(2, s.store.upsertCount())
	results, err := s.svc.LatestResults(s.ctx, s.agentID)
	s.Require().NoError(err)
	s.Len(results, 2)
}

func (s *RefreshAgentSuite) TestLaterReadEvaluatesOnlyOnceTheLastEvaluationIsOlderThanMaxAge() {
	tests := []struct {
		name      string
		elapsed   time.Duration
		evaluates bool
	}{
		{name: "a few seconds later", elapsed: 5 * time.Second, evaluates: false},
		{name: "exactly max age later", elapsed: refreshMaxAge, evaluates: false},
		{name: "past max age", elapsed: refreshMaxAge + time.Second, evaluates: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.refreshAt(0)
			before := s.store.upsertCount()

			s.refreshAt(tt.elapsed)

			if tt.evaluates {
				s.Greater(s.store.upsertCount(), before)
			} else {
				s.Equal(before, s.store.upsertCount())
			}
		})
	}
}

func (s *RefreshAgentSuite) TestKPIEditMakesTheNextReadEvaluate() {
	tests := []struct {
		name string
		edit func() error
	}{
		{name: "create", edit: func() error {
			s.createKPI("Revisions a third time")
			return nil
		}},
		{name: "update", edit: func() error {
			_, err := s.svc.UpdateKPI(s.ctx, s.agentID, s.kpiA.ID, domain.UpdateKPIRequest{
				Name: s.kpiA.Name, TargetFull: 1, TargetHalf: 3, Enabled: true,
			})
			return err
		}},
		{name: "delete", edit: func() error {
			return s.svc.DeleteKPI(s.ctx, s.agentID, s.kpiA.ID)
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.refreshAt(0)
			before := s.store.upsertCount()

			s.Require().NoError(tt.edit())
			s.refreshAt(time.Second)

			s.Greater(s.store.upsertCount(), before)
		})
	}
}

func (s *RefreshAgentSuite) TestEditDuringAnEvaluationLeavesTheAgentStale() {
	s.store.onList = func() {
		s.store.onList = nil
		_, err := s.svc.UpdateKPI(s.ctx, s.agentID, s.kpiA.ID, domain.UpdateKPIRequest{
			Name: s.kpiA.Name, TargetFull: 1, TargetHalf: 3, Enabled: true,
		})
		s.Require().NoError(err)
	}
	s.refreshAt(0)
	before := s.store.upsertCount()

	s.refreshAt(time.Second)

	s.Greater(s.store.upsertCount(), before)
}

func (s *RefreshAgentSuite) TestFreshnessIsTrackedPerAgent() {
	s.refreshAt(0)
	other := uuid.New()
	_, err := s.svc.CreateKPI(s.ctx, other, domain.CreateKPIRequest{
		MetricKey: "revisions_received", Name: "Other", TargetFull: 0, TargetHalf: 2, Enabled: true,
	})
	s.Require().NoError(err)
	before := s.store.upsertCount()

	s.Require().NoError(s.svc.RefreshAgent(s.ctx, other, s.now.Add(time.Second), refreshMaxAge))
	s.refreshAt(2 * time.Second)

	s.Equal(before+1, s.store.upsertCount())
}
