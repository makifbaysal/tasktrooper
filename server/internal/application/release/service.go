// Package release owns the release state machine: opening a release at
// merge, dispatching it, sweeping its deploy and soak window, and the
// verdicts (finish/rollback) that move its tasks on. It depends only on
// small interfaces of its own — never on board or projectmodel directly — so
// board can depend on release (taskpr_merge.go opens a release at merge)
// without a cycle; see Waker for the other direction (waking a parked card),
// which board implements against this package's interface instead.
package release

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Tasks is the board's task read/move surface a release needs: reading a
// task, moving it (with a system reason, the way
// repository/prodgate.go AutoReleaseIfUndeployable does), commenting on it,
// and listing a repository's tasks (OpenPending's catch-up scan). Satisfied
// directly by *repository.Service (GetTask/UpdateTask/AddComment/ListTasks).
type Tasks interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error)
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
	ListTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error)
}

// ParkedTasks claims a card parked on a resource. Satisfied directly by the
// raw postgres board-task store (same instance wired into
// board.NewDeploySweeper today), not by the repository service — parking is
// plumbing, not a gated move.
type ParkedTasks interface {
	TakeBlockedResourceTask(ctx context.Context, resource string, taskID uuid.UUID) (domain.BoardTask, bool, error)
}

// MergeStateResetter clears a task's merge bookkeeping so a rolled-back task
// can go through review again and get a fresh PR. WP-A adds this to the
// postgres board-task store.
type MergeStateResetter interface {
	ResetMergeState(ctx context.Context, taskID uuid.UUID) error
}

// Waker wakes the release engineer's card the way board/deploy_sweeper.go
// wakes a deploy-watch card: dispatched through board.Dispatcher, never
// waited on inline. Implemented in the board package (board.NewReleaseWaker)
// so release need not import board.
type Waker interface {
	Wake(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, releaseStatus domain.ReleaseStatus) error
}

// Components resolves the component a merged task ships as. GetComponent and
// ComponentByPath are satisfied directly by *projectmodel.Service;
// ListComponents needs a one-line wrapper there (it only has the method on
// its internal store today) — see the integration note in the WP-B report.
type Components interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
	ComponentByPath(ctx context.Context, repositoryID uuid.UUID, path string) (domain.Component, error)
	ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error)
}

// Environments is the slice of *cloud.Service a release needs: the
// component's bound environments, its deployments (vercel status matching)
// and its runtime error groups (verify window).
type Environments interface {
	ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error)
	Deployments(ctx context.Context, envID uuid.UUID, limit int) ([]domain.CloudDeployment, error)
	Errors(ctx context.Context, envID uuid.UUID, since time.Time) ([]domain.RuntimeErrorGroup, error)
}

// DeployStatus is WP-F's commit-keyed deploy watch, standing in for the
// concrete *deploywatch.Service so this package never imports it.
type DeployStatus interface {
	StatusForCommit(ctx context.Context, repositoryID uuid.UUID, sha, workflow string) (domain.DeployWatchStatus, error)
}

// Reverter is WP-F's safe default-branch revert, standing in for the
// concrete *git.Client.
type Reverter interface {
	RevertOnDefaultBranch(ctx context.Context, rootPath string, shas []string, message string) (string, error)
}

// Repos resolves a repository by id (root path, name). Satisfied directly by
// *repository.Service.
type Repos interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// IncidentIngester is the same shape deploywatch uses; optional.
type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

// Deps wires the release service. Every field is optional except Store: a
// nil dependency degrades its feature (no component resolved, no HTTP
// probing, no rollback dispatch) rather than panicking, the same contract
// deploywatch.Service holds to.
type Deps struct {
	Store port.ReleaseStore

	Tasks        Tasks
	ParkedTasks  ParkedTasks
	MergeState   MergeStateResetter
	Waker        Waker
	Components   Components
	Environments Environments
	// LegacyTargets is the fallback verify target when the component has no
	// bound production environment.
	LegacyTargets port.DeployTargetStore
	DeployStatus  DeployStatus
	Actions       port.ActionsClient
	Reverter      Reverter
	Repos         Repos
	Incidents     IncidentIngester

	// RepoCoordinates resolves a repository to its GitHub owner/name, the
	// same func type deploywatch.Deps.RepoCoordinates takes (see
	// platform/runtime/runtime.go for how it is built).
	RepoCoordinates func(ctx context.Context, repo domain.Repository) (owner, name string, err error)

	// IsRefAlreadyExists / IsCIUnavailableText classify a port.ActionsClient
	// error (githubapi.IsRefAlreadyExists / githubapi.IsCIUnavailableText).
	// Injected rather than imported: application/release must not import the
	// adapter package that defines them. Both default to "false" when nil.
	IsRefAlreadyExists  func(err error) bool
	IsCIUnavailableText func(text string) bool

	// URLPolicy defaults to urlguard.Default(); tests loosen it
	// (AllowLoopback) to probe an httptest server the way
	// deploywatch_test.go does.
	URLPolicy *urlguard.Policy

	// Clock defaults to time.Now; tests inject a fixed/steppable one so the
	// sweeper's timeouts and soak window are exercised deterministically.
	Clock func() time.Time
}

// Service is the release application service: see package doc.
type Service struct {
	store port.ReleaseStore

	tasks        Tasks
	parked       ParkedTasks
	mergeState   MergeStateResetter
	waker        Waker
	components   Components
	environments Environments
	legacy       port.DeployTargetStore
	deployStatus DeployStatus
	actions      port.ActionsClient
	reverter     Reverter
	repos        Repos
	incidents    IncidentIngester

	coords func(ctx context.Context, repo domain.Repository) (string, string, error)

	refAlreadyExists func(err error) bool
	ciUnavailable    func(text string) bool

	policy urlguard.Policy

	now func() time.Time

	mu             sync.Mutex
	lastErrorCheck map[uuid.UUID]time.Time
}

func New(d Deps) *Service {
	s := &Service{
		store:            d.Store,
		tasks:            d.Tasks,
		parked:           d.ParkedTasks,
		mergeState:       d.MergeState,
		waker:            d.Waker,
		components:       d.Components,
		environments:     d.Environments,
		legacy:           d.LegacyTargets,
		deployStatus:     d.DeployStatus,
		actions:          d.Actions,
		reverter:         d.Reverter,
		repos:            d.Repos,
		incidents:        d.Incidents,
		coords:           d.RepoCoordinates,
		refAlreadyExists: d.IsRefAlreadyExists,
		ciUnavailable:    d.IsCIUnavailableText,
		now:              d.Clock,
		lastErrorCheck:   map[uuid.UUID]time.Time{},
	}
	if s.refAlreadyExists == nil {
		s.refAlreadyExists = func(error) bool { return false }
	}
	if s.ciUnavailable == nil {
		s.ciUnavailable = func(string) bool { return false }
	}
	if d.URLPolicy != nil {
		s.policy = *d.URLPolicy
	} else {
		s.policy = urlguard.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) SetURLPolicy(p urlguard.Policy) { s.policy = p }
