package kpi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type Service struct {
	store port.AgentKPIStore
	deps  MetricDeps

	workflows port.WorkflowReader
	roles     port.RoleResolver

	evalMu      sync.Mutex
	evaluations map[uuid.UUID]evaluation
}

// evaluation is when RefreshAgent last evaluated an agent. gen counts KPI
// edits, so an evaluation that raced an edit is not recorded as fresh.
type evaluation struct {
	at  time.Time
	gen uint64
}

func NewService(store port.AgentKPIStore, deps MetricDeps) *Service {
	return &Service{store: store, deps: deps, evaluations: map[uuid.UUID]evaluation{}}
}

func (s *Service) SetWorkflows(w port.WorkflowReader)  { s.workflows = w }
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

func (s *Service) ListMetrics() []domain.KPIMetricInfo {
	return ListMetrics()
}

func (s *Service) validate(metricKey, period string, targetFull, targetHalf, weight float64) error {
	if _, err := MetricByKey(metricKey); err != nil {
		return err
	}
	if !domain.ValidKPIPeriod(period) {
		return fmt.Errorf("invalid period: %s (daily|weekly|monthly)", period)
	}
	def, _ := MetricByKey(metricKey)
	if def.Info.Direction == domain.KPIDirectionLowerBetter && targetHalf < targetFull {
		return fmt.Errorf("for lower-better metrics target_half must be >= target_full")
	}
	if def.Info.Direction == domain.KPIDirectionHigherBetter && targetHalf > targetFull {
		return fmt.Errorf("for higher-better metrics target_half must be <= target_full")
	}
	if weight <= 0 {
		return fmt.Errorf("weight must be positive")
	}
	return nil
}

func (s *Service) CreateKPI(ctx context.Context, agentID uuid.UUID, req domain.CreateKPIRequest) (domain.AgentKPI, error) {
	if req.Period == "" {
		req.Period = domain.KPIPeriodWeekly
	}
	if req.Weight == 0 {
		req.Weight = 1
	}
	if err := s.validate(req.MetricKey, req.Period, req.TargetFull, req.TargetHalf, req.Weight); err != nil {
		return domain.AgentKPI{}, err
	}
	created, err := s.store.CreateKPI(ctx, domain.AgentKPI{
		AgentID: agentID, MetricKey: req.MetricKey, Name: req.Name, Description: req.Description,
		Period: req.Period, TargetFull: req.TargetFull, TargetHalf: req.TargetHalf,
		Weight: req.Weight, Enabled: req.Enabled,
	})
	if err != nil {
		return domain.AgentKPI{}, err
	}
	s.forgetEvaluation(agentID)
	return created, nil
}

func (s *Service) UpdateKPI(ctx context.Context, agentID, kpiID uuid.UUID, req domain.UpdateKPIRequest) (domain.AgentKPI, error) {
	existing, err := s.store.GetKPI(ctx, kpiID)
	if err != nil {
		return domain.AgentKPI{}, err
	}
	if existing.AgentID != agentID {
		return domain.AgentKPI{}, fmt.Errorf("kpi does not belong to this agent")
	}
	if req.Period == "" {
		req.Period = existing.Period
	}
	if req.Weight == 0 {
		req.Weight = existing.Weight
	}
	if req.MetricKey == "" {
		req.MetricKey = existing.MetricKey
	}
	if err := s.validate(req.MetricKey, req.Period, req.TargetFull, req.TargetHalf, req.Weight); err != nil {
		return domain.AgentKPI{}, err
	}
	existing.MetricKey = req.MetricKey
	existing.Name = req.Name
	existing.Description = req.Description
	existing.Period = req.Period
	existing.TargetFull = req.TargetFull
	existing.TargetHalf = req.TargetHalf
	existing.Weight = req.Weight
	existing.Enabled = req.Enabled
	updated, err := s.store.UpdateKPI(ctx, existing)
	if err != nil {
		return domain.AgentKPI{}, err
	}
	s.forgetEvaluation(agentID)
	return updated, nil
}

func (s *Service) DeleteKPI(ctx context.Context, agentID, kpiID uuid.UUID) error {
	existing, err := s.store.GetKPI(ctx, kpiID)
	if err != nil {
		return err
	}
	if existing.AgentID != agentID {
		return fmt.Errorf("kpi does not belong to this agent")
	}
	if err := s.store.DeleteKPI(ctx, kpiID); err != nil {
		return err
	}
	s.forgetEvaluation(agentID)
	return nil
}

func (s *Service) ListKPIs(ctx context.Context, agentID uuid.UUID) ([]domain.AgentKPI, error) {
	return s.store.ListByAgent(ctx, agentID)
}

func (s *Service) LatestResults(ctx context.Context, agentID uuid.UUID) ([]domain.AgentKPIResult, error) {
	return s.store.LatestResults(ctx, agentID)
}

func (s *Service) ListResults(ctx context.Context, agentID uuid.UUID, from, to time.Time) ([]domain.AgentKPIResult, error) {
	return s.store.ListResults(ctx, agentID, from, to)
}

func PeriodBounds(period string, now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	switch period {
	case domain.KPIPeriodDaily:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1)
	case domain.KPIPeriodMonthly:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0)
	default:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(weekday - 1))
		return start, start.AddDate(0, 0, 7)
	}
}

func (s *Service) EvaluateAgent(ctx context.Context, agentID uuid.UUID, now time.Time) ([]domain.AgentKPIResult, error) {
	kpis, err := s.store.ListByAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	var results []domain.AgentKPIResult
	for _, k := range kpis {
		if !k.Enabled {
			continue
		}
		def, err := MetricByKey(k.MetricKey)
		if err != nil {
			log.Warn().Str("metric", k.MetricKey).Msg("kpi metric no longer in registry, skipping")
			continue
		}
		from, to := PeriodBounds(k.Period, now)
		deps := s.deps
		deps.Workflows = s.workflows
		value, err := def.Resolve(ctx, deps, agentID, from, to)
		if errors.Is(err, ErrInsufficientData) {

			log.Debug().Str("metric", k.MetricKey).Msg("kpi has insufficient data this period")
			continue
		}
		if err != nil {
			log.Warn().Err(err).Str("metric", k.MetricKey).Msg("kpi resolve failed")
			continue
		}
		res, err := s.store.UpsertResult(ctx, domain.AgentKPIResult{
			KPIID: k.ID, AgentID: agentID,
			PeriodStart: from, PeriodEnd: to,
			MeasuredValue: value,
			Attainment:    domain.KPIAttainment(def.Info.Direction, value, k.TargetFull, k.TargetHalf),
		})
		if err != nil {
			log.Warn().Err(err).Str("metric", k.MetricKey).Msg("kpi result upsert failed")
			continue
		}
		results = append(results, res)
	}
	return results, nil
}

// RefreshAgent runs EvaluateAgent unless this service already evaluated the
// agent within maxAge of now. A KPI created, updated or deleted through this
// service makes the next call evaluate regardless of age.
func (s *Service) RefreshAgent(ctx context.Context, agentID uuid.UUID, now time.Time, maxAge time.Duration) error {
	s.evalMu.Lock()
	last := s.evaluations[agentID]
	s.evalMu.Unlock()
	if !last.at.IsZero() && now.Sub(last.at) <= maxAge {
		return nil
	}
	if _, err := s.EvaluateAgent(ctx, agentID, now); err != nil {
		return err
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	if s.evaluations[agentID].gen == last.gen {
		s.evaluations[agentID] = evaluation{at: now, gen: last.gen}
	}
	return nil
}

func (s *Service) forgetEvaluation(agentID uuid.UUID) {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	s.evaluations[agentID] = evaluation{gen: s.evaluations[agentID].gen + 1}
}

func CompositeScore(kpis []domain.AgentKPI, results []domain.AgentKPIResult) float64 {
	byKPI := make(map[uuid.UUID]domain.AgentKPIResult, len(results))
	for _, r := range results {
		byKPI[r.KPIID] = r
	}
	var weightSum, scoreSum float64
	for _, k := range kpis {
		if !k.Enabled {
			continue
		}
		r, ok := byKPI[k.ID]
		if !ok {
			continue
		}
		weightSum += k.Weight
		scoreSum += k.Weight * r.Attainment
	}
	if weightSum == 0 {
		return 0
	}
	return scoreSum / weightSum * 100.0
}
