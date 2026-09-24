package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type RepositoryStore interface {
	// kind is written at insert time rather than left to the column default,
	// which mis-typed every imported repo until someone opened pipeline
	// settings; empty falls back to the default.
	Create(ctx context.Context, name, description, rootPath, remoteURL, kind string) (domain.Repository, error)
	// Records the git origin the working copy came from, so a missing RootPath
	// can be restored by cloning.
	UpdateRemoteURL(ctx context.Context, id uuid.UUID, remoteURL string) (domain.Repository, error)
	// Re-points a repository at the working copy actually on disk, after a
	// re-clone onto this machine.
	UpdateRootPath(ctx context.Context, id uuid.UUID, rootPath string) (domain.Repository, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	GetByRootPath(ctx context.Context, rootPath string) (domain.Repository, error)
	List(ctx context.Context) ([]domain.Repository, error)
	// name/description apply unconditionally; each command pointer applies only
	// when non-nil — non-nil "" clears it, nil leaves it unchanged.
	Update(ctx context.Context, id uuid.UUID, name, description string, verifyCommand, buildCommand, testCommand *string, requireHumanReview *bool) (domain.Repository, error)
	UpdateMeta(ctx context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string, autoReleaseOnDone *bool) (domain.Repository, error)
	// Its own setter, like UpdateSubProjects, so one settings writer cannot
	// blank a neighbouring column by omitting it.
	UpdateMobilePlatform(ctx context.Context, id uuid.UUID, platform string) (domain.Repository, error)
	UpdateReleaseEngine(ctx context.Context, id uuid.UUID, engine string) (domain.Repository, error)
	// Written by detection only; prefills what a repository ships under as a
	// deploy target's bundle_id / package_name var.
	UpdateDetectedAppIdentity(ctx context.Context, id uuid.UUID, identity domain.AppIdentity) (domain.Repository, error)
	// Written by detection only; "" travels to the release and refuses it.
	UpdateDetectedBuildTargets(ctx context.Context, id uuid.UUID, targets domain.BuildTargets) (domain.Repository, error)
	UpdateMutationGate(ctx context.Context, id uuid.UUID, enabled *bool, threshold *float64) (domain.Repository, error)
	SetDocsTaskID(ctx context.Context, id uuid.UUID, taskID string) error
	// Separate from UpdateMeta: sub_repo_kinds is the pipeline's routing key
	// set, and a writer of one must not be able to blank the other.
	UpdateSubProjects(ctx context.Context, id uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error)
	UpdateDocs(ctx context.Context, id uuid.UUID, docs domain.RepositoryDocs) (domain.Repository, error)
	UpdateIncidentPolicy(ctx context.Context, id uuid.UUID, policy domain.IncidentPolicy) (domain.Repository, error)
	UpdateTestStrategy(ctx context.Context, id uuid.UUID, strategy string) (domain.Repository, error)
	// Each of the five is applied only when its pointer is non-nil. The third
	// gates a DISPATCH (reviewer waits for CI), not a MOVE, but is set here
	// because it is the same kind of per-repository switch. New-code coverage
	// is deliberately not here: it is the bar every change is held to, not a
	// per-repository choice.
	UpdateLifecycleGates(ctx context.Context, id uuid.UUID, requireReviewChain, requireReleaseDeploy, requirePipelineForReview, requireOverallCoverage *bool, coverageThreshold *float64) (domain.Repository, error)
	// The secret must never appear in any API response.
	SetWebhook(ctx context.Context, id uuid.UUID, secret string, hookID int64) error
	WebhookSecret(ctx context.Context, id uuid.UUID) (string, error)
	Delete(ctx context.Context, id uuid.UUID) error
	SetProjects(ctx context.Context, repositoryID uuid.UUID, projectIDs []uuid.UUID) error
	ListProjectIDs(ctx context.Context, repositoryID uuid.UUID) ([]uuid.UUID, error)
	ListProjectIDsByRepositories(ctx context.Context, repositoryIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

// RepositoryPipelineJobStore persists the per-(sub-repo, category) mapping of
// pipeline categories to GitHub Actions jobs/workflows.
type RepositoryPipelineJobStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error)
	// Feeds the operations matrix, which needs every repository's mapping in
	// one query rather than one per repository.
	ListAll(ctx context.Context) ([]domain.RepositoryPipelineJob, error)
	ReplaceForRepository(ctx context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error)
}

type BoardTaskStore interface {
	Create(ctx context.Context, task domain.BoardTask) (domain.BoardTask, error)
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	GetByNumber(ctx context.Context, number int) (domain.BoardTask, error)
	LookupByKey(ctx context.Context, keyPrefix string, number int) (domain.BoardTask, error)
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error)
	ListAll(ctx context.Context) ([]domain.BoardTask, error)
	// ListAll minus released tasks that entered that column before
	// releasedCutoff, which is passed in so the window stays a policy of the
	// caller (domain.ReleasedBoardWindow) and the store stays a query.
	ListBoardVisible(ctx context.Context, releasedCutoff time.Time) ([]domain.BoardTask, error)
	// The other half: released tasks newest first, with an optional
	// case-insensitive search over key, title and description.
	ListReleasedArchive(ctx context.Context, query string, limit int) ([]domain.BoardTask, error)
	Update(ctx context.Context, task domain.BoardTask) (domain.BoardTask, error)
	Delete(ctx context.Context, repositoryID, taskID uuid.UUID) error
	ClaimAssignee(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) (domain.BoardTask, error)
	MarkCompleted(ctx context.Context, taskID uuid.UUID, clean bool, at time.Time) error
	// Detected from the changed files, not declared by the agent.
	SetMigrationFlag(ctx context.Context, taskID uuid.UUID, hasMigration bool) error
	// number may be 0 when the URL did not parse — the link is still worth
	// keeping.
	SetTaskPullRequest(ctx context.Context, taskID uuid.UUID, url string, number int) error
	// The board's only local record that the PR landed; the deploy that follows
	// a merge watches this commit rather than a branch name.
	SetTaskMergeCommit(ctx context.Context, taskID uuid.UUID, sha string) error
	// The reverse lookup that turns "some deploy failed" into a specific card,
	// for attribute-release. Returns port.ErrNotFound when no task claims the
	// commit (a human merge, or pre-migration).
	FindTaskByMergeCommit(ctx context.Context, repositoryID uuid.UUID, sha string) (domain.BoardTask, error)
	MarkStageVerified(ctx context.Context, taskID uuid.UUID, at time.Time) error
	// Re-arms the migration gate when new commits bring the task back through
	// review — the old stamp proves nothing about them.
	ClearStageVerification(ctx context.Context, taskID uuid.UUID) error
	NextTaskNumber(ctx context.Context, taskType domain.TaskType) (int, error)
	BlockOnQuestion(ctx context.Context, repositoryID, taskID, sessionID uuid.UUID, question string) error
	TakeBlockedBySession(ctx context.Context, sessionID uuid.UUID) (domain.BoardTask, bool, error)
	// Returns the column the task was in before the park, which the caller
	// writes as from_column on the moved event. Nothing announces a freed
	// device, so the release is a timed sweep, not an inbound event.
	BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error)
	TakeBlockedByResource(ctx context.Context, resource string) (domain.BoardTask, bool, error)
	// The deploy watch's pair: per-task rather than a queue, because two tasks
	// wait on two different deploys. The sweeper lists (read-only), asks GitHub
	// about each, and claims only the ones whose deploy has settled.
	ListBlockedByResource(ctx context.Context, resource string, limit int) ([]domain.BoardTask, error)
	TakeBlockedResourceTask(ctx context.Context, resource string, taskID uuid.UUID) (domain.BoardTask, bool, error)
	// The work_order pair, and the only one that does NOT move board_column: a
	// `blocks` wait is another card on the same board, so parking it in the
	// shared `blocked` column would hide it from a todo/in_progress view.
	MarkWorkOrderWaiting(ctx context.Context, repositoryID, taskID uuid.UUID, detail string) error
	ClearWorkOrderWaiting(ctx context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error)
	// The quota release half: the run that parked recorded the expected reset
	// time on its own row, so the due check and the claim are one statement.
	TakeQuotaResumable(ctx context.Context, now time.Time) (domain.BoardTask, bool, error)
	BlockOnCancel(ctx context.Context, repositoryID, taskID uuid.UUID, reason string) error
}

type AcceptanceCriterionStore interface {
	ReplaceForTask(ctx context.Context, taskID uuid.UUID, items []domain.AcceptanceCriterionInput) ([]domain.AcceptanceCriterion, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error)
	UpdateCompleted(ctx context.Context, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error)
	// The stored reason separates a decision from an omission.
	UpdateCanceled(ctx context.Context, criterionID uuid.UUID, canceled bool, reason string) (domain.AcceptanceCriterion, error)
	GetCriterion(ctx context.Context, criterionID uuid.UUID) (domain.AcceptanceCriterion, error)
	UpsertCheck(ctx context.Context, check domain.CriterionCheck) (domain.CriterionCheck, error)
}

// TaskTestCaseStore holds the test round a task was actually given. Keys are
// matched by (task, title) rather than by id on purpose: a round writes its
// case list first and comes back to each case as it executes it.
type TaskTestCaseStore interface {
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error)
	// Writes a batch matched by title; never deletes, because a case the round
	// stopped mentioning is a case that was tested once.
	UpsertForTask(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error)
	// The human's editor path — the drawer sends the list it shows, including
	// deletions.
	ReplaceForTask(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error)
	Get(ctx context.Context, id uuid.UUID) (domain.TaskTestCase, error)
	Update(ctx context.Context, id uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// Stamps cases already turned into a performance score event, so a rework
	// round never counts them twice.
	MarkScored(ctx context.Context, ids []uuid.UUID, at time.Time) error
}

type TaskRelationStore interface {
	ReplaceForTask(ctx context.Context, sourceTaskID uuid.UUID, relations []domain.TaskRelationInput) ([]domain.TaskRelation, error)
	// Swaps only the relations of one type, so editing deploy order must not
	// silently drop the task's blocks relations.
	ReplaceForTaskOfType(ctx context.Context, sourceTaskID uuid.UUID, relationType domain.TaskRelationType, relations []domain.TaskRelationInput) ([]domain.TaskRelation, error)
	ListBySource(ctx context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error)
	// The UNFINISHED sources of this task's blocks rows; empty is the whole
	// "may this task start" answer, so the column filter lives in the query.
	ListBlockingSources(ctx context.Context, targetTaskID uuid.UUID) ([]domain.BoardTask, error)
	// Same edges as relations unfiltered by column, so a card can show
	// "blocked by T-3 (done)" — the reverse-direction twin of ListBySource.
	ListBlockedBy(ctx context.Context, targetTaskID uuid.UUID) ([]domain.TaskRelation, error)
	// Writes `blocks` rows whose TARGET is this task — the direction migration
	// 022 stores them in. Additive and idempotent.
	AddBlockers(ctx context.Context, targetTaskID uuid.UUID, sourceTaskIDs []uuid.UUID) ([]domain.TaskRelation, error)
	// Every `blocks` edge whose SOURCE is unfinished, in one query — the bulk
	// twin that keeps the ready-tasks query from doing N+1.
	ListUnfinishedBlockers(ctx context.Context) ([]domain.TaskRelation, error)
}

// DeployPackageStore persists release trains and their membership.
type DeployPackageStore interface {
	Create(ctx context.Context, pkg domain.DeployPackage) (domain.DeployPackage, error)
	Get(ctx context.Context, repositoryID, packageID uuid.UUID) (domain.DeployPackage, error)
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployPackage, error)
	// Applies each field only where the pointer is non-nil, so the release
	// path can move status without touching a name a human is editing.
	Update(ctx context.Context, repositoryID, packageID uuid.UUID, name, description, status, note *string) (domain.DeployPackage, error)
	Delete(ctx context.Context, repositoryID, packageID uuid.UUID) error
	ReplaceTasks(ctx context.Context, packageID uuid.UUID, taskIDs []uuid.UUID) error
	ListTasks(ctx context.Context, packageID uuid.UUID) ([]domain.DeployPackageTask, error)
}

type TaskDocumentStore interface {
	Create(ctx context.Context, doc domain.TaskDocument) (domain.TaskDocument, error)
	Get(ctx context.Context, taskID, docID uuid.UUID) (domain.TaskDocument, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskDocument, error)
	Update(ctx context.Context, doc domain.TaskDocument) (domain.TaskDocument, error)
	Delete(ctx context.Context, taskID, docID uuid.UUID) error
}

type InitiativeProjectStore interface {
	Create(ctx context.Context, name, description string) (domain.InitiativeProject, error)
	Get(ctx context.Context, id uuid.UUID) (domain.InitiativeProject, error)
	List(ctx context.Context) ([]domain.InitiativeProject, error)
	Update(ctx context.Context, id uuid.UUID, name, description string) (domain.InitiativeProject, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
