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
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
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
	// ListBlockedByResource reads (claims nothing) every task currently
	// parked on a resource — the M2 watchdog's source of truth for a park
	// hand-back never freed (a race, a crash, a dropped dispatch).
	ListBlockedByResource(ctx context.Context, resource string, limit int) ([]domain.BoardTask, error)
}

// MergeStateResetter clears a task's merge bookkeeping so a rolled-back task
// can go through review again and get a fresh PR.
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
// its internal store today).
type Components interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
	ComponentByPath(ctx context.Context, repositoryID uuid.UUID, path string) (domain.Component, error)
	ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error)
}

// Environments is the slice of *cloud.Service a release needs: the
// component's bound environments, its deployments (vercel status matching)
// and its runtime error groups (verify window), plus the provider-rollback
// capability (CanRollback/CurrentDeployment/RollbackEnvironment/
// PromoteDeployment) *cloud.Service provides. A provider without the
// capability answers CanRollback false and the release falls back to the
// pushed revert, so every method here degrades rather than being required.
type Environments interface {
	ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error)
	Deployments(ctx context.Context, envID uuid.UUID, limit int) ([]domain.CloudDeployment, error)
	Errors(ctx context.Context, envID uuid.UUID, since time.Time) ([]domain.RuntimeErrorGroup, error)
	CanRollback(ctx context.Context, envID uuid.UUID) bool
	CurrentDeployment(ctx context.Context, envID uuid.UUID) (domain.CloudDeployment, error)
	RollbackEnvironment(ctx context.Context, envID uuid.UUID, deploymentID string) error
	PromoteDeployment(ctx context.Context, envID uuid.UUID, deploymentID string) error
}

// BeforeDeployConfirmer stamps a task's before-deploy confirmation the same
// way a human's confirm button does — used by Cut, where cutting the batch
// release IS the human's confirmation for every task it carries. Implemented
// by *repository.Service.
type BeforeDeployConfirmer interface {
	ConfirmBeforeDeploy(ctx context.Context, repositoryID, taskID uuid.UUID) error
}

// DeployStatus is the commit-keyed deploy watch, standing in for the
// concrete *deploywatch.Service so this package never imports it.
type DeployStatus interface {
	StatusForCommit(ctx context.Context, repositoryID uuid.UUID, sha, workflow string) (domain.DeployWatchStatus, error)
	// StatusForCommitSince is StatusForCommit narrowed to Actions runs
	// created at/after since — the rolling_back sweep's redeploy watch (see
	// rollback.go), which reuses the SAME sha/workflow an earlier release or
	// attempt already ran and must not read that other run as its own.
	StatusForCommitSince(ctx context.Context, repositoryID uuid.UUID, sha, workflow string, since time.Time) (domain.DeployWatchStatus, error)
}

// Reverter is the safe default-branch revert, standing in for the
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

// Git is the read-only surface a batch cut needs: the default branch's remote
// head, whether a task's merge commit is on it, and the newest tag matching a
// glob (the previous version, when nothing was ever released through this
// service). All three run read-only plumbing and never touch rootPath's
// working tree or index — see git.Client's doc comments on
// RemoteHead/IsAncestor/LatestTag.
type Git interface {
	RemoteHead(ctx context.Context, rootPath string) (string, error)
	IsAncestor(ctx context.Context, rootPath, ancestor, descendant string) (bool, error)
	LatestTag(ctx context.Context, rootPath, glob string) (string, error)
}

// LocalRunSpec is one batch release's local build-and-publish command.
type LocalRunSpec struct {
	RootPath  string
	CommitSHA string
	Argv      []string
	Env       []string
	LogPath   string
	Timeout   time.Duration
}

// LocalRunner runs a batch release's local command in a detached worktree of
// CommitSHA. Start returns once the process has started (or failed to) so the
// caller can record it immediately; done runs later, from a goroutine, once
// the process exits or is killed on timeout. Implemented by
// adapter/localexec.Runner.
type LocalRunner interface {
	Start(ctx context.Context, run LocalRunSpec, done func(exitCode int, tail string, err error)) error
}

// Store is the storeops slice a batch store release needs: the repository's
// linked apps, a fresh read of their tracks (the baseline/new internal-channel
// build), and starting a release build. Named Store (not StoreOps) in Deps
// would collide with the ReleaseStore field, so it is StoreOps there; the
// interface itself is satisfied directly by *storeops.Service.
type Store interface {
	AppsByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error)
	Tracks(ctx context.Context, repositoryID uuid.UUID, platform string) (domain.StoreTracks, error)
	StartBuild(ctx context.Context, repositoryID uuid.UUID, platform, engine, actor string) (storeops.BuildStart, error)
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
	// BeforeDeploy confirms a task's before-deploy steps from Cut; nil skips
	// the stamp.
	BeforeDeploy BeforeDeployConfirmer
	// DeployOrder and Locator enforce deploy_depends_on; nil lets tasks ship
	// in any order.
	DeployOrder DeployOrder
	Locator     TaskLocator

	// HealthWindow is how long after a release's FinishedAt/DeployedAt a
	// production incident is still attributed to it; defaults to
	// DefaultHealthWindow (15m), the same value deploywatch.Service uses for
	// its own (task-keyed) attribution.
	HealthWindow time.Duration

	// Git, LocalRunner, StoreOps and DataDir are only needed for batch
	// releases (cut preview/cut, and the three batch executors); nil/empty
	// degrades the same way the rest of Deps does.
	Git         Git
	LocalRunner LocalRunner
	StoreOps    Store
	// DataDir is the root a local batch release's log file is written under
	// (releases/<release-id>.log).
	DataDir string

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
	beforeDeploy BeforeDeployConfirmer
	deployOrder  DeployOrder
	locator      TaskLocator

	healthWindow time.Duration

	git         Git
	localRunner LocalRunner
	storeOps    Store
	dataDir     string

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
		beforeDeploy:     d.BeforeDeploy,
		deployOrder:      d.DeployOrder,
		locator:          d.Locator,
		healthWindow:     d.HealthWindow,
		git:              d.Git,
		localRunner:      d.LocalRunner,
		storeOps:         d.StoreOps,
		dataDir:          d.DataDir,
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
	if s.healthWindow <= 0 {
		s.healthWindow = DefaultHealthWindow
	}
	return s
}

func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) SetURLPolicy(p urlguard.Policy) { s.policy = p }

// ForAgent is ForTask stamped with AgentSeenAt: every release tool
// (adapter/tools/board/release_tools.go) resolves through this, not ForTask
// directly, because the hand-back watchdog's re-wake gate (N5) reads
// AgentSeenAt to tell "an agent is actively looking at this" apart from "the
// hand-back dispatch was dropped" — only a live tool call can make that
// distinction, a background read cannot.
func (s *Service) ForAgent(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.Release, error) {
	r, err := s.ForTask(ctx, repositoryID, taskID)
	if err != nil {
		return domain.Release{}, err
	}
	now := s.now()
	if err := s.store.MarkAgentSeen(ctx, r.ID, now); err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: marking a release agent-seen failed")
		return r, nil
	}
	r.AgentSeenAt = &now
	return r, nil
}
