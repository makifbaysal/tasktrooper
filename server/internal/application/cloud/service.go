// Package cloud connects components to where they run: cloud provider
// accounts, the per-(component, environment) bindings a scan matches or a
// human confirms, and the runtime picture (deployments, logs, errors) read
// through the matching port.CloudProvider.
package cloud

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Repos is the narrow repository read seam this package needs: MatchScan and
// the backfill read one repository's identity, rematchAll and Boot walk all
// of them.
type Repos interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	List(ctx context.Context) ([]domain.Repository, error)
}

// TaskCreator opens the board task CreateFixTask files. It is
// repository.Service.CreateTask narrowed to what this package needs.
type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
}

type Clock func() time.Time

type Deps struct {
	Accounts      port.CloudAccountStore
	Environments  port.EnvironmentStore
	Providers     []port.CloudProvider
	Components    port.ComponentStore
	Scans         port.ScanStore
	Repos         Repos
	DeployTargets port.DeployTargetStore
	Tasks         TaskCreator
	Legacy        port.LegacyCloudSource
}

type resourceCacheEntry struct {
	resources []domain.CloudResource
	expires   time.Time
}

type runtimeCacheKey struct {
	envID  uuid.UUID
	op     string
	params string
}

type runtimeCacheEntry struct {
	value   interface{}
	err     error
	expires time.Time
}

type Service struct {
	accounts      port.CloudAccountStore
	environments  port.EnvironmentStore
	providers     []port.CloudProvider
	components    port.ComponentStore
	scans         port.ScanStore
	repos         Repos
	deployTargets port.DeployTargetStore
	tasks         TaskCreator
	legacy        port.LegacyCloudSource

	now   Clock
	bgCtx context.Context

	resourceCacheMu sync.Mutex
	resourceCache   map[uuid.UUID]resourceCacheEntry

	runtimeCacheMu sync.Mutex
	runtimeCache   map[runtimeCacheKey]runtimeCacheEntry

	healthSweepInterval time.Duration
	healthFirstDelay    time.Duration
	healthRateLimit     time.Duration
}

func NewService(d Deps) *Service {
	return &Service{
		accounts:            d.Accounts,
		environments:        d.Environments,
		providers:           d.Providers,
		components:          d.Components,
		scans:               d.Scans,
		repos:               d.Repos,
		deployTargets:       d.DeployTargets,
		tasks:               d.Tasks,
		legacy:              d.Legacy,
		now:                 func() time.Time { return time.Now().UTC() },
		bgCtx:               context.Background(),
		resourceCache:       map[uuid.UUID]resourceCacheEntry{},
		runtimeCache:        map[runtimeCacheKey]runtimeCacheEntry{},
		healthSweepInterval: defaultHealthSweepInterval,
		healthFirstDelay:    defaultHealthFirstDelay,
		healthRateLimit:     defaultHealthRateLimit,
	}
}

func (s *Service) SetClock(c Clock) { s.now = c }

// SetBackgroundContext is the process-lifetime context rematchAll and Boot
// run under, so a request's cancellation never kills work it only started.
func (s *Service) SetBackgroundContext(ctx context.Context) { s.bgCtx = ctx }

func (s *Service) providerFor(kind domain.CloudProviderKind) (port.CloudProvider, bool) {
	for _, p := range s.providers {
		if p.Kind() == kind {
			return p, true
		}
	}
	return nil, false
}
