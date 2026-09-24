package cloud

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	runtimeCacheTTL      = 30 * time.Second
	defaultLogWindow     = time.Hour
	defaultLogLimit      = 200
	maxLogLimit          = 1000
	maxLogWindow         = 7 * 24 * time.Hour
	defaultDeployments   = 10
	overviewErrorsWindow = 24 * time.Hour
)

// runtimeBound is the connectivity a runtime read needs: an environment bound
// to an account+resource, with a live provider adapter for it. Every runtime
// method fails the same way when either half is missing, so it is resolved
// once here.
type runtimeBound struct {
	env      domain.ComponentEnvironment
	provider port.CloudProvider
	cred     domain.CloudCredential
}

func (s *Service) resolveRuntimeBound(ctx context.Context, envID uuid.UUID) (runtimeBound, error) {
	env, err := s.environments.GetEnvironment(ctx, envID)
	if err != nil {
		return runtimeBound{}, err
	}
	if !env.Bound() {
		return runtimeBound{env: env}, ErrNotConnected
	}
	provider, ok := s.providerFor(env.Provider)
	if !ok {
		return runtimeBound{env: env}, ErrNotConnected
	}
	cred, err := s.accounts.CloudCredential(ctx, *env.AccountID)
	if err != nil {
		return runtimeBound{env: env}, err
	}
	return runtimeBound{env: env, provider: provider, cred: cred}, nil
}

func (s *Service) markAccountError(ctx context.Context, accountID uuid.UUID, cause error) {
	acct, err := s.accounts.GetCloudAccount(ctx, accountID)
	if err != nil {
		return
	}
	acct.Status = domain.CloudAccountError
	acct.StatusDetail = cause.Error()
	_, _ = s.accounts.UpdateCloudAccount(ctx, acct, nil)
	s.forgetResourceCache(accountID)
}

func (s *Service) runtimeCached(key runtimeCacheKey, fetch func() (interface{}, error)) (interface{}, error) {
	s.runtimeCacheMu.Lock()
	if e, ok := s.runtimeCache[key]; ok && s.now().Before(e.expires) {
		s.runtimeCacheMu.Unlock()
		return e.value, e.err
	}
	s.runtimeCacheMu.Unlock()

	v, err := fetch()

	s.runtimeCacheMu.Lock()
	s.runtimeCache[key] = runtimeCacheEntry{value: v, err: err, expires: s.now().Add(runtimeCacheTTL)}
	s.runtimeCacheMu.Unlock()
	return v, err
}

// Overview is GET …/environments/:env/overview: the live picture the Deploy &
// Runtime tab opens on. It never returns an error for "not connected" or a
// provider auth failure — those are reported through Unavailable, same as any
// other unreachable-but-not-broken state, and a bad credential also marks the
// account so the UI can say "reconnect" instead of quietly retrying forever.
func (s *Service) Overview(ctx context.Context, envID uuid.UUID) (domain.EnvironmentRuntime, error) {
	bound, err := s.resolveRuntimeBound(ctx, envID)
	if err != nil {
		if errors.Is(err, ErrNotConnected) {
			return domain.EnvironmentRuntime{Environment: bound.env, Unavailable: "not connected to a cloud account"}, nil
		}
		return domain.EnvironmentRuntime{}, err
	}
	env := bound.env
	out := domain.EnvironmentRuntime{Environment: env}

	key := runtimeCacheKey{envID, "resource", ""}
	v, err := s.runtimeCached(key, func() (interface{}, error) {
		return bound.provider.Resource(ctx, bound.cred, *env.Resource)
	})
	if err != nil {
		if errors.Is(err, port.ErrCloudAuth) {
			s.markAccountError(ctx, *env.AccountID, err)
			out.Unavailable = fmt.Sprintf("the %s account needs reconnecting", env.Provider)
			return out, nil
		}
		out.Unavailable = err.Error()
		return out, nil
	}
	detail := v.(domain.CloudResourceDetail)
	out.Detail = &detail

	if deployments, err := s.Deployments(ctx, envID, defaultDeployments); err == nil {
		out.Deployments = deployments
	}
	if groups, err := s.Errors(ctx, envID, s.now().Add(-overviewErrorsWindow)); err == nil {
		out.ErrorsLast = groups
	}
	return out, nil
}

func normalizeLogQuery(now time.Time, q domain.RuntimeLogQuery) domain.RuntimeLogQuery {
	if q.Until.IsZero() {
		q.Until = now
	}
	if q.Since.IsZero() {
		q.Since = q.Until.Add(-defaultLogWindow)
	}
	if q.Limit <= 0 {
		q.Limit = defaultLogLimit
	}
	if q.Limit > maxLogLimit {
		q.Limit = maxLogLimit
	}
	if q.Until.Sub(q.Since) > maxLogWindow {
		q.Since = q.Until.Add(-maxLogWindow)
	}
	return q
}

func logQueryCacheParams(q domain.RuntimeLogQuery) string {
	return fmt.Sprintf("%d|%d|%s|%s|%d|%s", q.Since.Unix(), q.Until.Unix(), q.MinSeverity, q.Text, q.Limit, q.Cursor)
}

func (s *Service) Logs(ctx context.Context, envID uuid.UUID, q domain.RuntimeLogQuery) (domain.RuntimeLogPage, error) {
	bound, err := s.resolveRuntimeBound(ctx, envID)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	q = normalizeLogQuery(s.now(), q)

	key := runtimeCacheKey{envID, "logs", logQueryCacheParams(q)}
	v, err := s.runtimeCached(key, func() (interface{}, error) {
		return bound.provider.Logs(ctx, bound.cred, *bound.env.Resource, q)
	})
	if err != nil {
		if errors.Is(err, port.ErrCloudAuth) {
			s.markAccountError(ctx, *bound.env.AccountID, err)
		}
		return domain.RuntimeLogPage{}, err
	}
	return v.(domain.RuntimeLogPage), nil
}

// Errors is the provider's native error grouping when it has one; when it
// returns port.ErrUnsupported, error-level logs over the window are fetched
// instead and grouped locally (grouping.go).
func (s *Service) Errors(ctx context.Context, envID uuid.UUID, since time.Time) ([]domain.RuntimeErrorGroup, error) {
	bound, err := s.resolveRuntimeBound(ctx, envID)
	if err != nil {
		return nil, err
	}

	key := runtimeCacheKey{envID, "errors", since.Truncate(time.Minute).Format(time.RFC3339)}
	v, err := s.runtimeCached(key, func() (interface{}, error) {
		groups, gerr := bound.provider.Errors(ctx, bound.cred, *bound.env.Resource, since)
		if gerr == nil {
			return groups, nil
		}
		if !errors.Is(gerr, port.ErrUnsupported) {
			return nil, gerr
		}
		page, lerr := bound.provider.Logs(ctx, bound.cred, *bound.env.Resource, domain.RuntimeLogQuery{
			Since: since, Until: s.now(), MinSeverity: domain.LogError, Limit: maxLogLimit,
		})
		if lerr != nil {
			return nil, lerr
		}
		return GroupErrors(page.Entries, since), nil
	})
	if err != nil {
		if errors.Is(err, port.ErrCloudAuth) {
			s.markAccountError(ctx, *bound.env.AccountID, err)
		}
		return nil, err
	}
	return v.([]domain.RuntimeErrorGroup), nil
}

func (s *Service) Deployments(ctx context.Context, envID uuid.UUID, limit int) ([]domain.CloudDeployment, error) {
	bound, err := s.resolveRuntimeBound(ctx, envID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultDeployments
	}

	key := runtimeCacheKey{envID, "deployments", strconv.Itoa(limit)}
	v, err := s.runtimeCached(key, func() (interface{}, error) {
		return bound.provider.Deployments(ctx, bound.cred, *bound.env.Resource, limit)
	})
	if err != nil {
		if errors.Is(err, port.ErrCloudAuth) {
			s.markAccountError(ctx, *bound.env.AccountID, err)
		}
		return nil, err
	}
	return v.([]domain.CloudDeployment), nil
}
