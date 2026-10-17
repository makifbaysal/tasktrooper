package opsquery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// SetBoundLogs makes Logs read a component that has a confirmed logs.lines
// binding through that binding (Loki, Elasticsearch). Without it only cloud
// environments are read. Call it before serving.
func (s *Service) SetBoundLogs(r port.BoundLogReader) { s.boundLogs = r }

// logTarget is one place a logs query reads: a cloud environment, or a
// component's logs binding.
type logTarget struct {
	env     domain.ComponentEnvironment
	binding *domain.SignalBinding
}

func (t logTarget) entry(e domain.RuntimeLogEntry) domain.OpsLogEntry {
	if t.binding != nil {
		id := t.binding.DataSourceID
		return domain.OpsLogEntry{RuntimeLogEntry: e, ComponentID: t.binding.ComponentID, DataSourceID: &id}
	}
	return domain.OpsLogEntry{RuntimeLogEntry: e, ComponentID: t.env.ComponentID, EnvironmentID: t.env.ID}
}

// mergeLogs: a component's binding wins over its cloud environments rather
// than being merged with them. A binding is a person's choice of where that
// component's lines are read, and the same lines often reach both (a
// shipper forwarding the platform's logs to Loki), where a merge would show
// each twice with no reliable key to drop one: the two stores stamp and
// format a line differently. Binding reads count toward MaxLogEnvironments
// like environments do.
func (s *Service) mergeLogs(ctx context.Context, q LogsQuery, since, until time.Time, limit int) (domain.OpsLogs, error) {
	envs, err := s.selectEnvironments(ctx, q.Environment, q.ComponentIDs)
	if err != nil {
		return domain.OpsLogs{}, err
	}
	bound, err := s.selectLogBindings(ctx, q.Environment, q.ComponentIDs)
	if err != nil {
		return domain.OpsLogs{}, err
	}
	targets := logTargets(envs, bound)
	out := domain.OpsLogs{Entries: []domain.OpsLogEntry{}, Unavailable: []domain.OpsUnavailable{}}
	if len(targets) > MaxLogEnvironments {
		if len(q.ComponentIDs) == 0 {
			return domain.OpsLogs{}, &TooManyEnvironmentsError{Matched: len(targets), Max: MaxLogEnvironments}
		}
		targets = targets[:MaxLogEnvironments]
		out.Truncated = true
	}

	rq := domain.RuntimeLogQuery{Since: since, Until: until, MinSeverity: q.MinSeverity, Text: q.Text, Limit: limit}
	lq := domain.LogQuery{Since: since, Until: until, MinSeverity: q.MinSeverity, Text: q.Text, Limit: limit}
	results := fanOut(ctx, len(targets), s.concurrency, s.envTimeout, func(ctx context.Context, i int) (domain.RuntimeLogPage, error) {
		if b := targets[i].binding; b != nil {
			return s.boundLogs.ReadLogs(ctx, *b, lq)
		}
		return s.runtime.Logs(ctx, targets[i].env.ID, rq)
	})
	for i, r := range results {
		t := targets[i]
		if r.err != nil {
			if t.binding != nil {
				out.Unavailable = append(out.Unavailable, s.bindingUnavailable(*t.binding, r.err))
			} else {
				out.Unavailable = append(out.Unavailable, s.unavailable(t.env, r.err))
			}
			continue
		}
		// A full page may hide older entries even without a cursor; not every
		// provider hands one back.
		if r.value.Truncated || r.value.NextCursor != "" || len(r.value.Entries) >= limit {
			out.Truncated = true
		}
		for _, e := range r.value.Entries {
			out.Entries = append(out.Entries, t.entry(e))
		}
	}
	sort.SliceStable(out.Entries, func(i, j int) bool {
		return out.Entries[i].Timestamp.After(out.Entries[j].Timestamp)
	})
	if len(out.Entries) > limit {
		out.Entries = out.Entries[:limit]
		out.Truncated = true
	}
	return out, nil
}

// logTargets: every binding, then every environment of a component without
// one. Bindings go first so that cutting the list at MaxLogEnvironments drops
// environments, never a component's own choice of where its lines are read.
func logTargets(envs []domain.ComponentEnvironment, bound []domain.SignalBinding) []logTarget {
	viaBinding := make(map[uuid.UUID]bool, len(bound))
	out := make([]logTarget, 0, len(envs)+len(bound))
	for i := range bound {
		viaBinding[bound[i].ComponentID] = true
		out = append(out, logTarget{binding: &bound[i]})
	}
	for _, e := range envs {
		if !viaBinding[e.ComponentID] {
			out = append(out, logTarget{env: e})
		}
	}
	return out
}

func (s *Service) selectLogBindings(ctx context.Context, kind domain.DeployEnvironment, componentIDs []uuid.UUID) ([]domain.SignalBinding, error) {
	if s.boundLogs == nil {
		return nil, nil
	}
	all, err := s.boundLogs.LogBindings(ctx, kind)
	if err != nil || len(componentIDs) == 0 {
		return all, err
	}
	wanted := make(map[uuid.UUID]bool, len(componentIDs))
	for _, id := range componentIDs {
		wanted[id] = true
	}
	var out []domain.SignalBinding
	for _, b := range all {
		if wanted[b.ComponentID] {
			out = append(out, b)
		}
	}
	return out, nil
}

func (s *Service) bindingUnavailable(b domain.SignalBinding, err error) domain.OpsUnavailable {
	id := b.DataSourceID
	u := domain.OpsUnavailable{ComponentID: b.ComponentID, DataSourceID: &id}
	switch {
	case errors.Is(err, port.ErrNotFound):
		u.Code, u.Message = domain.OpsNotConnected, "the data source no longer exists"
	case errors.Is(err, context.DeadlineExceeded):
		u.Code, u.Message = domain.OpsTimeout, fmt.Sprintf("no answer from the data source within %s", s.envTimeout)
	default:
		u.Code, u.Message = domain.OpsProviderError, err.Error()
	}
	return u
}
