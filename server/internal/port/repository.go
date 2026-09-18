package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type RepositoryStore interface {
	// Create registers a repository. kind is one of domain.RepoKind* and is
	// written at insert time rather than left to the column default: the
	// default made every imported repo a "backend" until someone opened
	// pipeline settings, which mis-picked deploy templates and pipeline
	// keywords in the meantime. Empty falls back to the column default.
	Create(ctx context.Context, name, description, rootPath, remoteURL, kind string) (domain.Repository, error)
	// UpdateRemoteURL records (or backfills) the git origin the working copy
	// came from, so a missing RootPath can be restored by cloning.
	UpdateRemoteURL(ctx context.Context, id uuid.UUID, remoteURL string) (domain.Repository, error)
	// UpdateRootPath re-points a repository at the working copy that is
	// actually on disk. Used when a registered repository is re-cloned onto
	// this machine: the recorded path belonged to a runtime that no longer
	// holds the code, and every consumer reaches the tree through this column.
	UpdateRootPath(ctx context.Context, id uuid.UUID, rootPath string) (domain.Repository, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	GetByRootPath(ctx context.Context, rootPath string) (domain.Repository, error)
	List(ctx context.Context) ([]domain.Repository, error)
	// Update applies name/description unconditionally (empty description
	// clears it, per existing behavior) and applies verifyCommand/
	// buildCommand/testCommand only when non-nil — a non-nil empty string
	// clears the command, nil leaves it unchanged.
	Update(ctx context.Context, id uuid.UUID, name, description string, verifyCommand, buildCommand, testCommand *string, requireHumanReview *bool) (domain.Repository, error)
	// UpdateMeta applies the repo type fields (kind / sub-repo kinds /
	// auto-release toggle) only when the corresponding pointer is non-nil.
	UpdateMeta(ctx context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string, autoReleaseOnDone *bool) (domain.Repository, error)
	// UpdateMobilePlatform records which platform a mobile repository targets
	// (domain.MobilePlatform*, "" = unset). Separate from UpdateMeta for the
	// same reason UpdateSubProjects is: import detection and a settings form
	// each write one of these, and neither may blank the other.
	UpdateMobilePlatform(ctx context.Context, id uuid.UUID, platform string) (domain.Repository, error)
	// UpdateReleaseEngine records where the repository's mobile releases are
	// built and uploaded (domain.ReleaseEngine*). Its own setter for the same
	// reason UpdateMobilePlatform is: it is one field of a settings form and
	// must not blank a neighbouring column by omitting it.
	UpdateReleaseEngine(ctx context.Context, id uuid.UUID, engine string) (domain.Repository, error)
	// UpdateDetectedAppIdentity records the store identifiers read off the
	// working copy at import (either half "" = could not be read). Written by
	// detection only — what a repository actually ships under is a deploy
	// target's bundle_id / package_name var, which this prefills.
	UpdateDetectedAppIdentity(ctx context.Context, id uuid.UUID, identity domain.AppIdentity) (domain.Repository, error)
	// UpdateDetectedBuildTargets records the Xcode scheme / Gradle module read
	// off the working copy at import (either half "" = could not be read).
	// Its own setter beside UpdateDetectedAppIdentity rather than part of it:
	// the two answer different questions and are written by the same detection
	// pass only by coincidence, and one that reads nothing must not blank the
	// other. Written by detection only — unlike the identity pair there is no
	// form behind these, so "" travels to the release and refuses it.
	UpdateDetectedBuildTargets(ctx context.Context, id uuid.UUID, targets domain.BuildTargets) (domain.Repository, error)
	// UpdateMutationGate arms/disarms the mutation-score bar and sets the
	// percentage it is judged against, each only when its pointer is non-nil.
	// It is the mutation twin of the coverage pair on UpdateLifecycleGates.
	UpdateMutationGate(ctx context.Context, id uuid.UUID, enabled *bool, threshold *float64) (domain.Repository, error)
	// SetDocsTaskID records the reference-doc bundle task the repository is
	// waiting on, or clears it with "".
	SetDocsTaskID(ctx context.Context, id uuid.UUID, taskID string) error
	// UpdateSubProjects replaces the monorepo sub-project list wholesale.
	// Separate from UpdateMeta: sub_repo_kinds is the pipeline's routing key
	// set, written by the pipeline settings page and by profile proposals, and
	// a writer of one must not be able to blank the other.
	UpdateSubProjects(ctx context.Context, id uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error)
	// UpdateDocs replaces the repository's own reference-doc pointers
	// (coding standards / test standards / architecture / local run)
	// wholesale. Sub-project docs travel inside UpdateSubProjects instead —
	// they live on the RepoSubProject struct, not as a separate column.
	UpdateDocs(ctx context.Context, id uuid.UUID, docs domain.RepositoryDocs) (domain.Repository, error)
	// UpdateIncidentPolicy sets what a production incident triggers for the
	// repo (off / suggest / auto_fix).
	UpdateIncidentPolicy(ctx context.Context, id uuid.UUID, policy domain.IncidentPolicy) (domain.Repository, error)
	// UpdateTestStrategy sets how the repo's tasks are verified before they
	// move on (local / stage / per_step).
	UpdateTestStrategy(ctx context.Context, id uuid.UUID, strategy string) (domain.Repository, error)
	// UpdateLifecycleGates arms or disarms the repository's stage gates: the
	// review chain before done, the production deploy before released, and
	// whether code_review waits for CI before its reviewer is dispatched. Each
	// is applied only when its pointer is non-nil, so one can be changed
	// without touching the others.
	//
	// The third one is not a terminal-column gate like the first two — it gates
	// a DISPATCH, not a MOVE — but it lives here because it is the same kind of
	// object to the person setting it (a per-repository switch on how strict
	// the board is) and because the settings screen that owns the other two
	// saves all of them with one button.
	//
	// The last two are the coverage settings: whether the WHOLE-REPO figure
	// gates at all, and the number it gates against. New-code coverage is not
	// here because it is not a per-repository choice — it is the bar every
	// change is held to, and a repo that could opt out of it would be a repo
	// where an untested diff reaches code review.
	UpdateLifecycleGates(ctx context.Context, id uuid.UUID, requireReviewChain, requireReleaseDeploy, requirePipelineForReview, requireOverallCoverage *bool, coverageThreshold *float64) (domain.Repository, error)
	// UpdateProfile replaces the agent-maintained project profile markdown and
	// stamps profile_updated_at = now().
	UpdateProfile(ctx context.Context, id uuid.UUID, profileMD string) (domain.Repository, error)
	// SetWebhook stores the GitHub push-webhook secret (encrypted at rest)
	// together with the hook id GitHub assigned. WebhookSecret returns the
	// decrypted secret, or "" when no webhook is set up. The secret must never
	// appear in any API response.
	SetWebhook(ctx context.Context, id uuid.UUID, secret string, hookID int64) error
	WebhookSecret(ctx context.Context, id uuid.UUID) (string, error)
	Delete(ctx context.Context, id uuid.UUID) error
	SetProjects(ctx context.Context, repositoryID uuid.UUID, projectIDs []uuid.UUID) error
	ListProjectIDs(ctx context.Context, repositoryID uuid.UUID) ([]uuid.UUID, error)
	ListProjectIDsByRepositories(ctx context.Context, repositoryIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

// RepositoryProfileStore holds the sectioned project profile: the parser-owned
// facts, the agent-written judgment sections with their evidence, and the
// settings proposals a profiling pass produces.
type RepositoryProfileStore interface {
	ListSections(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileSection, error)
	// UpsertSections replaces the named sections only; every other section of
	// the repository is left as it was, which is what makes a scoped refresh
	// possible.
	UpsertSections(ctx context.Context, repositoryID uuid.UUID, sections []domain.ProfileSection) error
	DeleteSections(ctx context.Context, repositoryID uuid.UUID, sections []string) error
	// MarkStale flags the sections whose source files appear in changedPaths
	// and returns their names — the work list for a scoped refresh.
	MarkStale(ctx context.Context, repositoryID uuid.UUID, changedPaths []string) ([]string, error)
	ListProposals(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileProposal, error)
	ReplaceProposals(ctx context.Context, repositoryID uuid.UUID, proposals []domain.ProfileProposal) error
	GetProposal(ctx context.Context, id uuid.UUID) (domain.ProfileProposal, error)
	SetProposalStatus(ctx context.Context, id uuid.UUID, status domain.ProfileProposalStatus) (domain.ProfileProposal, error)
}

// RepositoryPipelineJobStore persists the per-(sub-repo, category) mapping of
// pipeline categories to GitHub Actions jobs/workflows.
type RepositoryPipelineJobStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error)
	// ListAll feeds the operations matrix, which needs every repository's
	// mapping in one query rather than one query per repository.
	ListAll(ctx context.Context) ([]domain.RepositoryPipelineJob, error)
	// ReplaceForRepository atomically replaces the full mapping set for a repo.
	ReplaceForRepository(ctx context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error)
}

type BoardTaskStore interface {
	Create(ctx context.Context, task domain.BoardTask) (domain.BoardTask, error)
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	GetByNumber(ctx context.Context, number int) (domain.BoardTask, error)
	LookupByKey(ctx context.Context, keyPrefix string, number int) (domain.BoardTask, error)
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error)
	ListAll(ctx context.Context) ([]domain.BoardTask, error)
	// ListBoardVisible is ListAll minus the released tasks that entered that
	// column before releasedCutoff — what the board renders. The cutoff is
	// passed in rather than baked in here so the window stays a policy of the
	// caller (domain.ReleasedBoardWindow) and the store stays a query.
	ListBoardVisible(ctx context.Context, releasedCutoff time.Time) ([]domain.BoardTask, error)
	// ListReleasedArchive is the other half: released tasks newest first, with
	// an optional case-insensitive search over key, title and description. It
	// is how work that has left the board is found again.
	ListReleasedArchive(ctx context.Context, query string, limit int) ([]domain.BoardTask, error)
	Update(ctx context.Context, task domain.BoardTask) (domain.BoardTask, error)
	Delete(ctx context.Context, repositoryID, taskID uuid.UUID) error
	ClaimAssignee(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) (domain.BoardTask, error)
	// MarkCompleted stamps the terminal verdict time-based KPIs read: when the
	// task finished, and whether it got there without rework.
	MarkCompleted(ctx context.Context, taskID uuid.UUID, clean bool, at time.Time) error
	// SetMigrationFlag records whether the task's diff carries a schema change
	// (detected from the changed files, not declared by the agent).
	SetMigrationFlag(ctx context.Context, taskID uuid.UUID, hasMigration bool) error
	// SetTaskPullRequest records the pull request the task's branch is reviewed
	// in. Called from every path that learns a PR URL, so the board can name the
	// PR without a working copy and a GitHub round-trip. number may be 0 when the
	// URL did not parse — the link is still worth keeping (see
	// domain.ParsePullRequestNumber).
	SetTaskPullRequest(ctx context.Context, taskID uuid.UUID, url string, number int) error
	// SetTaskMergeCommit records the squash commit the task's pull request
	// produced when it was merged. It is the board's only local record that the
	// PR landed — the dispatcher reads it to decide whether a task in done still
	// needs the QA agent woken to merge, and the deploy that follows a merge
	// should watch this commit rather than a branch name (see
	// domain.BoardTask.MergeCommitSHA).
	SetTaskMergeCommit(ctx context.Context, taskID uuid.UUID, sha string) error
	// FindTaskByMergeCommit is the reverse lookup: given a commit that is live
	// in an environment, which task put it there. It is what turns a generic
	// "some deploy failed" incident into a specific card with a rollback plan
	// and an owning agent (application/deploywatch.AttributeRelease). Returns
	// port.ErrNotFound when no task claims the commit — a merge made by a
	// human, or a commit from before migration 104.
	FindTaskByMergeCommit(ctx context.Context, repositoryID uuid.UUID, sha string) (domain.BoardTask, error)
	// MarkStageVerified stamps the successful stage deploy of this task.
	MarkStageVerified(ctx context.Context, taskID uuid.UUID, at time.Time) error
	// ClearStageVerification re-arms the migration gate when new commits bring
	// the task back through review — the old stamp proves nothing about them.
	ClearStageVerification(ctx context.Context, taskID uuid.UUID) error
	NextTaskNumber(ctx context.Context, taskType domain.TaskType) (int, error)
	// BlockOnQuestion / TakeBlockedBySession park a task on the clarification
	// chat an agent opened and release it once the human answers there.
	BlockOnQuestion(ctx context.Context, repositoryID, taskID, sessionID uuid.UUID, question string) error
	TakeBlockedBySession(ctx context.Context, sessionID uuid.UUID) (domain.BoardTask, bool, error)
	// BlockOnResource / TakeBlockedByResource are the same pair for a task
	// waiting on shared hardware rather than on a human. Nothing announces a
	// freed device, so the release is a timed sweep, not an inbound event —
	// see application/board/device_sweeper.go.
	//
	// BlockOnResource returns the column the task was in before the park, which
	// is what the caller writes as from_column on the task.moved event that
	// makes the park visible on the board. It is "" when the task no longer
	// exists.
	BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error)
	TakeBlockedByResource(ctx context.Context, resource string) (domain.BoardTask, bool, error)
	// ListBlockedByResource / TakeBlockedResourceTask are the pair the deploy
	// watch needs, and they exist because that park is per-TASK where the
	// device's is a queue.
	//
	// TakeBlockedByResource hands back the OLDEST parked task on a global
	// probe, which is right for one shared phone: any freed device releases any
	// waiting task. Two tasks waiting on two deploys are waiting on two
	// different things, and the older one's deploy may still be running while
	// the newer one's has already failed. So the deploy sweeper lists the
	// parked tasks (read-only, claiming nothing), asks GitHub about each one,
	// and claims only the ones whose deploy has settled.
	ListBlockedByResource(ctx context.Context, resource string, limit int) ([]domain.BoardTask, error)
	TakeBlockedResourceTask(ctx context.Context, resource string, taskID uuid.UUID) (domain.BoardTask, bool, error)
	// MarkWorkOrderWaiting / ClearWorkOrderWaiting are the work_order resource's
	// own park pair, and the only one that does NOT move board_column. A `blocks`
	// wait is not waiting on anything outside the board — the blocker is another
	// card on the same board — so parking it into the shared `blocked` column
	// would hide it from a todo/in_progress view for no reason the other four
	// resources share. The task stays exactly where it was; only
	// blocked_resource/blocked_question/blocked_at move.
	MarkWorkOrderWaiting(ctx context.Context, repositoryID, taskID uuid.UUID, detail string) error
	ClearWorkOrderWaiting(ctx context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error)
	// TakeQuotaResumable is the release half for the OTHER thing a task can
	// park on: the local Claude Code subscription's usage limit
	// (domain.ResourceClaudeCodeQuota). It cannot go through
	// TakeBlockedByResource because nothing can be asked whether a quota has
	// reset — the run that parked recorded the expected time on its own row, so
	// the due check and the claim have to be one statement. See
	// application/board/quota_sweeper.go.
	TakeQuotaResumable(ctx context.Context, now time.Time) (domain.BoardTask, bool, error)
	// BlockOnCancel parks a task whose run a human stopped. Same parking spot as
	// BlockOnQuestion but with no session to answer into: there is no question,
	// so the way out is a human moving the task to a column, which Update()
	// clears the block for.
	BlockOnCancel(ctx context.Context, repositoryID, taskID uuid.UUID, reason string) error
}

type AcceptanceCriterionStore interface {
	ReplaceForTask(ctx context.Context, taskID uuid.UUID, items []domain.AcceptanceCriterionInput) ([]domain.AcceptanceCriterion, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error)
	UpdateCompleted(ctx context.Context, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error)
	// UpdateCanceled drops a criterion from the task's scope, or restores it.
	// The reason is stored with it: a criterion may only leave the list by
	// saying why, which is what separates a decision from an omission.
	UpdateCanceled(ctx context.Context, criterionID uuid.UUID, canceled bool, reason string) (domain.AcceptanceCriterion, error)
	// GetCriterion resolves a single criterion so the review path can find the
	// task (and thus the column, which decides the reviewer role).
	GetCriterion(ctx context.Context, criterionID uuid.UUID) (domain.AcceptanceCriterion, error)
	// UpsertCheck records one role's verdict on one criterion, overwriting the
	// role's previous verdict for that criterion.
	UpsertCheck(ctx context.Context, check domain.CriterionCheck) (domain.CriterionCheck, error)
}

// TaskTestCaseStore holds the test round a task was actually given: every case
// derived from the request, with its verdict.
//
// Upsert is by (task, title) rather than by id on purpose. A round writes its
// case list first and comes back to each case as it executes it, and an agent
// that had to carry ids between those two moments would either re-read the list
// on every result or invent an id — both of which it demonstrably does badly.
type TaskTestCaseStore interface {
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error)
	// UpsertForTask writes a batch of cases, matching existing rows by title.
	// It never deletes: a case the round stopped mentioning is a case that was
	// tested once, and losing it would hide exactly the history this list is for.
	UpsertForTask(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error)
	// ReplaceForTask is the human's editor path — the drawer sends the list it
	// shows, including deletions.
	ReplaceForTask(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error)
	Get(ctx context.Context, id uuid.UUID) (domain.TaskTestCase, error)
	Update(ctx context.Context, id uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// MarkScored stamps cases as already turned into a performance score
	// event, so a rework round or a later forward exit never counts them twice.
	MarkScored(ctx context.Context, ids []uuid.UUID, at time.Time) error
}

type TaskRelationStore interface {
	ReplaceForTask(ctx context.Context, sourceTaskID uuid.UUID, relations []domain.TaskRelationInput) ([]domain.TaskRelation, error)
	// ReplaceForTaskOfType swaps only the relations of one type, leaving every
	// other type on the task alone. Editing deploy order must not silently drop
	// the task's blocks relations, which a full ReplaceForTask would do.
	ReplaceForTaskOfType(ctx context.Context, sourceTaskID uuid.UUID, relationType domain.TaskRelationType, relations []domain.TaskRelationInput) ([]domain.TaskRelation, error)
	ListBySource(ctx context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error)
	// ListBlockingSources returns the UNFINISHED tasks that block this one —
	// the sources of its `blocks` rows that have not reached done or released.
	// An empty result is the whole "may this task start" answer, which is why
	// the column filter lives in the query rather than in every caller.
	ListBlockingSources(ctx context.Context, targetTaskID uuid.UUID) ([]domain.BoardTask, error)
	// ListBlockedBy returns the same edges as relations rather than as tasks,
	// unfiltered by column, so a card can show "blocked by T-3 (done)" instead
	// of showing nothing once the blocker lands. It is the reverse-direction
	// twin of ListBySource: relations whose TARGET is this task.
	ListBlockedBy(ctx context.Context, targetTaskID uuid.UUID) ([]domain.TaskRelation, error)
	// AddBlockers writes `blocks` rows whose TARGET is this task and whose
	// sources are the blockers, which is the direction migration 022 stores
	// them in. It cannot go through ReplaceForTask*, because those key on a
	// single source and would delete that source's OTHER relations as a side
	// effect of declaring one blocker.
	//
	// Additive and idempotent: an edge that already exists is left alone rather
	// than duplicated or reported as an error.
	AddBlockers(ctx context.Context, targetTaskID uuid.UUID, sourceTaskIDs []uuid.UUID) ([]domain.TaskRelation, error)
	// ListUnfinishedBlockers returns every `blocks` edge in the board whose
	// SOURCE has not reached done or released, in one query. It is the bulk
	// twin of ListBlockingSources: the ready-tasks query runs this once over
	// the whole board on every call, and asking ListBlockingSources per task
	// would turn that into N+1 queries.
	ListUnfinishedBlockers(ctx context.Context) ([]domain.TaskRelation, error)
}

// DeployPackageStore persists release trains and their membership. Kept
// separate from BoardTaskStore because a package is a repository-level object
// that happens to reference tasks, not a property of any one task.
type DeployPackageStore interface {
	Create(ctx context.Context, pkg domain.DeployPackage) (domain.DeployPackage, error)
	Get(ctx context.Context, repositoryID, packageID uuid.UUID) (domain.DeployPackage, error)
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployPackage, error)
	// Update applies name/description/status/note only where the pointer is
	// non-nil, so the release path can move status without touching the name a
	// human is editing in another tab.
	Update(ctx context.Context, repositoryID, packageID uuid.UUID, name, description, status, note *string) (domain.DeployPackage, error)
	Delete(ctx context.Context, repositoryID, packageID uuid.UUID) error
	// ReplaceTasks sets the full membership in one transaction; position is the
	// index in the supplied slice.
	ReplaceTasks(ctx context.Context, packageID uuid.UUID, taskIDs []uuid.UUID) error
	// ListTasks returns membership enriched with each task's key/title/column,
	// ordered by position.
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
