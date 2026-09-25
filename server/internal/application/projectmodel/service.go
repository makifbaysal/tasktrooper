// Package projectmodel owns the structured knowledge about every repository:
// its components, the CI checks that verify them, and what they connect to.
// Scans write the detected half; the human writes overrides; agents read both
// through the brief and the tools.
package projectmodel

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Scanner is application/discovery behind an interface so tests can feed a
// fixed ScanResult.
type Scanner interface {
	Scan(ctx context.Context, root string, emit func(domain.ScanEvent)) (domain.ScanResult, error)
}

type RepositoryReader interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	List(ctx context.Context) ([]domain.Repository, error)
}

// LegacyProjector writes the pre-component repository fields that storeops,
// deploy, prodops and the board still read, derived from the components.
type LegacyProjector interface {
	UpdateMeta(ctx context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string) (domain.Repository, error)
	UpdateSubProjects(ctx context.Context, id uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error)
	UpdateMobilePlatform(ctx context.Context, id uuid.UUID, platform string) (domain.Repository, error)
	UpdateDetectedAppIdentity(ctx context.Context, id uuid.UUID, identity domain.AppIdentity) (domain.Repository, error)
	UpdateDetectedBuildTargets(ctx context.Context, id uuid.UUID, targets domain.BuildTargets) (domain.Repository, error)
	UpdateQualityGates(ctx context.Context, id uuid.UUID, coverage, mutation domain.QualityGate) (domain.Repository, error)
}

type ProjectLister interface {
	List(ctx context.Context) ([]domain.InitiativeProject, error)
	Get(ctx context.Context, id uuid.UUID) (domain.InitiativeProject, error)
}

// PipelineJobWriter receives the checks projected onto the legacy pipeline
// slots the board's CI gate and the deploy monitor read.
type PipelineJobWriter interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error)
	ReplaceForRepository(ctx context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error)
}

// DeployMatcher is cloud.Service narrowed to what a finished scan needs: turn
// its deploy signals into environment bindings. A local interface rather than
// an import of the cloud package, the same seam Scanner uses to keep
// application packages from depending on one another directly.
type DeployMatcher interface {
	MatchScan(ctx context.Context, repositoryID uuid.UUID, result domain.ScanResult) error
}

type Clock func() time.Time

type Service struct {
	store     port.ProjectModelStore
	repos     RepositoryReader
	projector LegacyProjector
	projects  ProjectLister
	pipelines PipelineJobWriter
	scanner   Scanner
	legacy    port.LegacyModelSource

	deployMatcher DeployMatcher
	environments  port.EnvironmentStore

	// deliveryConfirmedHook wakes the release service when a human confirms a
	// component's delivery profile, so tasks already merged and waiting in
	// done get their release opened without a second scan or merge.
	deliveryConfirmedHook func(ctx context.Context, repositoryID, componentID uuid.UUID)

	now Clock

	mu       sync.Mutex
	inflight map[uuid.UUID]uuid.UUID

	bgCtx context.Context
}

type Deps struct {
	Store     port.ProjectModelStore
	Repos     RepositoryReader
	Projector LegacyProjector
	Projects  ProjectLister
	Pipelines PipelineJobWriter
	Scanner   Scanner
	Legacy    port.LegacyModelSource
}

func NewService(d Deps) *Service {
	return &Service{
		store:     d.Store,
		repos:     d.Repos,
		projector: d.Projector,
		projects:  d.Projects,
		pipelines: d.Pipelines,
		scanner:   d.Scanner,
		legacy:    d.Legacy,
		now:       func() time.Time { return time.Now().UTC() },
		inflight:  make(map[uuid.UUID]uuid.UUID),
		bgCtx:     context.Background(),
	}
}

// SetDeployMatcher wires cloud.Service.MatchScan behind runScan; nil (the
// pre-wiring default) leaves a scan's deploy signals unmatched.
func (s *Service) SetDeployMatcher(m DeployMatcher) { s.deployMatcher = m }

// SetEnvironmentReader wires the same store cloud.Service persists through,
// so RepositoryModel/RepositorySummary/Brief read environments without this
// package depending on cloud.Service itself.
func (s *Service) SetEnvironmentReader(r port.EnvironmentStore) { s.environments = r }

func (s *Service) SetClock(c Clock) { s.now = c }

// SetDeliveryConfirmedHook wires the release service's OpenPending behind an
// interface this package does not otherwise depend on (release depends on
// projectmodel for component lookups, so the reverse import would cycle).
// Nil is the pre-wiring default and a legitimate steady state in any build
// without a release service.
func (s *Service) SetDeliveryConfirmedHook(fn func(ctx context.Context, repositoryID, componentID uuid.UUID)) {
	s.deliveryConfirmedHook = fn
}

// SetBackgroundContext is the process-lifetime context async scans run
// under, so a request's cancellation never kills a scan it started.
func (s *Service) SetBackgroundContext(ctx context.Context) { s.bgCtx = ctx }
