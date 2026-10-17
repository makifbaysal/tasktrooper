// Package opsquery answers the Operations console's cross-component reads —
// the Errors inbox and the global Logs explorer — by fanning one query out
// over every confirmed, bound environment of one deploy environment kind and
// merging the answers. An environment that fails is reported in Unavailable;
// it never fails the whole answer.
package opsquery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	DefaultErrorsWindow  = 24 * time.Hour
	MaxErrorsWindow      = 30 * 24 * time.Hour
	MaxErrorEnvironments = 50

	DefaultLogWindow   = time.Hour
	DefaultLogLimit    = 200
	MaxLogLimit        = 500
	MaxLogEnvironments = 10

	defaultConcurrency = 4
	defaultEnvTimeout  = 20 * time.Second
)

type Environments interface {
	ListAllEnvironments(ctx context.Context) ([]domain.ComponentEnvironment, error)
}

type Components interface {
	ListAllComponents(ctx context.Context) ([]domain.Component, error)
}

type Deps struct {
	Environments Environments
	// Components, when set, leaves out the environments of dismissed
	// components, which no component picker offers.
	Components Components
	Runtime    port.RuntimeReader
}

type Service struct {
	environments Environments
	components   Components
	runtime      port.RuntimeReader
	boundLogs    port.BoundLogReader
	boundErrors  port.BoundErrorReader
	now          func() time.Time
	concurrency  int
	envTimeout   time.Duration
}

type Option func(*Service)

func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

func WithEnvironmentTimeout(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.envTimeout = d
		}
	}
}

func WithConcurrency(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.concurrency = n
		}
	}
}

func NewService(d Deps, opts ...Option) *Service {
	s := &Service{
		environments: d.Environments,
		components:   d.Components,
		runtime:      d.Runtime,
		now:          func() time.Time { return time.Now().UTC() },
		concurrency:  defaultConcurrency,
		envTimeout:   defaultEnvTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ErrorsQuery: a zero Since is DefaultErrorsWindow ago; one further back than
// MaxErrorsWindow is clamped to it. No ComponentIDs means every component.
type ErrorsQuery struct {
	Environment  domain.DeployEnvironment
	Since        time.Time
	ComponentIDs []uuid.UUID
}

// LogsQuery: a zero Until is now and a zero Since is DefaultLogWindow before
// Until. Limit bounds both each environment's page and the merged answer.
type LogsQuery struct {
	Environment  domain.DeployEnvironment
	ComponentIDs []uuid.UUID
	Since        time.Time
	Until        time.Time
	MinSeverity  domain.LogSeverity
	Text         string
	Limit        int
}

// Errors skips an environment whose provider has no error surface without
// reporting it: there is nothing to read there, so nothing is missing. The
// groups of each component's errors.issues binding join the cloud's (see
// readBoundErrors).
func (s *Service) Errors(ctx context.Context, q ErrorsQuery) (domain.OpsErrors, error) {
	if !q.Environment.Valid() {
		return domain.OpsErrors{}, fmt.Errorf("unknown environment %q: %w", q.Environment, ErrInvalidInput)
	}
	now := s.now()
	since := q.Since
	if since.IsZero() {
		since = now.Add(-DefaultErrorsWindow)
	}
	if earliest := now.Add(-MaxErrorsWindow); since.Before(earliest) {
		since = earliest
	}

	envs, err := s.selectEnvironments(ctx, q.Environment, q.ComponentIDs)
	if err != nil {
		return domain.OpsErrors{}, err
	}
	readable := make([]domain.ComponentEnvironment, 0, len(envs))
	for _, e := range envs {
		if s.runtime.ErrorsSupported(e) {
			readable = append(readable, e)
		}
	}

	out := domain.OpsErrors{Groups: []domain.OpsErrorGroup{}, Unavailable: []domain.OpsUnavailable{}}
	if len(readable) > MaxErrorEnvironments {
		readable = readable[:MaxErrorEnvironments]
		out.Truncated = true
	}

	waitBound := s.readBoundErrors(ctx, q, since, now)
	results := fanOut(ctx, len(readable), s.concurrency, s.envTimeout, func(ctx context.Context, i int) ([]domain.RuntimeErrorGroup, error) {
		return s.runtime.Errors(ctx, readable[i].ID, since)
	})
	for i, r := range results {
		env := readable[i]
		if r.err != nil {
			out.Unavailable = append(out.Unavailable, s.unavailable(env, r.err))
			continue
		}
		for _, g := range r.value {
			out.Groups = append(out.Groups, domain.OpsErrorGroup{
				ComponentID:       env.ComponentID,
				RepositoryID:      env.RepositoryID,
				EnvironmentID:     env.ID,
				Provider:          env.Provider,
				RuntimeErrorGroup: g,
			})
		}
	}
	bound := waitBound()
	out.Groups = append(out.Groups, bound.groups...)
	out.Unavailable = append(out.Unavailable, bound.unavailable...)
	out.Truncated = out.Truncated || bound.truncated
	sort.SliceStable(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if a.New != b.New {
			return a.New
		}
		if !a.LastSeen.Equal(b.LastSeen) {
			return a.LastSeen.After(b.LastSeen)
		}
		return a.Count > b.Count
	})
	return out, nil
}

// Logs merges every environment's newest entries by timestamp, and those of
// each component's logs binding, which replaces that component's cloud
// environments (see mergeLogs). There is no cursor across providers: the
// next older page is the same query with Until set to the oldest timestamp
// returned.
func (s *Service) Logs(ctx context.Context, q LogsQuery) (domain.OpsLogs, error) {
	if !q.Environment.Valid() {
		return domain.OpsLogs{}, fmt.Errorf("unknown environment %q: %w", q.Environment, ErrInvalidInput)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLogLimit
	}
	if limit > MaxLogLimit {
		limit = MaxLogLimit
	}
	until := q.Until
	if until.IsZero() {
		until = s.now()
	}
	since := q.Since
	if since.IsZero() {
		since = until.Add(-DefaultLogWindow)
	}
	if !since.Before(until) {
		return domain.OpsLogs{}, fmt.Errorf("since must be before until: %w", ErrInvalidInput)
	}

	return s.mergeLogs(ctx, q, since, until, limit)
}

func (s *Service) selectEnvironments(ctx context.Context, kind domain.DeployEnvironment, componentIDs []uuid.UUID) ([]domain.ComponentEnvironment, error) {
	all, err := s.environments.ListAllEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	dismissed, err := s.dismissedComponents(ctx)
	if err != nil {
		return nil, err
	}
	wanted := make(map[uuid.UUID]bool, len(componentIDs))
	for _, id := range componentIDs {
		wanted[id] = true
	}
	var out []domain.ComponentEnvironment
	for _, e := range all {
		if e.Environment != kind || e.Status != domain.LinkConfirmed || !e.Bound() {
			continue
		}
		if len(wanted) > 0 && !wanted[e.ComponentID] {
			continue
		}
		if dismissed[e.ComponentID] {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Service) dismissedComponents(ctx context.Context) (map[uuid.UUID]bool, error) {
	if s.components == nil {
		return nil, nil
	}
	components, err := s.components.ListAllComponents(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	for _, c := range components {
		if c.Status == domain.ComponentStatusDismissed {
			out[c.ID] = true
		}
	}
	return out, nil
}

func (s *Service) unavailable(env domain.ComponentEnvironment, err error) domain.OpsUnavailable {
	u := domain.OpsUnavailable{ComponentID: env.ComponentID, EnvironmentID: env.ID}
	switch {
	case errors.Is(err, port.ErrCloudAuth):
		u.Code, u.Message = domain.OpsCloudAuth, fmt.Sprintf("the %s account needs reconnecting", env.Provider)
	case errors.Is(err, cloud.ErrNotConnected), errors.Is(err, port.ErrNotFound):
		u.Code, u.Message = domain.OpsNotConnected, "not connected to a cloud account"
	case errors.Is(err, context.DeadlineExceeded):
		u.Code, u.Message = domain.OpsTimeout, fmt.Sprintf("no answer from %s within %s", env.Provider, s.envTimeout)
	default:
		u.Code, u.Message = domain.OpsProviderError, err.Error()
	}
	return u
}
