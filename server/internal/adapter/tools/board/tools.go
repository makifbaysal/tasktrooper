package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskManager interface {
	ListTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error)
	ListAllTasks(ctx context.Context) ([]domain.BoardTask, error)
	// ListReadyTasks is the unblocked queue — backlog/todo tasks with no
	// unfinished blocker — an agent reads instead of guessing from ListTasks
	// which of its columns is actually startable.
	ListReadyTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error)
	// GetTask reads one task scoped by repository — the cheap membership
	// question resolveTaskRepositoryID asks on every task-scoped call. A task
	// id from another repository comes back missing rather than readable.
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	FindTaskRepositoryID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error)
	DefaultRepositoryID(ctx context.Context) (uuid.UUID, error)
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error)
	DeleteTask(ctx context.Context, repositoryID, taskID uuid.UUID) error
	ClaimTask(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) (domain.BoardTask, error)
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
	AddDocument(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskDocumentRequest) (domain.TaskDocument, error)
	ListTaskCriteria(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error)
	ReplaceAcceptanceCriteria(ctx context.Context, repositoryID, taskID uuid.UUID, items []domain.AcceptanceCriterionInput) ([]domain.AcceptanceCriterion, error)
	SetTaskCriterionCompleted(ctx context.Context, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error)
	SetTaskCriterionCanceled(ctx context.Context, criterionID uuid.UUID, canceled bool, reason, authorType, authorID string) (domain.AcceptanceCriterion, error)
	ReviewTaskCriterion(ctx context.Context, criterionID, agentID uuid.UUID, approved bool, note string) (domain.CriterionCheck, error)
	ListTestCases(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error)
	RecordTestCases(ctx context.Context, taskID uuid.UUID, items []domain.TaskTestCaseInput) ([]domain.TaskTestCase, error)
	SetTestCaseResult(ctx context.Context, testCaseID uuid.UUID, item domain.TaskTestCaseInput) (domain.TaskTestCase, error)
	LatestTaskPipeline(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPipeline, error)
}

// WorkspaceLister exposes the board's projects and code repositories so agents
// can answer factual questions ("how many projects do we have?") and manage the
// workspace (create/rename projects, link repositories) instead of guessing or
// describing what they would do.
type WorkspaceLister interface {
	ListProjects(ctx context.Context) ([]domain.InitiativeProject, error)
	ListRepositories(ctx context.Context) ([]domain.Repository, error)
	CreateProject(ctx context.Context, name, description string) (domain.InitiativeProject, error)
	UpdateProject(ctx context.Context, id uuid.UUID, name, description string) (domain.InitiativeProject, error)
	SetRepositoryProjects(ctx context.Context, repositoryID uuid.UUID, projectIDs []uuid.UUID) (domain.Repository, error)
}

// TeamLister exposes the roster of orchestration agents so any agent can look
// up who is on the team and what each role does, live, instead of relying on a
// hardcoded description that goes stale as roles change.
type TeamLister interface {
	ListAgents(ctx context.Context) ([]domain.Agent, error)
}

// AttachmentManager links already-uploaded binary attachments onto board
// tasks. Kept as a narrow interface so the toolkit does not depend on the
// attachment application package.
type AttachmentManager interface {
	LinkTask(ctx context.Context, repositoryID, taskID, attachmentID uuid.UUID) (domain.AttachmentMeta, error)
}

// ComponentResolver looks up a monorepo component by its repository-relative
// path, so create_task/update_task accept "services/api" (or "." for the
// repository root) instead of a UUID the planner would have to already know.
type ComponentResolver interface {
	ComponentByPath(ctx context.Context, repositoryID uuid.UUID, path string) (domain.Component, error)
}

// ReleaseService is the release-engineer's use case as the tools need it
// (implemented by application/release.Service), narrowed to the calls
// release_tools.go makes: reading the release covering a task, and moving it
// through deploy/watch/verify to a verdict.
type ReleaseService interface {
	ForTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.Release, error)
	// ForAgent is ForTask stamped with AgentSeenAt — release_tools.go's
	// resolveRelease resolves every release tool through this, not ForTask,
	// so the hand-back watchdog's AgentSeenAt gate sees a live agent call
	//. get_deploy_logs's release fallback still uses plain ForTask: it
	// is a read of another task's release, not the agent working this one.
	ForAgent(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.Release, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Release, error)
	Deploy(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor) (domain.Release, error)
	// Watch returns a ResourceBlock when the release is still Watched()
	// (deploying/verifying/rolling_back) — see release_tools.go's
	// watch_release, which parks the run on it exactly as get_deploy_logs's
	// predecessor used to for domain.ResourceDeployWatch.
	Watch(ctx context.Context, releaseID uuid.UUID) (domain.Release, *domain.ResourceBlock, error)
	RunSmoke(ctx context.Context, releaseID uuid.UUID) ([]domain.SmokeResult, error)
	Finish(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, note string) (domain.Release, error)
	Rollback(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, reason domain.RollbackReason, note string) (domain.Release, error)
}

type ToolKit struct {
	Tasks       TaskManager
	Workspace   WorkspaceLister
	Team        TeamLister
	Attachments AttachmentManager
	// PullRequests is the task↔pull-request use case. Nil on a build with no git
	// or no GitHub token store, which is why the three PR tools are registered
	// conditionally rather than always.
	PullRequests TaskPullRequests
	// DeployWatch is the legacy per-task deploy signal get_deploy_logs falls
	// back to for a task with no release. Nil for the same reasons
	// PullRequests is nil — it needs GitHub.
	DeployWatch DeployWatch
	// Releases is the release-engineer's service: the release covering a
	// task, and the actions that move it — deploy, watch, verify, finish,
	// roll back. Nil on a build where it has not been wired yet (it is built
	// later in the boot sequence than this registration runs, the same
	// reason DeployWatch is read at call time), which is why the six release
	// tools answer "the release service is not configured on this
	// deployment" instead of failing to register.
	Releases ReleaseService
	// Workflows/Roles back create_task.go's task_type validation and
	// assignee_role, and list_team.go's roles/subscribed_columns.
	Workflows port.WorkflowReader
	Roles     port.RoleResolver
	// Subscriptions answers list_team's subscribed_columns per agent. Narrow
	// on purpose (not the whole port.BoardConfigStore) since that is all this
	// package reads from it.
	Subscriptions SubscriptionLister
	// Components resolves create_task/update_task's optional component path
	// argument. Nil means the argument is accepted but ignored — a build with
	// no project model still runs the rest of the tool.
	Components ComponentResolver
}

// SubscriptionLister is the read-only slice of port.BoardConfigStore
// list_team's subscribed_columns needs.
type SubscriptionLister interface {
	ListAgentSubscriptions(ctx context.Context, agentID uuid.UUID) ([]string, error)
}

func NewExecutors(kit *ToolKit) []port.ToolExecutor {
	if kit == nil || kit.Tasks == nil {
		return nil
	}
	execs := []port.ToolExecutor{
		newListTasksTool(kit),
		newListReadyTasksTool(kit),
		newCreateTaskTool(kit),
		newMoveTaskTool(kit),
		newUpdateTaskTool(kit),
		newDeleteTaskTool(kit),
		newClaimTaskTool(kit),
		newAddCommentTool(kit),
		newListCommentsTool(kit),
		newAddDocumentTool(kit),
		newUpdateDocumentTool(kit),
		newListDocumentsTool(kit),
		newListCriteriaTool(kit),
		newSetCriterionTool(kit),
		newCancelCriterionTool(kit),
		newReviewCriterionTool(kit),
		newListTestCasesTool(kit),
		newRecordTestCasesTool(kit),
		newSetTestCaseResultTool(kit),
		newBoardSummaryTool(kit),
		newGetPipelineStatusTool(kit),
	}
	if kit.Workspace != nil {
		execs = append(execs,
			newListProjectsTool(kit),
			newListRepositoriesTool(kit),
			newCreateProjectTool(kit),
			newUpdateProjectTool(kit),
			newSetRepositoryProjectsTool(kit),
		)
	}
	if kit.Team != nil {
		execs = append(execs, newListTeamTool(kit))
	}
	if kit.Attachments != nil {
		execs = append(execs, newAttachTaskFileTool(kit))
	}
	if kit.PullRequests != nil {
		execs = append(execs,
			newGetTaskPullRequestTool(kit),
			newCommitTaskChangesTool(kit),
			newCommentOnPullRequestTool(kit),
			// Registered with the other PR tools, but held by QA alone (see
			// catalog/role_tools.go and migration 104): being registered is what
			// makes it callable at all, the policy is what decides by whom.
			newMergeTaskPullRequestTool(kit),
		)
	}
	if kit.PullRequests != nil {
		// Registered on the SAME condition as the PR tools — GitHub is wired —
		// rather than on kit.DeployWatch/kit.Releases being set, because those
		// services are built later in the boot sequence than this registration
		// runs (they need the deploy-target/release stores, opened with the
		// rest of the pipeline wiring). The kit is a pointer shared with every
		// executor, so the fields are read at CALL time; a build where the
		// later wiring did not happen answers "... is not configured on this
		// deployment" instead of silently missing the tools.
		//
		// They belong beside the merge for the same reason they belong beside it
		// in the prompt: merging is what puts a commit in front of a deploy, and
		// a release nobody watches is how a task reaches `released` on the
		// strength of a green PR check. Registration is what makes them callable
		// at all; catalog/role_tools.go decides by whom, and
		// domain.RestrictToolsForStage's strip_writers behaviour decides in which column.
		execs = append(execs,
			newDeployLogsTool(kit),
			newGetReleaseTool(kit),
			newDeployReleaseTool(kit),
			newWatchReleaseTool(kit),
			newRunSmokeChecksTool(kit),
			newFinishReleaseTool(kit),
			newRollbackReleaseTool(kit),
		)
	}
	return execs
}

// resolveComponentRef resolves a component path argument against
// repositoryID. A nil Components resolver silently ignores the argument
// (returns nil, nil) rather than failing the whole tool call over a
// deployment that has not scanned its repositories yet.
func (kit *ToolKit) resolveComponentRef(ctx context.Context, repositoryID uuid.UUID, path string) (*uuid.UUID, error) {
	if kit.Components == nil {
		return nil, nil
	}
	comp, err := kit.Components.ComponentByPath(ctx, repositoryID, path)
	if err != nil {
		return nil, fmt.Errorf("component %q: %w", path, err)
	}
	return &comp.ID, nil
}

func (kit *ToolKit) resolveCreateRepositoryID(ctx context.Context, explicit *uuid.UUID) (uuid.UUID, error) {
	if explicit != nil && *explicit != uuid.Nil {
		return *explicit, nil
	}
	if id := registry.RepositoryIDFromContext(ctx); id != uuid.Nil {
		return id, nil
	}
	return kit.Tasks.DefaultRepositoryID(ctx)
}

// resolveTaskRepositoryID answers which repository a task-scoped tool call acts
// in: the repository the run is bound to, whenever one is bound.
//
// That binding is containment, not addressing. This one resolver feeds
// merge_task_pull_request, deploy_release/watch_release, delete_board_task, the deploy
// tools and every board write, so a run bound to repository X that could be
// talked into naming a task in repository Y — by a planted task, a comment,
// anything the model read — would act on Y's code. It must not, and letting
// the task's real repository win would have made exactly that possible.
//
// The board-wide task lookup stays, but it now DETECTS rather than decides, and
// only when the cheap check has already missed: a task that resolves elsewhere
// is refused with domain.ErrTaskOutsideRepository naming the task and the
// repository this run is bound to. That refusal is the actual defect being
// fixed here — the cross-repo case already failed, but as a bare "board task
// not found: <id>" the agent could only read as bad luck and retry.
//
// A lookup that errors or resolves to nothing keeps the run's repository, so
// the caller fails with the store's own not-found error against the repository
// it was working in rather than with this lookup's. With no run repository (an
// unscoped chat) there is nothing to contain and the lookup's answer — error
// included — propagates unchanged.
func (kit *ToolKit) resolveTaskRepositoryID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	contextRepositoryID := registry.RepositoryIDFromContext(ctx)
	if contextRepositoryID == uuid.Nil {
		return kit.Tasks.FindTaskRepositoryID(ctx, taskID)
	}
	// The common case by far — the run is acting on its own task — and it is
	// answered by one row read from the repository this run is already bound
	// to. FindTaskRepositoryID is the expensive one: it lists every task in
	// every repository and bulk-reads their pipeline statuses, which is far too
	// much to spend confirming what the context already says.
	if _, err := kit.Tasks.GetTask(ctx, contextRepositoryID, taskID); err == nil {
		return contextRepositoryID, nil
	}
	// Not in this run's repository — which is either the attack (a task id from
	// somewhere else) or an ordinary bad id. Only that distinction is worth a
	// board-wide lookup.
	taskRepositoryID, err := kit.Tasks.FindTaskRepositoryID(ctx, taskID)
	if err == nil && taskRepositoryID != uuid.Nil && taskRepositoryID != contextRepositoryID {
		return uuid.Nil, fmt.Errorf(
			"%w: board task %s is not in repository %s, which this run is bound to; cross-repository actions are refused — leave a comment on your own task naming the other task instead",
			domain.ErrTaskOutsideRepository, taskID, contextRepositoryID)
	}
	return contextRepositoryID, nil
}

// findTask returns the task by id, or ok=false when it cannot be resolved.
// Callers use it for decisions that must not fail the tool call when the board
// is momentarily unreachable — an unknown task type means "no opinion".
func (kit *ToolKit) findTask(ctx context.Context, taskID uuid.UUID) (domain.BoardTask, bool) {
	repositoryID, err := kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return domain.BoardTask{}, false
	}
	tasks, err := kit.Tasks.ListTasks(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, false
	}
	for _, t := range tasks {
		if t.ID == taskID {
			return t, true
		}
	}
	return domain.BoardTask{}, false
}

func (kit *ToolKit) listBoardTasks(ctx context.Context) ([]domain.BoardTask, error) {
	if id := registry.RepositoryIDFromContext(ctx); id != uuid.Nil {
		return kit.Tasks.ListTasks(ctx, id)
	}
	return kit.Tasks.ListAllTasks(ctx)
}

func (kit *ToolKit) listReadyTasks(ctx context.Context) ([]domain.BoardTask, error) {
	return kit.Tasks.ListReadyTasks(ctx, registry.RepositoryIDFromContext(ctx))
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

func toolJSON(name string, payload any) domain.ToolResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError(name, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: name, Content: string(raw), IsError: false}
}
