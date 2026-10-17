package alerting_test

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeRules is an in-memory port.AlertRuleStore: the evaluator's behavior
// depends on the state it wrote in earlier sweeps.
type fakeRules struct {
	mu      sync.Mutex
	rules   map[uuid.UUID]domain.AlertRule
	events  []domain.AlertEvent
	version time.Time
	pruned  []time.Time
}

var _ port.AlertRuleStore = (*fakeRules)(nil)

func newFakeRules() *fakeRules {
	return &fakeRules{rules: map[uuid.UUID]domain.AlertRule{}, version: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *fakeRules) nextVersion() time.Time {
	f.version = f.version.Add(time.Second)
	return f.version
}

func (f *fakeRules) ListAlertRules(_ context.Context, filter domain.AlertRuleFilter) ([]domain.AlertRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.AlertRule{}
	for _, r := range f.rules {
		if (filter.ComponentID == nil || r.ComponentID == *filter.ComponentID) &&
			(filter.Environment == "" || r.Environment == filter.Environment) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (f *fakeRules) GetAlertRule(_ context.Context, id uuid.UUID) (domain.AlertRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rules[id]
	if !ok {
		return domain.AlertRule{}, port.ErrNotFound
	}
	return r, nil
}

func (f *fakeRules) CreateAlertRule(_ context.Context, r domain.AlertRule) (domain.AlertRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	r.CreatedAt = f.nextVersion()
	r.UpdatedAt = r.CreatedAt
	f.rules[r.ID] = r
	return r, nil
}

func (f *fakeRules) UpdateAlertRule(_ context.Context, r domain.AlertRule) (domain.AlertRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rules[r.ID]; !ok {
		return domain.AlertRule{}, port.ErrNotFound
	}
	r.UpdatedAt = f.nextVersion()
	f.rules[r.ID] = r
	return r, nil
}

func (f *fakeRules) DeleteAlertRule(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rules[id]; !ok {
		return port.ErrNotFound
	}
	delete(f.rules, id)
	return nil
}

func (f *fakeRules) DueAlertRules(_ context.Context, before time.Time, limit int) ([]domain.AlertRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.AlertRule
	for _, r := range f.rules {
		if r.Enabled && (r.LastEvaluatedAt == nil || !r.LastEvaluatedAt.After(before)) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].LastEvaluatedAt, out[j].LastEvaluatedAt
		switch {
		case a == nil && b == nil:
			return out[i].ID.String() < out[j].ID.String()
		case a == nil || b == nil:
			return a == nil
		}
		return a.Before(*b)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRules) RecordAlertEvaluation(_ context.Context, id uuid.UUID, specVersion time.Time, eval domain.AlertEvaluation, event *domain.AlertEvent) (domain.AlertRule, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rules[id]
	if !ok || !r.UpdatedAt.Equal(specVersion) {
		return domain.AlertRule{}, false, nil
	}
	evaluatedAt := eval.At
	r.State, r.StateSince, r.LastValue, r.LastEvaluatedAt = eval.State, eval.StateSince, eval.Value, &evaluatedAt
	r.AlertStreak = eval.Streak
	r.AlertReadErrors = eval.Errors
	f.rules[id] = r
	if event != nil {
		e := *event
		e.RuleID = id
		f.events = append(f.events, e)
	}
	return r, true, nil
}

func (f *fakeRules) AppendAlertEvent(_ context.Context, e domain.AlertEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeRules) ListAlertEvents(_ context.Context, ruleID uuid.UUID, limit int) ([]domain.AlertEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.AlertEvent{}
	for _, e := range slices.Backward(f.events) {
		if e.RuleID == ruleID && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeRules) PruneAlertEvents(_ context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruned = append(f.pruned, before)
	return 0, nil
}

func (f *fakeRules) get(id uuid.UUID) domain.AlertRule {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rules[id]
}

func (f *fakeRules) transitions(id uuid.UUID) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, e := range f.events {
		if e.RuleID == id {
			out = append(out, string(e.FromState)+">"+string(e.ToState))
		}
	}
	return out
}

func (f *fakeRules) eventsOf(id uuid.UUID) []domain.AlertEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.AlertEvent
	for _, e := range f.events {
		if e.RuleID == id {
			out = append(out, e)
		}
	}
	return out
}
