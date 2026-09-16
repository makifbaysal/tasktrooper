package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/google/uuid"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/ci"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/taskkey"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ColumnValidator interface {
	ValidateColumn(ctx context.Context, slug string) error
	ValidateTransition(ctx context.Context, from, to string) error
}

// RevisionNotifier is implemented by the evolution service; it is poked when a
// task lands in need_revision so the assignee agent can run a mini-reflection.
type RevisionNotifier interface {
	NotifyRevision(ctx context.Context, task domain.BoardTask)
}

// AnalizAssignmentSource is the narrow slice of settings.Service CreateTask
// needs: read the backend/frontend/mobile analiz-assignment settings to
// override an analiz task's assignee. Wired late by runtime, same setter
// style as SetEvolution, so this package stays decoupled from settings.
type AnalizAssignmentSource interface {
	Get(ctx context.Context) (domain.AppSettings, error)
}

// ProfileRefresher is implemented by the repoprofile service; it rebuilds the
// agent-maintained project profile in the background. Wired late by runtime
// (same setter style as SetEvolution) so this package stays decoupled from it.
type ProfileRefresher interface {
	// RefreshAsync starts a rebuild; false = one is already running. The
	// rebuild outlives the request, so ctx's cancellation is not honoured.
	RefreshAsync(ctx context.Context, repositoryID uuid.UUID, reason string) bool
	// RefreshIfStale rebuilds only when the profile is missing or stale.
	RefreshIfStale(ctx context.Context, repositoryID uuid.UUID, reason string)
	// RefreshAfterPush rebuilds only the profile sections whose source files
	// the push actually touched, falling back to the age gate when it cannot
	// tell what moved.
	RefreshAfterPush(ctx context.Context, repositoryID uuid.UUID, reason string)
	// Sections and Proposals back the settings page's profile view.
	Sections(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileSection, error)
	Proposals(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProfileProposal, error)
	// ApplyProposal / DismissProposal are the two answers a human can give to
	// a setting the profiling pass worked out but would not overwrite.
	ApplyProposal(ctx context.Context, repositoryID, proposalID uuid.UUID) (domain.ProfileProposal, error)
	DismissProposal(ctx context.Context, repositoryID, proposalID uuid.UUID) (domain.ProfileProposal, error)
}

type Service struct {
	repos           port.RepositoryStore
	tasks           port.BoardTaskStore
	criteria        port.AcceptanceCriterionStore
	testCases       port.TaskTestCaseStore
	relations       port.TaskRelationStore
	documents       port.TaskDocumentStore
	comments        port.TaskCommentStore
	attachments     port.AttachmentStore
	columns         ColumnValidator
	dispatcher      *board.Dispatcher
	scorer          *board.ScoreTracker
	completion      *board.CompletionStamper
	reviewGate      *board.ReviewGate
	evolution       RevisionNotifier
	pipelines       *board.PipelineRunner
	pipelineStore   port.TaskPipelineStore
	deployPackages  port.DeployPackageStore
	spans           StageEvidence
	requireCriteria bool
	indexer         *indexer.Service
	allowedRoots    []string
	indexMu         sync.Mutex
	indexing        map[uuid.UUID]struct{}
	git             port.GitClient
	deployTargets   port.DeployTargetStore
	mobileStoreApps port.MobileStoreAppStore
	workspaceRoot   string
	gitWarnMu       sync.Mutex
	gitWarnings     map[uuid.UUID]string

	// restoreMu guards the working-copy restore ledger: the running or last
	// clone-onto-this-host attempt per repository (see restore.go). It is
	// process-local on purpose — it describes work THIS runtime is doing, and a
	// restart legitimately forgets an attempt it is no longer running.
	restoreMu sync.Mutex
	restores  map[uuid.UUID]*domain.RepositoryRestore
	// restoreRun is a test seam (same shape as pushRunFn); nil = `go fn()`.
	restoreRun func(fn func())
	// prAsyncRun is a test seam for ensurePullRequestAsync's background half;
	// nil = `go fn()`.
	prAsyncRun func(fn func())

	// syncMu guards the clone-freshness ledger: the last pull failure per
	// repository (reported on the index status so a stale clone stops looking
	// like a healthy index) and when its freshness was last checked.
	syncMu        sync.Mutex
	syncWarnings  map[uuid.UUID]string
	syncCheckedAt map[uuid.UUID]time.Time

	pipelineJobs     port.RepositoryPipelineJobStore
	githubToken      func(ctx context.Context) (string, error)
	agentLister      func(ctx context.Context) ([]domain.Agent, error)
	analizAssignment AnalizAssignmentSource
	profiles         ProfileRefresher

	// GitHub push-webhook state (see webhook.go). publicBaseURL is where
	// GitHub must deliver; the maps are the per-repo debounce ledger and the
	// delivery-id replay cache, both guarded by pushMu.
	publicBaseURL       string
	githubAPIBase       string // test seam; empty = real github.com API
	pushReindexInterval time.Duration
	pushRunFn           func(repositoryID uuid.UUID) // test seam; nil = startPushReindex
	pushMu              sync.Mutex
	pushRepos           map[uuid.UUID]*pushRepoState
	pushDeliveries      map[string]time.Time
	// deliveries is the shared, durable delivery ledger. Nil on a deployment
	// with no Postgres, where pushDeliveries above is the whole mechanism and
	// is correct — one process there sees every delivery.
	deliveries DeliveryLedger
}

func NewService(
	repos port.RepositoryStore,
	tasks port.BoardTaskStore,
	criteria port.AcceptanceCriterionStore,
	relations port.TaskRelationStore,
	documents port.TaskDocumentStore,
	comments port.TaskCommentStore,
	columns ColumnValidator,
	dispatcher *board.Dispatcher,
	indexerSvc *indexer.Service,
	allowedRoots []string,
) *Service {
	return &Service{
		repos:        repos,
		tasks:        tasks,
		criteria:     criteria,
		relations:    relations,
		documents:    documents,
		comments:     comments,
		columns:      columns,
		dispatcher:   dispatcher,
		indexer:      indexerSvc,
		allowedRoots: allowedRoots,
		indexing:     make(map[uuid.UUID]struct{}),
		gitWarnings:  make(map[uuid.UUID]string),

		syncWarnings:  make(map[uuid.UUID]string),
		syncCheckedAt: make(map[uuid.UUID]time.Time),
	}
}

func (s *Service) SetScorer(st *board.ScoreTracker) {
	s.scorer = st
}

// SetCompletionStamper wires the stamper that decides, when a task reaches a
// terminal column, whether it got there without rework. Nil disables stamping,
// which leaves every task unclean and every time KPI unmeasured.
func (s *Service) SetCompletionStamper(cs *board.CompletionStamper) {
	s.completion = cs
}

// SetReviewGate wires "human after agent" review. Nil leaves every move
// unintercepted, which is the pre-gate behaviour.
func (s *Service) SetReviewGate(g *board.ReviewGate) {
	s.reviewGate = g
}

func (s *Service) SetEvolution(n RevisionNotifier) {
	s.evolution = n
}

// SetProfileRefresher wires the project-profile rebuilder. Nil leaves imports
// and pushes without auto-profiling, which is the pre-profile behaviour.
func (s *Service) SetProfileRefresher(p ProfileRefresher) {
	s.profiles = p
}

// SetPipelineRunner wires the QA-gate pipeline runner (late-wired to avoid an
// import cycle at construction time, mirroring SetGit/SetScorer/SetEvolution).
func (s *Service) SetPipelineRunner(pr *board.PipelineRunner) {
	s.pipelines = pr
}

// SetPipelineStore wires read access to task pipelines (late-set for the same
// reason as SetPipelineRunner: avoids an import cycle at construction time).
func (s *Service) SetPipelineStore(store port.TaskPipelineStore) {
	s.pipelineStore = store
}

func (s *Service) SetRequireCriteriaComplete(require bool) {
	s.requireCriteria = require
}

// SetAttachmentStore wires binary attachments into the single-task detail
// enrichment (late-set like the other optional stores). Nil leaves task
// payloads without an attachments field.
func (s *Service) SetAttachmentStore(store port.AttachmentStore) {
	s.attachments = store
}

// ListTaskCriteria exposes acceptance criteria for board tools.
func (s *Service) ListTaskCriteria(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	if s.criteria == nil {
		return nil, fmt.Errorf("criteria store unavailable")
	}
	return s.criteria.ListByTask(ctx, taskID)
}

// SetTaskCriterionCompleted marks a single criterion done/undone.
func (s *Service) SetTaskCriterionCompleted(ctx context.Context, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error) {
	if s.criteria == nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("criteria store unavailable")
	}
	return s.criteria.UpdateCompleted(ctx, criterionID, completed)
}

// SetTaskCriterionCanceled drops a criterion from the task's scope, or puts it
// back, and writes the reason where humans read the card.
//
// The comment is not decoration and it is not the agent's to forget: a criterion
// that leaves the list changes what "done" means for this task, and the argument
// for it has to sit in the same thread as everything else that was decided. The
// stored reason drives the UI; the comment drives the conversation. Failing to
// post it does not fail the cancellation — the decision is already recorded on
// the criterion itself, and losing the cancellation over a comment write would
// re-park the task for the wrong reason.
func (s *Service) SetTaskCriterionCanceled(ctx context.Context, criterionID uuid.UUID, canceled bool, reason string, authorType, authorID string) (domain.AcceptanceCriterion, error) {
	if s.criteria == nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("criteria store unavailable")
	}
	reason = strings.TrimSpace(reason)
	if canceled && reason == "" {
		return domain.AcceptanceCriterion{}, fmt.Errorf("cancelling a criterion requires a reason: say why it is not being done (out of scope, superseded, impossible as written)")
	}
	item, err := s.criteria.UpdateCanceled(ctx, criterionID, canceled, reason)
	if err != nil {
		return domain.AcceptanceCriterion{}, err
	}
	if s.comments != nil {
		content := fmt.Sprintf("Acceptance criterion cancelled — %q\nReason: %s", item.Text, reason)
		if !canceled {
			content = fmt.Sprintf("Acceptance criterion put back in scope — %q", item.Text)
		}
		if authorType == "" {
			authorType = "agent"
		}
		if _, cErr := s.comments.Create(ctx, domain.TaskComment{
			TaskID:     item.TaskID,
			AuthorType: authorType,
			AuthorID:   authorID,
			Content:    content,
		}); cErr != nil {
			log.Warn().Err(cErr).Str("criterion_id", criterionID.String()).Msg("criterion cancellation comment failed")
		}
	}
	return item, nil
}

// ReviewTaskCriterion records a reviewer's verdict on one criterion. The role
// is derived from the column the task is in — QA while the task is in
// ready_for_qa/in_qa, PM while it is in pm_uat — so an agent cannot file a
// verdict for a phase it is not working. A rejection must carry a note: that
// note is what the developer reads in need_revision and what the UI shows next
// to the failed step.
func (s *Service) ReviewTaskCriterion(ctx context.Context, criterionID, agentID uuid.UUID, approved bool, note string) (domain.CriterionCheck, error) {
	if s.criteria == nil {
		return domain.CriterionCheck{}, fmt.Errorf("criteria store unavailable")
	}
	note = strings.TrimSpace(note)
	if !approved && note == "" {
		return domain.CriterionCheck{}, fmt.Errorf("a rejected criterion needs a note explaining what failed and how it was observed")
	}
	criterion, err := s.criteria.GetCriterion(ctx, criterionID)
	if err != nil {
		return domain.CriterionCheck{}, fmt.Errorf("criterion not found: %w", err)
	}
	repositoryID, err := s.FindTaskRepositoryID(ctx, criterion.TaskID)
	if err != nil {
		return domain.CriterionCheck{}, err
	}
	task, err := s.tasks.Get(ctx, repositoryID, criterion.TaskID)
	if err != nil {
		return domain.CriterionCheck{}, err
	}
	var role domain.CriterionReviewRole
	switch task.Column {
	case domain.TaskColumnReadyForQA, domain.TaskColumnInQA:
		role = domain.CriterionReviewRoleQA
	case domain.TaskColumnPMUAT:
		role = domain.CriterionReviewRolePM
	default:
		return domain.CriterionCheck{}, fmt.Errorf("criterion verdicts are recorded while the task is under QA (ready_for_qa/in_qa) or PM UAT (pm_uat); task %s is in %s — do not move the task to reach the criteria: their ids are in your run context and in move refusals; if the task already left your column, stop and report instead of retrying", task.Key, task.Column)
	}
	check := domain.CriterionCheck{CriterionID: criterionID, Role: role, Approved: approved, Note: note}
	if agentID != uuid.Nil {
		check.AgentID = &agentID
	}
	return s.criteria.UpsertCheck(ctx, check)
}

// criteriaGate blocks forward moves while acceptance criteria remain open.
func (s *Service) criteriaGate(ctx context.Context, taskID uuid.UUID, target domain.TaskColumn) error {
	if !s.requireCriteria || s.criteria == nil {
		return nil
	}
	switch target {
	case domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskColumnDone, domain.TaskColumnReleased:
	default:
		return nil
	}
	items, err := s.criteria.ListByTask(ctx, taskID)
	if err != nil || len(items) == 0 {
		return nil
	}
	var open []string
	for _, c := range items {
		// A cancelled criterion is settled, not met: it carries a written
		// reason, so it no longer owes the board an answer and must not hold
		// the hand-off. See migration 131.
		if !c.Settled() && !criterionApprovedByAReviewer(c) {
			open = append(open, criterionRef(c))
		}
	}
	if len(open) == 0 {
		return nil
	}
	// The remedy names both tools because this gate is hit by two roles holding
	// two different halves of the vocabulary. set_criterion_completed is the
	// implementer's; QA and the PM do not have it and never will — a reviewer
	// that could tick the implementer's box could pass its own work. Telling QA
	// to call it produced exactly the dead end this sentence now avoids: the run
	// answered that it had no such tool, ticked nothing, and the task sat where
	// it was until a human noticed.
	return fmt.Errorf("cannot move to %s: %d acceptance criteria incomplete: %s — "+
		"if you implemented them, tick each with set_criterion_completed; "+
		"if one is deliberately not being done, cancel it with cancel_criterion and a reason; "+
		"if you are reviewing (QA in ready_for_qa/in_qa, PM in pm_uat), record your verdict with review_criterion instead",
		target, len(open), strings.Join(open, "; "))
}

// criterionRef renders a criterion the way a refusal has to name it: id first,
// then text. The gates used to list only the text, and the agent reading the
// refusal did the obvious thing — passed that text to review_criterion as the
// criterion_id and was told it was invalid, with no id anywhere in reach.
func criterionRef(c domain.AcceptanceCriterion) string {
	return fmt.Sprintf("[%s] %s", c.ID, c.Text)
}

// criterionApprovedByAReviewer reports whether QA or the PM has approved this
// criterion in their own name.
//
// Such an approval satisfies criteriaGate on its own, without the implementer's
// checkmark. The two signals are not equal and the stronger one was being
// ignored: Completed is a claim the implementer makes about its own work, while
// a check is a verdict recorded by a role that had to execute the criterion to
// record it. Demanding the weaker signal anyway deadlocked the board — criteria
// added to a task already in flight arrive unticked (update_board_task replaces
// the whole list), and the roles standing in front of the gate at done and
// released are QA and the PM, neither of which holds set_criterion_completed.
//
// A REJECTION is deliberately not enough. It is a verdict too, but it is the
// verdict that means the criterion is NOT met, and criteriaReviewGate refuses
// the same forward move over it a few lines later.
func criterionApprovedByAReviewer(c domain.AcceptanceCriterion) bool {
	for i := range c.Checks {
		if c.Checks[i].Approved {
			return true
		}
	}
	return false
}

// criteriaReviewGate enforces that the reviewing role has verified every
// criterion before the task leaves its review phase FORWARD, in any direction:
// QA must approve all criteria to leave ready_for_qa/in_qa, PM to leave pm_uat.
// Moves to need_revision are never gated — a rejected criterion is exactly why
// that move happens. Like criteriaGate it only runs with
// require_criteria_complete.
//
// It used to name the ONE forward column each phase normally hands off to
// (QA → pm_uat, PM → human_uat/done/released), which left the skip-ahead moves
// ungated: a QA run moved a task from in_qa straight to `done` and every
// criterion verdict it never recorded went with it. The task was reported
// tested by a run that had executed nothing. Any forward exit is now the same
// exit, so skipping the next column no longer skips the gate.
func (s *Service) criteriaReviewGate(ctx context.Context, taskID uuid.UUID, prev, target domain.TaskColumn) error {
	if !s.requireCriteria || s.criteria == nil {
		return nil
	}
	if !isForwardReviewExit(target) {
		return nil
	}
	var role domain.CriterionReviewRole
	switch prev {
	case domain.TaskColumnReadyForQA, domain.TaskColumnInQA:
		role = domain.CriterionReviewRoleQA
	case domain.TaskColumnPMUAT:
		role = domain.CriterionReviewRolePM
	default:
		return nil
	}
	items, err := s.criteria.ListByTask(ctx, taskID)
	if err != nil || len(items) == 0 {
		return nil
	}
	var unchecked, rejected []string
	for _, c := range items {
		// Nobody verifies a criterion that was cancelled: there is nothing to
		// execute, and demanding a verdict on it would make QA either invent
		// one or bounce a task whose scope was legitimately reduced.
		if c.Canceled {
			continue
		}
		verdict := criterionCheckFor(c, role)
		switch {
		case verdict == nil:
			unchecked = append(unchecked, criterionRef(c))
		case !verdict.Approved:
			rejected = append(rejected, fmt.Sprintf("%s (%s)", criterionRef(c), verdict.Note))
		}
	}
	if len(rejected) > 0 {
		return fmt.Errorf("cannot move to %s: %d acceptance criteria are rejected by %s: %s — move the task to need_revision instead, or re-verify and approve them (review_criterion)", target, len(rejected), role, strings.Join(rejected, "; "))
	}
	if len(unchecked) > 0 {
		return fmt.Errorf("cannot move to %s: %d acceptance criteria await your %s verdict: %s — call review_criterion with each id above, then retry the move", target, len(unchecked), role, strings.Join(unchecked, "; "))
	}
	return nil
}

// isForwardReviewExit reports whether moving a task INTO this column means its
// current review phase is over and passed.
//
// pm_uat/human_uat/done/released are all "this passed" claims — which one a
// board actually uses depends on its columns and on require_human_review, so
// the gate cannot key on the single expected next column. need_revision and
// the working columns are not here on purpose: handing work back, or pulling it
// back to be redone, never needs a verdict it is about to invalidate.
func isForwardReviewExit(target domain.TaskColumn) bool {
	switch target {
	case domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT,
		domain.TaskColumnDone, domain.TaskColumnReleased:
		return true
	default:
		return false
	}
}

func criterionCheckFor(c domain.AcceptanceCriterion, role domain.CriterionReviewRole) *domain.CriterionCheck {
	for i := range c.Checks {
		if c.Checks[i].Role == role {
			return &c.Checks[i]
		}
	}
	return nil
}

// SetDeployTargets lets the env inventory report where each environment
// actually answers alongside the variables it needs.
func (s *Service) SetDeployTargets(store port.DeployTargetStore) {
	s.deployTargets = store
}

// SetMobileStoreApps wires the mobile store lifecycle rows so mobileStoreGate
// can block stage deploys and prod releases behind onboarding and store
// review state. Nil (the pre-wiring default) makes any store target gate as
// not-ready rather than silently letting an unverified app ship.
func (s *Service) SetMobileStoreApps(store port.MobileStoreAppStore) {
	s.mobileStoreApps = store
}

func (s *Service) SetGit(git port.GitClient, workspaceRoot string) {
	s.git = git
	s.workspaceRoot = workspaceRoot
}

// SetPipelineJobStore wires the category→GitHub-Actions mapping store.
func (s *Service) SetPipelineJobStore(store port.RepositoryPipelineJobStore) {
	s.pipelineJobs = store
}

// SetGitHubTokenSource wires read access to the stored GitHub token (for
// parsing workflows when building the pipeline job config view).
func (s *Service) SetGitHubTokenSource(src func(ctx context.Context) (string, error)) {
	s.githubToken = src
}

// SetAgentLister wires the roster lookup used to auto-assign the workflow-setup
// task to the right role.
func (s *Service) SetAgentLister(fn func(ctx context.Context) ([]domain.Agent, error)) {
	s.agentLister = fn
}

// SetAnalizAssignmentSource wires the backend/frontend/mobile analiz-assignment
// settings CreateTask consults to override an analiz task's assignee. Nil (the
// pre-wiring behaviour) leaves CreateTask's existing behaviour untouched: the
// PM's requested assignee is used as-is.
func (s *Service) SetAnalizAssignmentSource(src AnalizAssignmentSource) {
	s.analizAssignment = src
}

// CreateWorkflowSetupTask opens a board task (assigned by repo kind) to author
// the repo's GitHub Actions CI/CD workflows, for repos that have none yet.
func (s *Service) CreateWorkflowSetupTask(ctx context.Context, repositoryID uuid.UUID) (domain.BoardTask, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	role := domain.DeveloperAgentForKind(repo.Kind, repo.SubProjects)
	var assignee *uuid.UUID
	if s.agentLister != nil {
		if agents, aerr := s.agentLister(ctx); aerr == nil {
			for i := range agents {
				if agents[i].Name == role {
					id := agents[i].ID
					assignee = &id
					break
				}
			}
		}
	}
	desc := fmt.Sprintf(`Create and push GitHub Actions CI/CD workflows for this repository (kind: %s).

Required jobs (under .github/workflows):
- validate: lint / static analysis / type checking
- build: compilation (%s)
- test: automated tests
- stage_deploy: a workflow_dispatch-triggerable workflow that deploys to staging
- preprod_deploy: (optional) a workflow_dispatch-triggerable workflow that deploys to a pre-production environment
- prod_deploy: a workflow_dispatch-triggerable workflow that deploys to production

Once each workflow exists, save the job/workflow mapping under Repository Settings > Pipeline so the QA gate and release steps run through GitHub Actions.`,
		repo.Kind, buildHintForKind(repo.Kind))

	return s.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:           "Set up GitHub Actions CI/CD workflows",
		Description:     desc,
		TaskType:        domain.TaskTypeTask,
		Priority:        domain.TaskPriorityHigh,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: assignee,
	})
}

// resolveAnalizAssignee overrides an analiz task's assignee with the agent
// named by the backend/frontend/mobile analiz-assignment settings, regardless
// of what the caller (typically the PM agent) requested — the setting exists
// precisely to correct a PM that keeps assigning analiz to a developer whose
// tool policy cannot carry the task (see domain.RequiredAnalizTools). Returns
// nil (leave the caller's assignee alone) when the setting source or agent
// roster is not wired, or the resolved agent name has no matching row.
func (s *Service) resolveAnalizAssignee(ctx context.Context, repo domain.Repository) *uuid.UUID {
	if s.analizAssignment == nil || s.agentLister == nil {
		return nil
	}
	settings, err := s.analizAssignment.Get(ctx)
	if err != nil {
		return nil
	}
	area := domain.ResolveAnalizArea(repo.Kind, repo.SubProjects)
	name := domain.AnalizAssigneeForArea(settings, area)
	agents, err := s.agentLister(ctx)
	if err != nil {
		return nil
	}
	for i := range agents {
		if agents[i].Name == name {
			id := agents[i].ID
			return &id
		}
	}
	return nil
}

func buildHintForKind(kind string) string {
	switch kind {
	case domain.RepoKindFrontend:
		return "e.g. npm/pnpm build"
	case domain.RepoKindMobile:
		return "e.g. xcodebuild/fastlane — do not use docker"
	case domain.RepoKindMonorepo:
		return "a separate build per subproject (backend/frontend/mobile/worker)"
	default:
		return "e.g. go build / docker build"
	}
}

// GetPipelineConfig returns the repo's type + saved mappings and, when GitHub
// is reachable, auto-detect suggestions parsed from .github/workflows.
func (s *Service) GetPipelineConfig(ctx context.Context, repositoryID uuid.UUID) (ci.ConfigView, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return ci.ConfigView{}, err
	}
	view := ci.ConfigView{
		Kind:              repo.Kind,
		SubRepoKinds:      repo.SubRepoKinds,
		AutoReleaseOnDone: repo.AutoReleaseOnDone,
	}
	if s.pipelineJobs != nil {
		if saved, serr := s.pipelineJobs.ListByRepository(ctx, repositoryID); serr == nil {
			view.Saved = saved
		}
	}
	refs, hasWorkflows := s.parseWorkflowRefs(ctx, repo)
	view.HasWorkflows = hasWorkflows
	// One suggestion group per actual sub-project (by path, not by kind) so
	// two sub-projects sharing a kind get their own slots instead of
	// colliding on one. A monorepo with no sub_projects yet (never detected
	// one, or predates this feature) falls back to one group per checked
	// sub_repo_kind — the pre-sub-projects behavior — over every kind when
	// none is checked yet, so a freshly ticked box shows candidates before
	// the selection is saved.
	subProjects := repo.SubProjects
	if repo.Kind == domain.RepoKindMonorepo && len(subProjects) == 0 {
		kinds := repo.SubRepoKinds
		if len(kinds) == 0 {
			kinds = domain.AllSubRepoKinds()
		}
		for _, k := range kinds {
			subProjects = append(subProjects, domain.RepoSubProject{Kind: k})
		}
	}
	view.Suggestions = ci.Suggest(repo.Kind, subProjects, refs)
	return view, nil
}

func (s *Service) parseWorkflowRefs(ctx context.Context, repo domain.Repository) ([]ci.JobRef, bool) {
	if s.githubToken == nil || s.git == nil {
		return nil, false
	}
	token, err := s.githubToken(ctx)
	if err != nil || strings.TrimSpace(token) == "" {
		return nil, false
	}
	info, err := s.git.TaskGitInfo(ctx, repo.RootPath)
	if err != nil {
		return nil, false
	}
	defs, err := githubapi.ParseWorkflowJobs(ctx, token, info.Owner, info.Repo)
	if err != nil || len(defs) == 0 {
		return nil, false
	}
	refs := make([]ci.JobRef, 0, len(defs))
	for _, d := range defs {
		refs = append(refs, ci.JobRef{Key: d.Key, Name: d.Name, WorkflowFile: d.WorkflowFile})
	}
	return refs, true
}

// SavePipelineConfig persists the repo type fields and replaces the mapping set.
func (s *Service) SavePipelineConfig(ctx context.Context, repositoryID uuid.UUID, kind *string, subRepoKinds *[]string, autoRelease *bool, jobs []domain.RepositoryPipelineJob) (ci.ConfigView, error) {
	if kind != nil && !domain.ValidRepoKind(*kind) {
		return ci.ConfigView{}, fmt.Errorf("invalid repository kind: %s", *kind)
	}
	if _, err := s.repos.UpdateMeta(ctx, repositoryID, kind, subRepoKinds, autoRelease); err != nil {
		return ci.ConfigView{}, err
	}
	if s.pipelineJobs != nil {
		for i := range jobs {
			jobs[i].RepositoryID = repositoryID
		}
		if _, err := s.pipelineJobs.ReplaceForRepository(ctx, repositoryID, jobs); err != nil {
			return ci.ConfigView{}, err
		}
	}
	return s.GetPipelineConfig(ctx, repositoryID)
}

func (s *Service) List(ctx context.Context) ([]domain.Repository, error) {
	repos, err := s.repos.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range repos {
		repos[i] = s.withGitWarning(repos[i])
	}
	return repos, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Repository, error) {
	repo, err := s.repos.Get(ctx, id)
	if err != nil {
		return domain.Repository{}, err
	}
	return s.withGitWarning(repo), nil
}

// validateRequestKind rejects an explicitly supplied kind that is not one of
// the five known ones. Empty is fine everywhere — it means "detect it".
func validateRequestKind(kind string) error {
	kind = strings.TrimSpace(kind)
	if kind == "" || domain.ValidRepoKind(kind) {
		return nil
	}
	return fmt.Errorf("invalid repository kind: %s", kind)
}

func (s *Service) Open(ctx context.Context, req domain.OpenRepositoryRequest) (domain.Repository, error) {
	if err := validateRequestKind(req.Kind); err != nil {
		return domain.Repository{}, err
	}
	absRoot, err := workspace.ValidateProjectRoot(req.RootPath, s.workspaceRoot, s.allowedRoots)
	if err != nil {
		return domain.Repository{}, err
	}
	name := filepath.Base(absRoot)
	if name == "" || name == "." || name == string(os.PathSeparator) {
		name = absRoot
	}
	existing, existingErr := s.repos.GetByRootPath(ctx, absRoot)
	if existingErr == nil {
		if req.Description != "" && existing.Description != req.Description {
			updated, err := s.repos.Update(ctx, existing.ID, "", req.Description, nil, nil, nil, nil)
			if err != nil {
				return domain.Repository{}, err
			}
			existing = updated
		}
		// An explicit kind still lands on a repo that is merely being re-opened;
		// detection does not, because a kind chosen (or corrected) earlier must
		// not be silently overwritten by a guess.
		if kind := strings.TrimSpace(req.Kind); kind != "" && kind != existing.Kind {
			updated, err := s.repos.UpdateMeta(ctx, existing.ID, &kind, nil, nil)
			if err != nil {
				return domain.Repository{}, err
			}
			updated.ProjectIDs = existing.ProjectIDs
			existing = updated
		}
		if len(req.ProjectIDs) > 0 {
			if err := s.repos.SetProjects(ctx, existing.ID, req.ProjectIDs); err != nil {
				return domain.Repository{}, err
			}
			existing.ProjectIDs = req.ProjectIDs
		}
		s.ensureGitAsync(ctx, existing.ID, absRoot, name)
		existing = s.syncRemoteURL(ctx, existing, req.CloneURL)
		s.startIndex(ctx, existing.ID, absRoot)
		return s.withGitWarning(existing), nil
	}
	// Git zorunlu: kurulamıyorsa kayıt hiç oluşmaz (gitsiz proje açılmaz).
	if err := s.ensureGitSync(ctx, absRoot, name, req.Owner); err != nil {
		return domain.Repository{}, fmt.Errorf("git/github kurulumu: %w", err)
	}
	remoteURL := strings.TrimSpace(req.CloneURL)
	if remoteURL == "" && s.git != nil {
		remoteURL = s.git.OriginURL(ctx, absRoot)
	}
	// Kind is decided here, at the one place every registration funnels
	// through, so the repo is never left on the column default: the caller's
	// choice wins, otherwise the working copy is read.
	kind := strings.TrimSpace(req.Kind)
	var subProjects []domain.RepoSubProject
	if kind == "" {
		kind = DetectRepoKind(absRoot)
		if kind == domain.RepoKindMonorepo {
			subProjects = DetectRepoSubProjects(absRoot)
		}
	}
	// The platform is read off the working copy even when the CALLER named the
	// kind: "mobile" is a choice a human can make, "which platform" is a fact
	// about the tree, and asking the importer for it would be asking them to
	// retype what is already on disk.
	mobilePlatform := ""
	// The store identifiers are read here for a second reason on top of the
	// platform's: this is the one moment the code is guaranteed to be on the
	// disk of the host doing the reading. The deploy settings screen that wants
	// them is served by a shared agent-server that may hold no working copy at
	// all, so an on-demand read there would answer "" for every repository
	// imported from somebody's Mac.
	appIdentity := domain.AppIdentity{}
	// The build targets are read here for a third reason on top of those two:
	// nothing downstream has a human in front of it to correct them. They are
	// interpolated into the generated release script, so a value that could not
	// be read here stays unread all the way to the release, which refuses to
	// start rather than archiving a target nobody named.
	buildTargets := domain.BuildTargets{}
	if kind == domain.RepoKindMobile {
		mobilePlatform = DetectMobilePlatform(absRoot)
		appIdentity = DetectAppIdentity(absRoot)
		buildTargets = DetectBuildTargets(absRoot)
	}
	// absRoot is deliberately this host's absolute path, even though the same
	// database may also be served by another host (the cloud pod and the
	// user's Mac behind a reverse tunnel both run this binary). root_path is
	// advisory across hosts: the store re-anchors a foreign path onto the
	// reading host's workspace root (postgres.RepositoryStore.localizeRootPath),
	// so whichever host wrote the row, the other one still resolves it. Storing
	// a host-neutral value instead would mean inventing a second notion of
	// "where the code is" for the one host that can answer it truthfully.
	repo, err := s.repos.Create(ctx, name, strings.TrimSpace(req.Description), absRoot, remoteURL, kind)
	if err != nil {
		return domain.Repository{}, err
	}
	if len(req.ProjectIDs) > 0 {
		if err := s.repos.SetProjects(ctx, repo.ID, req.ProjectIDs); err != nil {
			return domain.Repository{}, err
		}
		repo.ProjectIDs = req.ProjectIDs
	}
	if mobilePlatform != "" {
		// Written after the insert rather than through it, the same way
		// sub_projects below is: both are detection results, and Create's
		// signature is the registration contract every caller shares.
		updated, err := s.repos.UpdateMobilePlatform(ctx, repo.ID, mobilePlatform)
		if err != nil {
			return domain.Repository{}, err
		}
		updated.ProjectIDs = repo.ProjectIDs
		repo = updated
	}
	if !appIdentity.IsZero() {
		updated, err := s.repos.UpdateDetectedAppIdentity(ctx, repo.ID, appIdentity)
		if err != nil {
			return domain.Repository{}, err
		}
		updated.ProjectIDs = repo.ProjectIDs
		repo = updated
	}
	if !buildTargets.IsZero() {
		updated, err := s.repos.UpdateDetectedBuildTargets(ctx, repo.ID, buildTargets)
		if err != nil {
			return domain.Repository{}, err
		}
		updated.ProjectIDs = repo.ProjectIDs
		repo = updated
	}
	if len(subProjects) > 0 {
		// Persisted before RefreshAsync is kicked off below, so that goroutine
		// never observes a half-written row. sub_repo_kinds is also seeded here
		// from the same detection: the row is brand new, nothing has chosen a
		// pipeline routing set yet, and this list is now the authoritative
		// statement of which kinds are present.
		updated, err := s.repos.UpdateSubProjects(ctx, repo.ID, subProjects)
		if err != nil {
			return domain.Repository{}, err
		}
		updated.ProjectIDs = repo.ProjectIDs
		repo = updated
		kinds := make([]string, 0, len(subProjects))
		seenKind := map[string]bool{}
		for _, sp := range subProjects {
			if !seenKind[sp.Kind] {
				seenKind[sp.Kind] = true
				kinds = append(kinds, sp.Kind)
			}
		}
		if updated, err := s.repos.UpdateMeta(ctx, repo.ID, nil, &kinds, nil); err == nil {
			updated.ProjectIDs = repo.ProjectIDs
			repo = updated
		}
	}
	s.startIndex(ctx, repo.ID, absRoot)
	// ctx is handed to all three for its identity, not its lifetime: each
	// starts a goroutine that outlives this request and each writes
	// policy-protected rows, so each detaches it with context.WithoutCancel.
	s.setupWebhookAsync(ctx, repo.ID)
	// First registration of this working copy: build the project profile in
	// the background so the first agent run already knows the basics.
	if s.profiles != nil {
		s.profiles.RefreshAsync(ctx, repo.ID, "import")
	}
	return s.withGitWarning(repo), nil
}

// ImportFromGitHub, GitHub reposunu çalışma alanına klonlayıp kayıt eder.
func (s *Service) ImportFromGitHub(ctx context.Context, req domain.ImportGitHubRepositoryRequest) (domain.Repository, error) {
	if err := validateRequestKind(req.Kind); err != nil {
		return domain.Repository{}, err
	}
	owner := strings.TrimSpace(req.Owner)
	name := strings.TrimSpace(req.Name)
	if owner == "" || name == "" {
		return domain.Repository{}, fmt.Errorf("owner and name are required")
	}
	if s.git == nil {
		return domain.Repository{}, fmt.Errorf("git client is not configured")
	}
	// Same layout helper the restore path uses, so a re-cloned working copy
	// always lands where a freshly imported one would.
	dest, err := s.workspaceRepoPath(name)
	if err != nil {
		return domain.Repository{}, err
	}
	cloneURL := strings.TrimSpace(req.CloneURL)
	if cloneURL == "" {
		cloneURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, name)
	}
	if _, err := os.Stat(dest); err == nil {
		if !s.git.HasGit(dest) {
			return domain.Repository{}, fmt.Errorf("directory already exists but is not a git repository: %s", dest)
		}
		// Skipping the clone means ADOPTING whatever is already there, so what
		// is there has to be this repository and not merely a repository.
		// Naming dest from the GitHub owner/repo is what makes a stray
		// checkout unlikely; this is what makes acting on one impossible.
		if err := s.assertSameRepo(ctx, dest, cloneURL); err != nil {
			return domain.Repository{}, err
		}
	} else {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := s.git.CloneRepo(cctx, cloneURL, dest); err != nil {
			return domain.Repository{}, err
		}
	}
	return s.Open(ctx, domain.OpenRepositoryRequest{
		RootPath:    dest,
		Description: strings.TrimSpace(req.Description),
		ProjectIDs:  req.ProjectIDs,
		Owner:       owner,
		// Carry the URL into the record: previously it was used to clone and
		// then thrown away, leaving nothing to re-clone from later.
		CloneURL: cloneURL,
		// Empty here means Open detects the kind from the fresh clone, which
		// is the normal case: the import UI has nothing to classify from until
		// the code is on disk.
		Kind: strings.TrimSpace(req.Kind),
	})
}

// ensureGitSync, yeni kayıtlar için git kurulumunu bloklayarak yapar.
func (s *Service) ensureGitSync(ctx context.Context, rootPath, name, owner string) error {
	if s.git == nil {
		return fmt.Errorf("git client is not configured")
	}
	if s.git.HasGit(rootPath) {
		return nil
	}
	gctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return s.git.EnsureRepoWithRemote(gctx, rootPath, name, owner)
}

// ctx is taken via context.WithoutCancel so this goroutine is not killed when
// the request returns: EnsureRepoWithRemote resolves the GitHub token through
// the client's token source, a read from the settings row.
func (s *Service) ensureGitAsync(ctx context.Context, repositoryID uuid.UUID, rootPath, name string) {
	if s.git == nil || s.git.HasGit(rootPath) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if err := s.git.EnsureRepoWithRemote(ctx, rootPath, name, ""); err != nil {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("git/github setup failed")
			s.gitWarnMu.Lock()
			s.gitWarnings[repositoryID] = "Git/GitHub setup failed: " + err.Error()
			s.gitWarnMu.Unlock()
			return
		}
		s.gitWarnMu.Lock()
		delete(s.gitWarnings, repositoryID)
		s.gitWarnMu.Unlock()
	}()
}

// syncRemoteURL persists the repository's origin when it is not recorded yet.
// Rows created before remote_url existed carry "", and the only place the URL
// still lives is the working copy's .git/config — so it has to be harvested
// while a clone is present, before the disk it sits on can disappear.
func (s *Service) syncRemoteURL(ctx context.Context, repo domain.Repository, hint string) domain.Repository {
	if repo.RemoteURL != "" {
		return repo
	}
	remote := strings.TrimSpace(hint)
	if remote == "" && s.git != nil {
		remote = s.git.OriginURL(ctx, repo.RootPath)
	}
	if remote == "" {
		return repo
	}
	updated, err := s.repos.UpdateRemoteURL(ctx, repo.ID, remote)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repo.ID.String()).Msg("could not persist repository remote url")
		repo.RemoteURL = remote
		return repo
	}
	updated.ProjectIDs = repo.ProjectIDs
	return updated
}

func (s *Service) withGitWarning(repo domain.Repository) domain.Repository {
	// The running (or last) restore rides along on every read, so a page
	// reloaded mid-clone still shows the clone rather than a dead card.
	repo.GitRestore = s.restoreState(repo.ID)
	s.gitWarnMu.Lock()
	warning := s.gitWarnings[repo.ID]
	s.gitWarnMu.Unlock()
	// Presence, not HasGit: a root path that does not resolve on this host is a
	// different problem from a folder nobody ran `git init` in, and telling the
	// user the second one when it is the first sends them to a folder that is
	// not there. The sentence itself belongs to domain.GitPresence.
	if s.git != nil {
		presence := s.git.Presence(repo.RootPath)
		if w := presence.Warning(); w != "" {
			repo.GitWarning = w
		}
		// Whether the code can be fetched here is a fact about the disk and the
		// recorded remote, not about which sentence won below: a repository
		// whose git/GitHub setup once failed is still restorable if its folder
		// is missing and its origin is known.
		restorable, _ := domain.CanRestoreWorkingCopy(presence, repo.RemoteURL)
		repo.GitRestorable = restorable
	}
	// A setup failure recorded by ensureGitAsync still wins the sentence: it is
	// the more specific answer, and it is about this repository rather than
	// about the disk.
	if warning != "" {
		repo.GitWarning = warning
	}
	return repo
}

func (s *Service) Create(ctx context.Context, req domain.CreateRepositoryRequest) (domain.Repository, error) {
	// Validated before the directory is made, so a bad kind cannot leave an
	// empty folder behind on the way to a 400.
	if err := validateRequestKind(req.Kind); err != nil {
		return domain.Repository{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return domain.Repository{}, fmt.Errorf("name is required")
	}
	parent := strings.TrimSpace(req.ParentDir)
	if parent == "" {
		// Klasör seçilmediyse depolar çalışma alanının repos/ klasöründe yaşar.
		repos, err := workspace.ReposDir(s.workspaceRoot)
		if err != nil {
			return domain.Repository{}, err
		}
		parent = repos
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return domain.Repository{}, fmt.Errorf("create repos dir: %w", err)
		}
	}
	parentDir, err := workspace.ValidateProjectRoot(parent, s.workspaceRoot, s.allowedRoots)
	if err != nil {
		return domain.Repository{}, err
	}
	rootPath := filepath.Join(parentDir, name)
	if _, err := os.Stat(rootPath); err == nil {
		return domain.Repository{}, fmt.Errorf("directory already exists: %s", rootPath)
	}
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		return domain.Repository{}, fmt.Errorf("create directory: %w", err)
	}
	return s.Open(ctx, domain.OpenRepositoryRequest{
		RootPath:    rootPath,
		Description: req.Description,
		ProjectIDs:  req.ProjectIDs,
		Owner:       req.Owner,
		// A brand-new empty directory has nothing to detect from, so whatever
		// the caller declares is all there is; empty settles on the default.
		Kind: strings.TrimSpace(req.Kind),
	})
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req domain.UpdateRepositoryRequest) (domain.Repository, error) {
	if req.Kind != nil && !domain.ValidRepoKind(strings.TrimSpace(*req.Kind)) {
		return domain.Repository{}, fmt.Errorf("invalid repository kind: %s", *req.Kind)
	}
	repo, err := s.repos.Update(ctx, id, req.Name, req.Description, req.VerifyCommand, req.BuildCommand, req.TestCommand, req.RequireHumanReview)
	if err != nil {
		return domain.Repository{}, err
	}
	// Kind lives on UpdateMeta, not Update — the request carried it since the
	// field was added but nothing ever forwarded it, so PATCHing a kind was a
	// silent no-op. It is applied after the base update so a single PATCH can
	// carry both.
	if req.Kind != nil {
		kind := strings.TrimSpace(*req.Kind)
		var kinds *[]string
		if req.SubProjects != nil {
			// An explicit sub_projects statement in the same PATCH also restates
			// sub_repo_kinds — the routing set derived from it — same as at
			// import. A kind-only PATCH leaves sub_repo_kinds alone.
			seenKind := map[string]bool{}
			deduped := make([]string, 0, len(*req.SubProjects))
			for _, sp := range *req.SubProjects {
				if !seenKind[sp.Kind] {
					seenKind[sp.Kind] = true
					deduped = append(deduped, sp.Kind)
				}
			}
			kinds = &deduped
		}
		updated, err := s.repos.UpdateMeta(ctx, id, &kind, kinds, nil)
		if err != nil {
			return domain.Repository{}, err
		}
		repo = updated
	}
	// Judged against the kind the repository has AFTER this request, so a PATCH
	// that turns a repo mobile and names its platform in one go is accepted,
	// and one that names a platform on a backend is not.
	if req.MobilePlatform != nil {
		platform := strings.TrimSpace(*req.MobilePlatform)
		if err := domain.ValidateMobilePlatform(platform, repo.Kind); err != nil {
			return domain.Repository{}, err
		}
		updated, err := s.repos.UpdateMobilePlatform(ctx, id, platform)
		if err != nil {
			return domain.Repository{}, err
		}
		repo = updated
	}
	// Same "judged against the kind this request leaves behind" rule as
	// mobile_platform above, and the same one-field-one-setter split.
	if req.ReleaseEngine != nil {
		engine := strings.TrimSpace(*req.ReleaseEngine)
		if err := domain.ValidateReleaseEngine(engine, repo.Kind); err != nil {
			return domain.Repository{}, err
		}
		// validateReleaseEngine accepts "" as "the default", but the column
		// itself must never hold one: ValidReleaseEngine("") is false, so a
		// blank would read as a bug everywhere downstream.
		if engine == "" {
			engine = domain.ReleaseEngineAuto
		}
		updated, err := s.repos.UpdateReleaseEngine(ctx, id, engine)
		if err != nil {
			return domain.Repository{}, err
		}
		repo = updated
	}
	if req.MutationEnabled != nil || req.MutationThreshold != nil {
		if req.MutationThreshold != nil && (*req.MutationThreshold < 0 || *req.MutationThreshold > 100) {
			return domain.Repository{}, fmt.Errorf(
				"mutation_threshold must be between 0 and 100 (0 means no bar), got %.1f", *req.MutationThreshold)
		}
		updated, err := s.repos.UpdateMutationGate(ctx, id, req.MutationEnabled, req.MutationThreshold)
		if err != nil {
			return domain.Repository{}, err
		}
		repo = updated
	}
	if req.Docs != nil {
		updated, err := s.repos.UpdateDocs(ctx, id, *req.Docs)
		if err != nil {
			return domain.Repository{}, err
		}
		repo = updated
	}
	if req.SubProjects != nil {
		cleaned, err := domain.ValidateSubProjects(*req.SubProjects)
		if err != nil {
			return domain.Repository{}, err
		}
		return s.repos.UpdateSubProjects(ctx, id, cleaned)
	}
	return repo, nil
}

// SetDocsTaskID records (or clears, with "") the reference-doc bundle task the
// repository is waiting on. Its own method rather than a field of
// UpdateRepositoryRequest: it is written by repodocs.Service, never by a human
// editing repository settings.
func (s *Service) SetDocsTaskID(ctx context.Context, repositoryID uuid.UUID, taskID string) error {
	return s.repos.SetDocsTaskID(ctx, repositoryID, taskID)
}

func (s *Service) SetProjects(ctx context.Context, repositoryID uuid.UUID, req domain.SetRepositoryProjectsRequest) (domain.Repository, error) {
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return domain.Repository{}, err
	}
	if err := s.repos.SetProjects(ctx, repositoryID, req.ProjectIDs); err != nil {
		return domain.Repository{}, err
	}
	return s.repos.Get(ctx, repositoryID)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	// indexer.Service exposes no cancel/stop API for an in-flight index job — only
	// IsProjectIndexActive, a read-only check. If a job is running for this repo we
	// cannot halt its background goroutine here; it will keep running against a
	// repository row that is about to disappear. This is safe from a data-integrity
	// standpoint because every table the indexer writes to (workspace_indexes and its
	// children) has ON DELETE CASCADE back to repositories, so no orphaned rows result
	// — but the goroutine itself keeps consuming CPU/LLM calls until it finishes.
	if s.indexer != nil && s.indexer.IsProjectIndexActive(id) {
		log.Warn().Str("repository_id", id.String()).Msg("deleting repository with an active index job; job will run to completion and its rows will cascade-delete")
	}
	return s.repos.Delete(ctx, id)
}

// freshnessCheckInterval is how often one repository's clone is compared with
// origin while its status is being polled.
const freshnessCheckInterval = 10 * time.Minute

func (s *Service) IndexStatus(ctx context.Context, repositoryID uuid.UUID) (domain.WorkspaceIndex, error) {
	if s.indexer == nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("indexer disabled")
	}
	idx, err := s.indexer.GetProjectStatus(ctx, repositoryID)
	if err != nil {
		return idx, err
	}
	repo, getErr := s.repos.Get(ctx, repositoryID)
	if idx.Status == domain.IndexStatusRunning && !s.indexer.IsProjectIndexActive(repositoryID) {
		if getErr == nil {
			s.restartIndex(ctx, repo.ID, repo.RootPath)
		}
	}
	if getErr == nil {
		s.ensureIndexFresh(ctx, repo, idx)
	}
	idx.SyncWarning = s.syncWarning(repositoryID)
	// Two questions a green "completed 100%" cannot answer on its own, both
	// answered here because this is the call the settings page polls:
	//
	//	is the code it describes current?   → SyncWarning, above
	//	are its vectors still usable?       → EmbeddingStale, here
	//
	// The second is the one that looks like nothing is wrong. A repository
	// whose embedding provider changed keeps a full, finished, correctly
	// counted index whose every vector belongs to a coordinate system no query
	// will ever be embedded into again — searches against it are refused
	// (postgres.IndexStore.assertEmbeddingComparable), and without this the
	// person looking at the card would see a healthy index and a search that
	// answers nothing, with no connection between the two.
	s.indexer.AnnotateEmbeddingProvenance(ctx, &idx)
	return idx, err
}

func (s *Service) SearchIndex(ctx context.Context, repositoryID uuid.UUID, query string, topK int) ([]domain.WorkspaceChunk, error) {
	if s.indexer == nil {
		return nil, fmt.Errorf("indexer disabled")
	}
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return nil, err
	}
	return s.indexer.SearchProject(ctx, repositoryID, query, topK)
}

func (s *Service) Reindex(ctx context.Context, repositoryID uuid.UUID) error {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return err
	}
	s.pullAndRestartIndex(ctx, repo.ID, repo.RootPath)
	return nil
}

// StopIndex cancels a running index pass. It reports whether one was running,
// so the caller can say "stopped" or "nothing was running" instead of guessing.
//
// The stopped index keeps every file it finished: the next pass re-reads only
// the files with no stored hash, so stopping is a pause, not a rollback.
func (s *Service) StopIndex(ctx context.Context, repositoryID uuid.UUID) (bool, error) {
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return false, err
	}
	if s.indexer == nil {
		return false, nil
	}
	stopped := s.indexer.StopIndexProject(repositoryID)
	if stopped {
		s.indexMu.Lock()
		delete(s.indexing, repositoryID)
		s.indexMu.Unlock()
	}
	return stopped, nil
}

// pullAndRestartIndex refreshes the project root from origin before rebuilding
// the index. The indexer walks the checked-out tree, so a reindex on a clone
// that was never pulled re-processes the commit it was created at — the pass
// completes, reports success, and still misses every push since. Only the
// explicit reindex pulls; the crash-recovery restart in GetIndexStatus keeps
// re-walking local state, which is all it is there to do.
//
// The pull runs in the background because the caller is an HTTP handler that
// only kicks the pass off, and a fetch is a network round trip.
func (s *Service) pullAndRestartIndex(ctx context.Context, repositoryID uuid.UUID, rootPath string) {
	s.pullAndRestartIndexNotify(ctx, repositoryID, rootPath, nil)
}

// pullAndRestartIndexNotify is pullAndRestartIndex with a completion hook,
// which the webhook debounce uses to learn when the pass is over. onDone also
// fires when the indexer is disabled, so a caller waiting on it never hangs.
//
// ctx is the caller's, not a bare context.Background(), and what matters about
// it is not its deadline (the pass deliberately outlives the request) but the
// rest of what it carries. See indexer.detachIndexContext, which strips the
// cancellation and keeps that.
func (s *Service) pullAndRestartIndexNotify(ctx context.Context, repositoryID uuid.UUID, rootPath string, onDone func()) {
	if s.indexer == nil {
		if onDone != nil {
			onDone()
		}
		return
	}
	s.indexMu.Lock()
	delete(s.indexing, repositoryID)
	s.indexing[repositoryID] = struct{}{}
	s.indexMu.Unlock()

	done := s.indexDone(repositoryID)
	finish := done
	if onDone != nil {
		finish = func() {
			done()
			onDone()
		}
	}
	passCtx := context.WithoutCancel(ctx)
	go func() {
		s.pullProjectRoot(passCtx, repositoryID, rootPath)
		s.indexer.RestartIndexProject(passCtx, repositoryID, rootPath, finish)
	}()
}

// pullProjectRoot is best effort for the index pass — indexing slightly old
// code beats refusing to index at all when the network or the remote is down —
// but the failure is recorded, not just logged. A silent failure here is how an
// index sat four commits behind for three weeks while the UI showed a green
// "completed 100%".
func (s *Service) pullProjectRoot(ctx context.Context, repositoryID uuid.UUID, rootPath string) {
	if s.git == nil || !s.git.HasGit(rootPath) {
		// Nothing to pull. This is not the "the clone is gone" case being
		// silently accepted: the index pass itself refuses to walk a missing
		// mirror (EnsureIndexMirror), and reporting a pull failure for a
		// checkout that does not exist would put the wrong sentence on the
		// index card.
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := s.git.SyncDefaultBranch(ctx, rootPath)
	s.recordSyncResult(repositoryID, err)
	if err != nil {
		log.Warn().Err(err).
			Str("repository_id", repositoryID.String()).
			Str("root", rootPath).
			Msg("reindex: project root could not be pulled, indexing local state")
	}
}

// recordSyncResult stores (or clears) why this repository's index is behind the
// code it describes. Cleared by the next pass that does bring it up to date.
func (s *Service) recordSyncResult(repositoryID uuid.UUID, err error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncWarnings == nil {
		s.syncWarnings = make(map[uuid.UUID]string)
	}
	if err == nil {
		delete(s.syncWarnings, repositoryID)
		return
	}
	s.syncWarnings[repositoryID] = err.Error()
}

func (s *Service) syncWarning(repositoryID uuid.UUID) string {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	return s.syncWarnings[repositoryID]
}

// claimFreshnessCheck rate-limits the clone-freshness probe: the index status
// endpoint is polled every few seconds while the settings page is open, and a
// git fetch per poll would hammer the remote.
func (s *Service) claimFreshnessCheck(repositoryID uuid.UUID) bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncCheckedAt == nil {
		s.syncCheckedAt = make(map[uuid.UUID]time.Time)
	}
	if last, ok := s.syncCheckedAt[repositoryID]; ok && time.Since(last) < freshnessCheckInterval {
		return false
	}
	s.syncCheckedAt[repositoryID] = time.Now()
	return true
}

// ensureIndexFresh pulls the project root and rebuilds the index when origin
// has moved past the commit the index was built from. Pushes normally arrive as
// webhooks, but a repository whose webhook was installed late — or whose pushes
// landed while the control plane was down — would otherwise keep serving the
// clone-time commit forever, since nothing else advances the working copy.
func (s *Service) ensureIndexFresh(ctx context.Context, repo domain.Repository, idx domain.WorkspaceIndex) {
	if s.git == nil || s.indexer == nil || !s.git.HasGit(repo.RootPath) {
		return
	}
	if idx.Status != domain.IndexStatusCompleted || s.indexer.IsProjectIndexActive(repo.ID) {
		return
	}
	if !s.claimFreshnessCheck(repo.ID) {
		return
	}

	// The caller's context, not its deadline: the probe outlives the poll that
	// triggered it.
	freshCtx := context.WithoutCancel(ctx)
	go func() {
		pullCtx, cancel := context.WithTimeout(freshCtx, 2*time.Minute)
		err := s.git.SyncDefaultBranch(pullCtx, repo.RootPath)
		cancel()
		s.recordSyncResult(repo.ID, err)
		if err != nil {
			log.Warn().Err(err).Str("repository_id", repo.ID.String()).Msg("index freshness: project root could not be pulled")
			return
		}
		head := indexer.GitHeadSHA(repo.RootPath)
		if head == "" || head == idx.CommitSHA {
			return
		}
		log.Info().
			Str("repository_id", repo.ID.String()).
			Str("indexed_commit", idx.CommitSHA).
			Str("head", head).
			Msg("index is behind origin; rebuilding")
		// The pull already ran, so this restart must not repeat it.
		s.restartIndex(freshCtx, repo.ID, repo.RootPath)
		// Same follow-up a push webhook triggers: refresh only the profile
		// sections the new commits touched. On an instance GitHub cannot
		// deliver webhooks to, this sweep is the only way a push reaches the
		// profile at all.
		if s.profiles != nil {
			s.profiles.RefreshAfterPush(freshCtx, repo.ID, "poll")
		}
	}()
}

// SweepIndexFreshnessAsync checks every repository's clone against origin once,
// at boot.
//
// The per-repository check already existed, but only ran off IndexStatus — a
// call that happens when a human opens the repository settings page. A push
// that landed while this pod was down (scale-to-zero, a deploy, a crash) is not
// redelivered by GitHub, so nothing else notices it: agents kept running
// against the commit the clone was left at, for as long as nobody opened that
// page. Booting is exactly when the missed pushes are known to exist, so this
// is where they are picked up.
//
// Called once per process (boot, then the periodic sweep loop): s.repos.List
// here is every repository on this install, so there is nothing to fan out.
func (s *Service) SweepIndexFreshness(ctx context.Context) {
	if s.indexer == nil || s.git == nil {
		return
	}
	func() {
		repos, err := s.repos.List(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("index freshness sweep: repository list failed")
			return
		}
		for _, repo := range repos {
			idx, err := s.indexer.GetProjectStatus(ctx, repo.ID)
			if err != nil {
				continue
			}
			s.ensureIndexFresh(ctx, repo, idx)
		}
	}()
}

func (s *Service) startIndex(ctx context.Context, repositoryID uuid.UUID, rootPath string) {
	s.startIndexWith(ctx, repositoryID, rootPath, false)
}

func (s *Service) restartIndex(ctx context.Context, repositoryID uuid.UUID, rootPath string) {
	s.startIndexWith(ctx, repositoryID, rootPath, true)
}

// startIndexWith hands the pass the caller's context; the indexer strips the
// cancellation itself, because a pass must survive the request that started
// it.
func (s *Service) startIndexWith(ctx context.Context, repositoryID uuid.UUID, rootPath string, force bool) {
	if s.indexer == nil {
		return
	}
	if force {
		s.indexMu.Lock()
		delete(s.indexing, repositoryID)
		s.indexing[repositoryID] = struct{}{}
		s.indexMu.Unlock()
		s.indexer.RestartIndexProject(ctx, repositoryID, rootPath, s.indexDone(repositoryID))
		return
	}
	s.indexMu.Lock()
	if _, running := s.indexing[repositoryID]; running {
		s.indexMu.Unlock()
		return
	}
	s.indexing[repositoryID] = struct{}{}
	s.indexMu.Unlock()
	s.indexer.StartIndexProject(ctx, repositoryID, rootPath, s.indexDone(repositoryID))
}

func (s *Service) indexDone(repositoryID uuid.UUID) func() {
	return func() {
		s.indexMu.Lock()
		delete(s.indexing, repositoryID)
		s.indexMu.Unlock()
	}
}

func (s *Service) ListTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return nil, err
	}
	tasks, err := s.tasks.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	return s.withLatestPipelineStatus(ctx, tasks)
}

func (s *Service) ListAllTasks(ctx context.Context) ([]domain.BoardTask, error) {
	tasks, err := s.tasks.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	if tasks == nil {
		tasks = []domain.BoardTask{}
	}
	return s.withLatestPipelineStatus(ctx, tasks)
}

// ListBoardTasks is what the board renders: every task except the released ones
// that have been sitting there longer than domain.ReleasedBoardWindow. Those
// are still whole tasks — they are read through ListReleasedArchive, which is
// the page the board links to. ListAllTasks stays unwindowed because its
// callers (id lookups, sweepers) are asking about the tasks that EXIST.
func (s *Service) ListBoardTasks(ctx context.Context) ([]domain.BoardTask, error) {
	tasks, err := s.tasks.ListBoardVisible(ctx, time.Now().Add(-domain.ReleasedBoardWindow))
	if err != nil {
		return nil, err
	}
	if tasks == nil {
		tasks = []domain.BoardTask{}
	}
	return s.withLatestPipelineStatus(ctx, tasks)
}

// ListReleasedArchive is the released column in full, newest first, searchable
// by key, title or description.
func (s *Service) ListReleasedArchive(ctx context.Context, query string, limit int) ([]domain.BoardTask, error) {
	tasks, err := s.tasks.ListReleasedArchive(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if tasks == nil {
		tasks = []domain.BoardTask{}
	}
	return s.withLatestPipelineStatus(ctx, tasks)
}

// withLatestPipelineStatus enriches a task list with each task's latest
// pipeline status using a single bulk query (avoids N+1 queries per task).
func (s *Service) withLatestPipelineStatus(ctx context.Context, tasks []domain.BoardTask) ([]domain.BoardTask, error) {
	if s.pipelineStore == nil || len(tasks) == 0 {
		return tasks, nil
	}
	ids := make([]uuid.UUID, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	statuses, err := s.pipelineStore.LatestStatusByTasks(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		if digest, ok := statuses[tasks[i].ID]; ok {
			tasks[i].LatestPipelineStatus = digest.Status
			// Why the gate opened, when it opened without a build. The card
			// needs both: "skipped" alone cannot distinguish a repository with
			// no CI from one whose CI ran out of quota while the card waited.
			tasks[i].LatestPipelineGateReason = digest.GateReason
		}
	}
	return tasks, nil
}

func (s *Service) LookupTaskByKey(ctx context.Context, key string) (domain.BoardTask, error) {
	prefix, number, err := parseTaskKey(key)
	if err != nil {
		return domain.BoardTask{}, err
	}
	return s.tasks.LookupByKey(ctx, prefix, number)
}

func parseTaskKey(key string) (string, int, error) {
	parts := strings.Split(strings.TrimSpace(key), "-")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid task key format, expected PREFIX-NUMBER")
	}
	var number int
	if _, err := fmt.Sscanf(parts[1], "%d", &number); err != nil || number <= 0 {
		return "", 0, fmt.Errorf("invalid task key number")
	}
	return taskkey.NormalizeKeyPrefix(parts[0]), number, nil
}

func (s *Service) FindTaskRepositoryID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	tasks, err := s.ListAllTasks(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	for _, task := range tasks {
		if task.ID == taskID {
			return task.RepositoryID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("task not found")
}

func (s *Service) DefaultRepositoryID(ctx context.Context) (uuid.UUID, error) {
	repos, err := s.repos.List(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if len(repos) == 0 {
		return uuid.Nil, fmt.Errorf("no repositories; add a repository before creating board tasks")
	}
	return repos[0].ID, nil
}

func (s *Service) GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	return s.enrichTask(ctx, task)
}

func (s *Service) enrichTask(ctx context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	if s.criteria != nil {
		criteria, err := s.criteria.ListByTask(ctx, task.ID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.AcceptanceCriteria = criteria
	}
	if s.testCases != nil {
		cases, err := s.testCases.ListByTask(ctx, task.ID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.TestCases = cases
	}
	if s.relations != nil {
		rels, err := s.relations.ListBySource(ctx, task.ID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.Relations = rels
		// The other end of the blocks graph. Read here rather than derived by
		// the caller because the two directions are stored in one table and
		// nothing outside this package should have to know which end a `blocks`
		// row is written from.
		blockedBy, err := s.relations.ListBlockedBy(ctx, task.ID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.BlockedBy = blockedBy
	}
	if s.documents != nil {
		docs, err := s.documents.ListByTask(ctx, task.ID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.Documents = docs
	}
	if s.attachments != nil {
		metas, err := s.attachments.ListMetaByTask(ctx, task.ID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.Attachments = metas
	}
	return task, nil
}

func (s *Service) CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	taskType := req.TaskType
	if taskType == "" {
		taskType = domain.TaskTypeTask
	}
	if !domain.ValidTaskType(taskType) {
		return domain.BoardTask{}, fmt.Errorf("invalid task type: %s", taskType)
	}
	priority := req.Priority
	if priority == "" {
		priority = domain.TaskPriorityMedium
	}
	if !domain.ValidTaskPriority(priority) {
		return domain.BoardTask{}, fmt.Errorf("invalid priority: %s", priority)
	}
	col := req.Column
	if col == "" {
		col = domain.TaskColumnBacklog
	}
	if err := s.validateColumn(ctx, col); err != nil {
		return domain.BoardTask{}, err
	}
	if err := s.validateMoveAllowed(ctx, uuid.Nil, col); err != nil {
		return domain.BoardTask{}, err
	}
	createdBy := req.CreatedBy
	if createdBy == "" {
		createdBy = "user"
	}
	existing, err := s.tasks.ListByRepository(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	position := 0
	for _, t := range existing {
		if t.Column == col && t.Position >= position {
			position = t.Position + 1
		}
	}
	taskNumber, err := s.tasks.NextTaskNumber(ctx, taskType)
	if err != nil {
		return domain.BoardTask{}, err
	}
	assignee := req.AssigneeAgentID
	if taskType == domain.TaskTypeAnaliz {
		if resolved := s.resolveAnalizAssignee(ctx, repo); resolved != nil {
			assignee = resolved
		}
	}
	task, err := s.tasks.Create(ctx, domain.BoardTask{
		RepositoryID:         repositoryID,
		TaskNumber:           taskNumber,
		Title:                strings.TrimSpace(req.Title),
		TaskType:             taskType,
		Description:          req.Description,
		TechnicalDescription: req.TechnicalDescription,
		InitiativeProjectID:  req.InitiativeProjectID,
		Column:               col,
		Position:             position,
		Priority:             priority,
		CreatedBy:            createdBy,
		AssigneeAgentID:      assignee,
		BeforeDeploy:         req.BeforeDeploy,
		AfterDeploy:          req.AfterDeploy,
		RollbackPlan:         req.RollbackPlan,
	})
	if err != nil {
		return domain.BoardTask{}, err
	}
	if s.criteria != nil && len(req.AcceptanceCriteria) > 0 {
		criteria, err := s.criteria.ReplaceForTask(ctx, task.ID, req.AcceptanceCriteria)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.AcceptanceCriteria = criteria
	}
	if s.relations != nil && len(req.Relations) > 0 {
		resolved, err := s.resolveRelations(ctx, req.Relations)
		if err != nil {
			return domain.BoardTask{}, err
		}
		// Every deploy dependency is cycle-checked before it is written, for the
		// same reason a blocker is: an order nothing can satisfy is a deadlock
		// with a gate holding it shut, and the release path would only discover
		// it once somebody tried to ship.
		for _, rel := range resolved {
			if rel.RelationType != domain.TaskRelationDeployDependsOn {
				continue
			}
			if err := s.guardDeployOrderCycle(ctx, task.ID, rel.TargetTaskID); err != nil {
				return domain.BoardTask{}, err
			}
		}
		rels, err := s.relations.ReplaceForTask(ctx, task.ID, resolved)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.Relations = rels
	}
	if s.relations != nil && len(req.BlockedBy) > 0 {
		blockers, err := s.SetBlockers(ctx, task.ID, req.BlockedBy)
		if err != nil {
			return domain.BoardTask{}, err
		}
		task.BlockedBy = blockers
	}
	// The generated ordering block goes into before_deploy as soon as the order
	// is known, not at release time, so the card states it for the whole life of
	// the task. Release regenerates it, which is what keeps it true.
	if len(req.Relations) > 0 || len(req.BlockedBy) > 0 {
		task = s.syncOrderNote(ctx, task)
	}
	if s.documents != nil {
		for i, docReq := range req.Documents {
			authorType := docReq.CreatedByType
			if authorType == "" {
				authorType = createdBy
				if authorType != "agent" {
					authorType = "user"
				}
			}
			pos := docReq.Position
			if pos == 0 {
				pos = i
			}
			doc, err := s.documents.Create(ctx, domain.TaskDocument{
				TaskID:        task.ID,
				Title:         strings.TrimSpace(docReq.Title),
				Content:       docReq.Content,
				Position:      pos,
				CreatedByType: authorType,
				CreatedByID:   docReq.CreatedByID,
			})
			if err != nil {
				return domain.BoardTask{}, err
			}
			task.Documents = append(task.Documents, doc)
		}
	}
	createdPayload := map[string]interface{}{
		"column":    string(task.Column),
		"task_type": string(task.TaskType),
		"priority":  string(task.Priority),
	}
	if task.AssigneeAgentID != nil {
		createdPayload["assignee_agent_id"] = task.AssigneeAgentID.String()
	}
	_ = s.emit(ctx, repo, task, domain.BoardEventTaskCreated, createdPayload)
	return task, nil
}

func (s *Service) resolveRelations(ctx context.Context, inputs []domain.TaskRelationInput) ([]domain.TaskRelationInput, error) {
	out := make([]domain.TaskRelationInput, 0, len(inputs))
	for _, rel := range inputs {
		if rel.RelationType == "" {
			rel.RelationType = domain.TaskRelationBlocks
		}
		// Caught here rather than at the database: the schema's CHECK would
		// reject an unknown type with a constraint-violation error that names
		// nothing the caller can act on.
		if !domain.ValidTaskRelationType(rel.RelationType) {
			return nil, fmt.Errorf("invalid relation type: %s", rel.RelationType)
		}
		if rel.TargetTaskID != uuid.Nil {
			out = append(out, rel)
			continue
		}
		if rel.TargetKey == "" {
			return nil, fmt.Errorf("relation requires target_task_id or target_key")
		}
		target, err := s.LookupTaskByKey(ctx, rel.TargetKey)
		if err != nil {
			return nil, err
		}
		rel.TargetTaskID = target.ID
		out = append(out, rel)
	}
	return out, nil
}

func (s *Service) UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	prevColumn := task.Column
	prevAssignee := task.AssigneeAgentID
	if req.Title != nil {
		task.Title = *req.Title
	}
	if req.TaskType != nil {
		if !domain.ValidTaskType(*req.TaskType) {
			return domain.BoardTask{}, fmt.Errorf("invalid task type: %s", *req.TaskType)
		}
		task.TaskType = *req.TaskType
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.TechnicalDescription != nil {
		task.TechnicalDescription = *req.TechnicalDescription
	}
	if req.InitiativeProjectID != nil {
		task.InitiativeProjectID = req.InitiativeProjectID
	}
	if req.BeforeDeploy != nil {
		task.BeforeDeploy = req.BeforeDeploy
	}
	if req.AfterDeploy != nil {
		task.AfterDeploy = req.AfterDeploy
	}
	if req.RollbackPlan != nil {
		task.RollbackPlan = req.RollbackPlan
	}
	if req.Priority != nil {
		if !domain.ValidTaskPriority(*req.Priority) {
			return domain.BoardTask{}, fmt.Errorf("invalid priority: %s", *req.Priority)
		}
		task.Priority = *req.Priority
	}
	if req.Column != nil {
		if err := s.validateColumn(ctx, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		if err := validateAgentSelfMove(task, req, prevColumn); err != nil {
			return domain.BoardTask{}, err
		}
		if *req.Column != prevColumn && s.columns != nil {
			if err := s.columns.ValidateTransition(ctx, string(prevColumn), string(*req.Column)); err != nil {
				return domain.BoardTask{}, err
			}
		}
		if err := s.validateMoveAllowed(ctx, task.ID, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		if err := s.criteriaGate(ctx, task.ID, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		if err := s.criteriaReviewGate(ctx, task.ID, prevColumn, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		// The QA phase owes the board its round, not only its verdicts: which
		// cases were derived, which were executed, and which were rejected as
		// invalid. See testCaseGate.
		if err := s.testCaseGate(ctx, task.ID, prevColumn, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		// The two terminal-column gates: done must have been earned by the
		// task's review chain, and released must have been earned by a real
		// production deploy. Both are per-repository opt-ins and both apply
		// to every mover — human, agent and the control plane itself.
		if err := s.reviewChainGate(ctx, repo, task, prevColumn, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		if err := s.releaseDeployGate(ctx, repo, task, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		task.Column = *req.Column
	}
	if req.Position != nil {
		task.Position = *req.Position
	}
	if req.AssigneeAgentID.Present {
		task.AssigneeAgentID = req.AssigneeAgentID.Value
	}
	// Review hand-back: with require_human_review on, a reviewing agent's
	// approval is recorded as a verdict and the task waits where it is. The
	// human performs the move, which is what makes an escaped defect
	// attributable to the reviewer that approved it.
	if req.Column != nil && *req.Column != prevColumn && s.reviewGate != nil {
		if !s.reviewGate.InterceptAgentMove(ctx, task, prevColumn, *req.Column, req.Actor, repo) {
			return task, nil
		}
	}
	// The release gate compares the commit a task was signed off at against the
	// branch as it stands at dispatch time, so the stamp has to be written by
	// the move that earns it — in the same write, or a crash in between leaves
	// a done task claiming a sign-off for the wrong code.
	if req.Column != nil && *req.Column != prevColumn {
		task.VerifiedSHA = s.verifiedSHAForMove(ctx, task, prevColumn, *req.Column)
	}
	updated, err := s.tasks.Update(ctx, task)
	if err != nil {
		return domain.BoardTask{}, err
	}
	if req.Column != nil && *req.Column != prevColumn {
		if *req.Column == domain.TaskColumnNeedRevision && req.Actor == domain.TaskActorHuman && s.reviewGate != nil {
			s.reviewGate.OnHumanRejection(ctx, updated, prevColumn)
		}
		movePayload := map[string]interface{}{
			"from_column": string(prevColumn),
			"to_column":   string(*req.Column),
		}
		// The dispatcher must know which agent produced this move so it never
		// re-dispatches that same agent on its own event (claim→move→new run loop).
		if req.Actor == domain.TaskActorAgent && req.ActorAgentID != nil {
			movePayload["actor_agent_id"] = req.ActorAgentID.String()
		}
		// Who moved it, recorded explicitly. A pipeline/verification move used to
		// carry no actor at all, and task history rendered that as "by User" —
		// the human was shown moves they never made.
		switch req.Actor {
		case domain.TaskActorAgent:
			movePayload[domain.EventPayloadActor] = domain.EventActorAgent
		case domain.TaskActorHuman:
			movePayload[domain.EventPayloadActor] = domain.EventActorHuman
		default:
			movePayload[domain.EventPayloadActor] = domain.EventActorSystem
			if req.SystemReason != "" {
				movePayload[domain.EventPayloadReason] = req.SystemReason
			}
		}
		_ = s.emit(ctx, repo, updated, domain.BoardEventTaskMoved, movePayload)
		if s.scorer != nil {
			s.scorer.OnColumnTransition(ctx, updated, prevColumn, *req.Column)
		}
		if s.completion != nil {
			s.completion.OnColumnTransition(ctx, updated, prevColumn, *req.Column)
		}
		if s.evolution != nil && *req.Column == domain.TaskColumnNeedRevision {
			s.evolution.NotifyRevision(ctx, updated)
		}
		// A task entering ready_for_qa starts a fresh verification round: stale
		// verdicts from the previous round would let the review gates pass on
		// criteria nobody re-tested. Coming back from in_qa is the same round,
		// so those verdicts survive.
		if s.criteria != nil && *req.Column == domain.TaskColumnReadyForQA && prevColumn != domain.TaskColumnInQA {
			if cerr := s.criteria.ClearChecksForTask(ctx, updated.ID); cerr != nil {
				log.Warn().Err(cerr).Str("task_id", updated.ID.String()).Msg("clear criterion checks failed")
			}
		}
		// The PR is opened when a task enters code_review so the
		// validate/build/test pipeline can read PR-linked GitHub Actions runs.
		// It is re-ensured on the later columns because that is also how a task
		// whose first attempt raced the branch push gets one at all — and by
		// `done` it has to exist, since that is where it gets merged.
		if *req.Column == domain.TaskColumnCodeReview || *req.Column == domain.TaskColumnPMUAT || *req.Column == domain.TaskColumnDone {
			s.ensurePullRequestAsync(ctx, updated)
		}
		// code_review entry → validate/build/test GitHub Actions gate; the
		// architect reviews the diff only after it goes green.
		if s.pipelines != nil && *req.Column == domain.TaskColumnCodeReview {
			if _, perr := s.pipelines.Trigger(ctx, repositoryID, updated, domain.PipelineTriggerReadyForQA); perr != nil {
				log.Warn().Err(perr).Str("task_id", updated.ID.String()).Msg("pipeline trigger failed")
			}
		}
		// The schema-change check reads the branch diff, so it runs as soon as
		// the work is reviewable — long before anyone tries to release it.
		if *req.Column == domain.TaskColumnCodeReview || *req.Column == domain.TaskColumnReadyForQA {
			s.DetectTaskMigration(ctx, updated)
		}
		// Staging deploys follow the repo's test strategy: local runs nothing,
		// stage deploys once before QA, per_step also deploys at code review.
		if s.pipelines != nil {
			strategy := s.testStrategy(ctx, repositoryID)
			deployNow := (*req.Column == domain.TaskColumnReadyForQA && domain.DeploysForQA(strategy)) ||
				(*req.Column == domain.TaskColumnCodeReview && domain.DeploysOnCodeReview(strategy))
			if deployNow {
				// A store-shipped stage target does not deploy until the app's
				// onboarding checklist is verified (test_ready or later) — the
				// same block TriggerRelease enforces for prod, one lifecycle
				// stage earlier.
				if gerr := s.mobileStoreGate(ctx, repositoryID, domain.DeployEnvStage); gerr != nil {
					log.Warn().Err(gerr).Str("task_id", updated.ID.String()).Msg("stage deploy blocked by mobile store gate")
					if s.comments != nil {
						_, _ = s.comments.Create(ctx, domain.TaskComment{
							TaskID:     updated.ID,
							AuthorType: "system",
							Content:    "Stage deploy blocked: " + gerr.Error(),
						})
					}
				} else if _, perr := s.pipelines.TriggerDeploy(ctx, repositoryID, updated, domain.PipelineTriggerStageDeploy); perr != nil {
					log.Warn().Err(perr).Str("task_id", updated.ID.String()).Msg("stage deploy trigger failed")
				}
			}
		}
	}
	if req.AssigneeAgentID.Present && !assigneeEqual(prevAssignee, req.AssigneeAgentID.Value) {
		// Both sides of the hand-off go in the payload so the activity feed can
		// say "X → Y" instead of just "assignment changed".
		assignPayload := map[string]interface{}{}
		if prevAssignee != nil {
			assignPayload["from_agent_id"] = prevAssignee.String()
		}
		if updated.AssigneeAgentID != nil {
			assignPayload["to_agent_id"] = updated.AssigneeAgentID.String()
		}
		// Same reason the move payload carries it: without the actor the
		// dispatcher cannot tell an agent's own assignment from someone else's,
		// and re-dispatches that agent on its own event.
		if req.Actor == domain.TaskActorAgent && req.ActorAgentID != nil {
			assignPayload["actor_agent_id"] = req.ActorAgentID.String()
		}
		_ = s.emit(ctx, repo, updated, domain.BoardEventTaskAssigned, assignPayload)
	}
	// Ordering last, after the row is written: a relation pointing at a task
	// whose own update failed would outlive the change that asked for it.
	if req.DeployDependsOn != nil {
		if _, rerr := s.ReplaceDeployDependencies(ctx, updated.ID, *req.DeployDependsOn); rerr != nil {
			return domain.BoardTask{}, rerr
		}
	}
	if len(req.BlockedBy) > 0 {
		if _, rerr := s.SetBlockers(ctx, updated.ID, req.BlockedBy); rerr != nil {
			return domain.BoardTask{}, rerr
		}
	}
	if req.DeployDependsOn != nil || len(req.BlockedBy) > 0 {
		updated = s.syncOrderNote(ctx, updated)
	}
	return s.enrichTask(ctx, updated)
}

// ReplaceDeployDependencies swaps the task's deploy_depends_on relations for
// the supplied set, leaving its blocks relations untouched. Targets may be
// given by id or by board key; a task may not depend on itself, which would
// deadlock its own release gate.
func (s *Service) ReplaceDeployDependencies(ctx context.Context, taskID uuid.UUID, inputs []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	if s.relations == nil {
		return nil, fmt.Errorf("relation store unavailable")
	}
	for i := range inputs {
		inputs[i].RelationType = domain.TaskRelationDeployDependsOn
	}
	resolved, err := s.resolveRelations(ctx, inputs)
	if err != nil {
		return nil, err
	}
	for _, rel := range resolved {
		if rel.TargetTaskID == taskID {
			return nil, fmt.Errorf("a task cannot deploy-depend on itself")
		}
		// Checked against the graph as it stands, before anything is written.
		// The replace is wholesale, so an edge in the incoming set cannot be
		// part of a cycle with an edge this call is about to delete.
		if err := s.guardDeployOrderCycle(ctx, taskID, rel.TargetTaskID); err != nil {
			return nil, err
		}
	}
	return s.relations.ReplaceForTaskOfType(ctx, taskID, domain.TaskRelationDeployDependsOn, resolved)
}

// ensurePullRequestAsync opens (or finds) the task's pull request off the
// request path. It was createDraftPRAsync and opened a draft; task PRs are
// opened ready for review now — see git.EnsurePullRequest for why a draft made
// every task PR unmergeable.
//
// It only records the PR on the task row — it no longer also announces it with
// a system comment. The card already shows the link (get_task_pull_request,
// the task detail drawer), and this runs on every column that re-ensures the
// PR (code_review, PM UAT, done), so a comment here repeated the same link up
// to three times per task for no new information.
//
// ctx is taken via context.WithoutCancel: it resolves the GitHub token and
// writes the PR back onto the task row after the request that triggered it has
// already returned.
func (s *Service) ensurePullRequestAsync(ctx context.Context, task domain.BoardTask) {
	if s.git == nil || s.workspaceRoot == "" {
		return
	}
	workspacePath := s.taskWorkspacePath(task.ID)
	if workspacePath == "" || !s.git.HasGit(workspacePath) {
		return
	}
	run := s.prAsyncRun
	if run == nil {
		run = func(fn func()) { go fn() }
	}
	run(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		url, err := s.git.EnsurePullRequest(ctx, workspacePath)
		if err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("task pull request create failed")
			return
		}
		number, _ := domain.ParsePullRequestNumber(url)
		if setErr := s.tasks.SetTaskPullRequest(ctx, task.ID, url, number); setErr != nil {
			log.Warn().Err(setErr).Str("task_id", task.ID.String()).Msg("recording the task's pull request failed")
		}
	})
}

// validateAgentSelfMove rejects an assignee agent pushing its OWN task into a
// column it must never own the transition to. need_revision belongs to
// reviewers (architect/PM/QA hand work back with a comment), and todo would
// abandon claimed work. Blocked stays allowed: an agent with a question about
// its own task may park it there. DE-4 showed what the gap produces: the
// implementer moved its own task to need_revision, was then re-dispatched to
// "read the revision comment" that never existed, and asked the human what
// the revision was. The error text is the tool result the LLM reads — it must
// teach the correct move, not just deny.
func validateAgentSelfMove(task domain.BoardTask, req domain.UpdateBoardTaskRequest, prevColumn domain.TaskColumn) error {
	if req.Actor != domain.TaskActorAgent || req.Column == nil || *req.Column == prevColumn {
		return nil
	}
	if req.ActorAgentID == nil || task.AssigneeAgentID == nil || *req.ActorAgentID != *task.AssigneeAgentID {
		return nil
	}
	switch *req.Column {
	case domain.TaskColumnNeedRevision:
		return fmt.Errorf("you are this task's assignee: need_revision is reserved for reviewers handing work back. Fix the work yourself and move the task to code_review when done; if you are missing information, use ask_user instead")
	case domain.TaskColumnTodo:
		return fmt.Errorf("you claimed this task: do not move it back to todo. Continue the work and move it to code_review when done; if you are missing information, use ask_user")
	}
	return nil
}

func (s *Service) validateMoveAllowed(ctx context.Context, taskID uuid.UUID, target domain.TaskColumn) error {
	if taskID == uuid.Nil {
		return nil
	}
	// todo is joining the queue, not starting the work; the real gate at
	// dispatch time is board.WorkOrder, which already covers todo too. Only
	// the transition into in_progress — work actually starting — is refused
	// here.
	if target != domain.TaskColumnInProgress {
		return nil
	}
	if s.relations == nil {
		return nil
	}
	blockers, err := s.relations.ListBlockingSources(ctx, taskID)
	if err != nil {
		return err
	}
	if len(blockers) > 0 {
		// Naming them is the difference between an error a caller can act on
		// and one it can only report. "task is blocked by incomplete tasks"
		// sent an agent looking for a blocker the board never showed it, and a
		// human dragging the card had nothing to open.
		labels := make([]string, 0, len(blockers))
		for _, b := range blockers {
			labels = append(labels, domain.RelationLabel(b.Key, b.Title, b.ID)+" ["+string(b.Column)+"]")
		}
		return fmt.Errorf("work order: this task is blocked until these are done: %s", strings.Join(labels, ", "))
	}
	return nil
}

func (s *Service) ClaimTask(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) (domain.BoardTask, error) {
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return domain.BoardTask{}, err
	}
	task, err := s.tasks.ClaimAssignee(ctx, repositoryID, taskID, agentID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	return s.enrichTask(ctx, task)
}

func (s *Service) DeleteTask(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	if err := s.tasks.Delete(ctx, repositoryID, taskID); err != nil {
		return err
	}
	// The checkout outlives the row unless something removes it, and until now
	// nothing did: a repository with node_modules is a few hundred megabytes per
	// task, and the only symptom is an ENOSPC weeks later in an unrelated task.
	// A deleted task is the unambiguous case — no row is left to reopen, so
	// nothing can want the directory back.
	//
	// After the delete rather than before, and never fatal: the row is what the
	// caller asked to remove, and a wedged unlink must not resurrect it.
	if s.workspaceRoot != "" {
		if err := workspace.RemoveDirWithin(s.workspaceRoot, s.taskWorkspacePath(taskID)); err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Msg("board: removing the workspace of a deleted task failed")
		}
	}
	return nil
}

func (s *Service) ReplaceAcceptanceCriteria(ctx context.Context, repositoryID, taskID uuid.UUID, items []domain.AcceptanceCriterionInput) ([]domain.AcceptanceCriterion, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.criteria == nil {
		return nil, fmt.Errorf("acceptance criteria not enabled")
	}
	return s.criteria.ReplaceForTask(ctx, taskID, items)
}

func (s *Service) UpdateCriterionCompleted(ctx context.Context, repositoryID, taskID, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.AcceptanceCriterion{}, err
	}
	if s.criteria == nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("acceptance criteria not enabled")
	}
	return s.criteria.UpdateCompleted(ctx, criterionID, completed)
}

func (s *Service) ListComments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskComment, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.comments == nil {
		return []domain.TaskComment{}, nil
	}
	comments, err := s.comments.ListByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return s.nameCommentAuthors(ctx, comments), nil
}

// nameCommentAuthors fills AuthorName for agent-written comments so the UI can
// say which agent spoke instead of showing a bare UUID. A roster lookup failure
// is not fatal: the comments still return, just without names.
func (s *Service) nameCommentAuthors(ctx context.Context, comments []domain.TaskComment) []domain.TaskComment {
	if s.agentLister == nil {
		return comments
	}
	needed := false
	for i := range comments {
		if comments[i].AuthorType == "agent" && comments[i].AuthorID != "" {
			needed = true
			break
		}
	}
	if !needed {
		return comments
	}
	agents, err := s.agentLister(ctx)
	if err != nil {
		return comments
	}
	names := make(map[string]string, len(agents))
	for i := range agents {
		names[agents[i].ID.String()] = agents[i].Name
	}
	for i := range comments {
		if comments[i].AuthorType != "agent" {
			continue
		}
		if name := names[comments[i].AuthorID]; name != "" {
			comments[i].AuthorName = name
		}
	}
	return comments
}

func (s *Service) AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.TaskComment{}, err
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskComment{}, err
	}
	if s.comments == nil {
		return domain.TaskComment{}, fmt.Errorf("comments not enabled")
	}
	authorType := req.AuthorType
	if authorType == "" {
		authorType = "user"
	}
	var actorUserID *string
	if uid := registry.ActorUserIDFromContext(ctx); uid != "" {
		actorUserID = &uid
	}
	comment, err := s.comments.Create(ctx, domain.TaskComment{
		TaskID:      taskID,
		AuthorType:  authorType,
		AuthorID:    req.AuthorID,
		Content:     strings.TrimSpace(req.Content),
		ActorUserID: actorUserID,
	})
	if err != nil {
		return domain.TaskComment{}, err
	}
	comment = s.nameCommentAuthors(ctx, []domain.TaskComment{comment})[0]
	_ = s.emit(ctx, repo, task, domain.BoardEventTaskCommented, map[string]interface{}{
		"comment_id":  comment.ID.String(),
		"content":     comment.Content,
		"author_type": comment.AuthorType,
		"author_id":   comment.AuthorID,
		"author_name": comment.AuthorName,
	})
	return comment, nil
}

func (s *Service) ListDocuments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskDocument, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.documents == nil {
		return []domain.TaskDocument{}, nil
	}
	return s.documents.ListByTask(ctx, taskID)
}

func (s *Service) AddDocument(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskDocumentRequest) (domain.TaskDocument, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskDocument{}, err
	}
	if s.documents == nil {
		return domain.TaskDocument{}, fmt.Errorf("documents not enabled")
	}
	authorType := req.CreatedByType
	if authorType == "" {
		authorType = "user"
	}
	return s.documents.Create(ctx, domain.TaskDocument{
		TaskID:        taskID,
		Title:         strings.TrimSpace(req.Title),
		Content:       req.Content,
		Position:      req.Position,
		CreatedByType: authorType,
		CreatedByID:   req.CreatedByID,
	})
}

func (s *Service) UpdateDocument(ctx context.Context, repositoryID, taskID, docID uuid.UUID, req domain.UpdateTaskDocumentRequest) (domain.TaskDocument, error) {
	doc, err := s.documents.Get(ctx, taskID, docID)
	if err != nil {
		return domain.TaskDocument{}, err
	}
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskDocument{}, err
	}
	if req.Title != nil {
		doc.Title = *req.Title
	}
	if req.Content != nil {
		doc.Content = *req.Content
	}
	if req.Position != nil {
		doc.Position = *req.Position
	}
	return s.documents.Update(ctx, doc)
}

func (s *Service) DeleteDocument(ctx context.Context, repositoryID, taskID, docID uuid.UUID) error {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return err
	}
	return s.documents.Delete(ctx, taskID, docID)
}

func (s *Service) ListTaskRuns(ctx context.Context, repositoryID, taskID uuid.UUID, runs port.TaskAgentRunStore, limit int) ([]domain.TaskAgentRun, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if runs == nil {
		return []domain.TaskAgentRun{}, nil
	}
	return runs.ListByTask(ctx, taskID, limit)
}

// ListTaskEvents returns one task's board events oldest-first — its history of
// column moves, assignments, comments and creation.
func (s *Service) ListTaskEvents(ctx context.Context, repositoryID, taskID uuid.UUID, events port.BoardEventStore, limit int) ([]domain.BoardEvent, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if events == nil {
		return []domain.BoardEvent{}, nil
	}
	return events.ListByTask(ctx, taskID, limit)
}

// ListTaskPipelines returns pipeline runs for a task (most recent first, jobs
// attached), validating the task belongs to the repository first.
func (s *Service) ListTaskPipelines(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskPipeline, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.pipelineStore == nil {
		return []domain.TaskPipeline{}, nil
	}
	return s.pipelineStore.ListByTask(ctx, taskID)
}

// GetTaskPipeline fetches a single pipeline run (jobs attached), validating
// the task belongs to the repository and the pipeline belongs to the task.
func (s *Service) GetTaskPipeline(ctx context.Context, repositoryID, taskID, pipelineID uuid.UUID) (domain.TaskPipeline, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskPipeline{}, err
	}
	if s.pipelineStore == nil {
		return domain.TaskPipeline{}, fmt.Errorf("pipeline store unavailable")
	}
	pipeline, err := s.pipelineStore.Get(ctx, pipelineID)
	if err != nil {
		return domain.TaskPipeline{}, err
	}
	if pipeline.TaskID != taskID {
		return domain.TaskPipeline{}, fmt.Errorf("task pipeline not found")
	}
	return pipeline, nil
}

// LatestTaskPipeline returns the most recent pipeline run for a task (jobs
// attached), validating the task belongs to the repository first — the same
// scoping ListTaskPipelines / GetTaskPipeline enforce, so a caller cannot
// read another repository's build logs via a planted task_id. When the task
// has no pipeline runs yet, the store returns domain.ErrPipelineNotFound,
// which callers treat as "nothing to report" rather than a failure.
func (s *Service) LatestTaskPipeline(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPipeline, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskPipeline{}, err
	}
	if s.pipelineStore == nil {
		return domain.TaskPipeline{}, domain.ErrPipelineNotFound
	}
	pipeline, err := s.pipelineStore.LatestByTask(ctx, taskID)
	if err != nil {
		return domain.TaskPipeline{}, err
	}
	// Defense in depth: LatestByTask is keyed by taskID (already validated
	// against repositoryID above), but never hand out a pipeline row that
	// claims a different repository.
	if pipeline.RepositoryID != repositoryID {
		return domain.TaskPipeline{}, domain.ErrPipelineNotFound
	}
	return pipeline, nil
}

// ErrReleaseDisabled signals the repo opted out of per-task auto release (it
// uses batched release trains), so trigger_release is a no-op.
var ErrReleaseDisabled = fmt.Errorf("repository uses batched release; automatic prod deploy on the done column is off")

// TriggerRelease dispatches the prod deploy workflow for a done task. The
// pipeline runner moves the task to released on success or need_revision on
// failure. Returns ErrReleaseDisabled when AutoReleaseOnDone is off.
func (s *Service) TriggerRelease(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPipeline, error) {
	return s.triggerRelease(ctx, repositoryID, taskID, releaseOptions{})
}

// releaseOptions are the internal knobs on the release path. Never reachable
// from HTTP or from a tool: both go through TriggerRelease, which passes the
// zero value and so keeps today's behaviour exactly.
type releaseOptions struct {
	// FromPackage skips the AutoReleaseOnDone check. That flag means "do not
	// release each task automatically as it reaches done" — it is the whole
	// reason deploy packages exist. Honouring it here would make a package on a
	// batched-release repository (the only kind that needs one) refuse every
	// member it was assembled from.
	FromPackage bool
}

func (s *Service) triggerRelease(ctx context.Context, repositoryID, taskID uuid.UUID, opts releaseOptions) (domain.TaskPipeline, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.TaskPipeline{}, err
	}
	if !repo.AutoReleaseOnDone && !opts.FromPackage {
		return domain.TaskPipeline{}, ErrReleaseDisabled
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskPipeline{}, err
	}
	if s.pipelines == nil {
		return domain.TaskPipeline{}, fmt.Errorf("pipeline runner unavailable")
	}
	// The column the trigger_release tool has always CLAIMED to require, now
	// actually required.
	//
	// Its description says "for a task that is in the done column" and its
	// task_id parameter says "must be in the done column", and nothing checked
	// it: an agent could release a card sitting in in_progress, in code_review
	// or in need_revision, and the gates below would let it through — the
	// migration gate reads a flag, the dependency gate reads other tasks, and
	// the release-target gate compares the branch against verified_sha, which
	// a task that never reached done simply does not have. That last one turned
	// the miss into a confusing error (ErrReleaseTargetUnverified, "no verified
	// commit is stamped") instead of the obvious one.
	//
	// `released` is accepted alongside `done` because a re-release of an
	// already-released task is a legitimate operation (a deploy package
	// replaying its members, a re-run after an infrastructure failure) and the
	// card has moved on by then. Everything else is refused before any deploy
	// is dispatched and before any comment is posted.
	if err := s.releaseColumnGate(ctx, repositoryID, task); err != nil {
		return domain.TaskPipeline{}, err
	}
	// A schema change that stage never applied does not go to production, no
	// matter which column the task reached.
	if err := s.migrationGate(ctx, repositoryID, task); err != nil {
		return domain.TaskPipeline{}, err
	}
	// Nothing ships ahead of what it declares it must deploy after. The only
	// gate that reads OTHER tasks — the rest can all pass on a task whose
	// dependency does not exist in production yet.
	if err := s.deployDependencyGate(ctx, repositoryID, task); err != nil {
		return domain.TaskPipeline{}, err
	}
	// A store-shipped prod target does not release until the app is actually
	// live in the store — the first store submit is a manual product
	// decision (listing, screenshots, privacy forms) this gate never makes.
	if err := s.mobileStoreGate(ctx, repositoryID, domain.DeployEnvProd); err != nil {
		if s.comments != nil {
			_, _ = s.comments.Create(ctx, domain.TaskComment{
				TaskID:     task.ID,
				AuthorType: "system",
				Content:    "Release blocked: " + err.Error(),
			})
		}
		log.Warn().Str("task_id", task.ID.String()).Str("repository_id", repositoryID.String()).
			Msg("release blocked: mobile app not ready")
		return domain.TaskPipeline{}, err
	}
	// Last gate before dispatch, and the only one that re-reads the world:
	// the gates above trust what the task row says, but "done" is a state,
	// not a commit. Re-resolve what the branch points at now and refuse to
	// ship anything except the commit that was signed off.
	if err := s.releaseTargetGate(ctx, repositoryID, task); err != nil {
		return domain.TaskPipeline{}, err
	}
	// Start the release chain at preprod when the repo has a preprod workflow
	// mapped; the runner promotes preprod → prod on success. No preprod → deploy
	// straight to prod (runner still releases if prod is unmapped).
	trigger := domain.PipelineTriggerProdDeploy
	if s.pipelineJobs != nil {
		if jobs, jerr := s.pipelineJobs.ListByRepository(ctx, repositoryID); jerr == nil {
			for _, j := range jobs {
				if j.Category == domain.PipelineCategoryPreProdDeploy && j.TargetKind == domain.PipelineTargetWorkflow && strings.TrimSpace(j.TargetRef) != "" {
					trigger = domain.PipelineTriggerPreProdDeploy
					break
				}
			}
		}
	}
	pipeline, err := s.pipelines.TriggerDeploy(ctx, repositoryID, task, trigger)
	if err != nil {
		return domain.TaskPipeline{}, err
	}
	// Regenerated here rather than trusted from creation time. The ordering may
	// have been edited since (update_board_task replaces deploy_depends_on
	// wholesale), and the checklist about to be posted is the last moment the
	// statement can still be made true before the deploy goes out.
	s.postPreDeployChecklist(ctx, s.syncOrderNote(ctx, task))
	return pipeline, nil
}

// postPreDeployChecklist puts the task's pre-deploy runbook on the task at the
// moment the deploy is dispatched.
//
// This replaces the instruction that used to live in the trigger_release tool
// description ("post a short pre-deploy checklist as a comment before
// calling"). Asking the model to write the checklist from scratch each time
// produced a different checklist every release, and only when an agent —
// rather than the pipeline — was the one releasing. Reading it back from the
// structured fields makes it the same text every time, and it appears whoever
// dispatched.
//
// Silent when both fields are empty: a comment saying "no checklist" is noise
// on every release of every task that does not need one.
func (s *Service) postPreDeployChecklist(ctx context.Context, task domain.BoardTask) {
	if s.comments == nil {
		return
	}
	var sections []string
	if before := trimmedPtr(task.BeforeDeploy); before != "" {
		// The fence markers are HTML comments — invisible wherever this is
		// rendered as markdown, noise wherever it is not (a notification, a
		// terminal, a plain-text export). They are meaningful in the stored
		// field, which is regenerated; they are meaningless in a comment, which
		// is a snapshot.
		before = strings.ReplaceAll(before, domain.OrderNoteOpen+"\n", "")
		before = strings.ReplaceAll(before, domain.OrderNoteOpen, "")
		before = strings.ReplaceAll(before, "\n"+domain.OrderNoteClose, "")
		before = strings.ReplaceAll(before, domain.OrderNoteClose, "")
		sections = append(sections, "Deploy öncesi kontrol listesi:\n"+strings.TrimSpace(before))
	}
	if rollback := trimmedPtr(task.RollbackPlan); rollback != "" {
		sections = append(sections, "Rollback planı:\n"+rollback)
	}
	if len(sections) == 0 {
		return
	}
	if _, err := s.comments.Create(ctx, domain.TaskComment{
		TaskID:     task.ID,
		AuthorType: "system",
		Content:    strings.Join(sections, "\n\n"),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("pre-deploy checklist comment failed")
	}
}

// trimmedPtr reads an optional text field as a trimmed string. A pointer to
// whitespace is the same as no value everywhere these fields are consumed.
func trimmedPtr(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// TriggerTaskPipeline manually (re-)triggers the QA-gate pipeline for a task.
func (s *Service) TriggerTaskPipeline(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPipeline, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskPipeline{}, err
	}
	if s.pipelines == nil {
		return domain.TaskPipeline{}, fmt.Errorf("pipeline runner unavailable")
	}
	return s.pipelines.Trigger(ctx, repositoryID, task, domain.PipelineTriggerManual)
}

func (s *Service) ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return "", err
	}
	return repo.RootPath, nil
}

// ListDirectories lists the immediate subdirectories under rel (repo-relative,
// "" for the root) inside repositoryID's working copy — the folder picker
// behind manually adding a monorepo sub-project reads real directories
// instead of taking a free-text path. Returns the cleaned repo-relative path
// alongside the child directory names.
// Returns the cleaned repo-relative path, that directory's own detected kind
// (so the picker can default a manually-added sub-project's kind correctly
// instead of a hardcoded guess), and its child directory names.
func (s *Service) ListDirectories(ctx context.Context, repositoryID uuid.UUID, rel string) (string, string, []string, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return "", "", nil, err
	}
	abs, err := workspace.ResolveWithinRoot(repo.RootPath, rel)
	if err != nil {
		return "", "", nil, err
	}
	absRoot, err := filepath.Abs(repo.RootPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve repository root: %w", err)
	}
	relClean, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve %q: %w", rel, err)
	}
	if relClean == "." {
		relClean = ""
	} else {
		relClean = filepath.ToSlash(relClean)
	}
	entries, err := ListChildDirectories(abs)
	if err != nil {
		return "", "", nil, err
	}
	kind, _ := classifyDir(abs)
	return relClean, kind, entries, nil
}

func (s *Service) ResolveDescription(ctx context.Context, repositoryID uuid.UUID) (string, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return "", err
	}
	return repo.Description, nil
}

func (s *Service) ResolveRepository(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error) {
	return s.repos.Get(ctx, repositoryID)
}

// GetProfile returns the agent-maintained project profile and its timestamp.
func (s *Service) GetProfile(ctx context.Context, repositoryID uuid.UUID) (string, *time.Time, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return "", nil, err
	}
	return repo.ProfileMD, repo.ProfileUpdatedAt, nil
}

// ProfileForRun renders the profile narrowed to the sections a run of the
// given kind needs. It falls back to the stored markdown whenever sections are
// unavailable, so a run never loses its brief to a lookup failure.
func (s *Service) ProfileForRun(ctx context.Context, repositoryID uuid.UUID, kind string) string {
	if s.profiles == nil {
		return ""
	}
	sections, err := s.profiles.Sections(ctx, repositoryID)
	if err != nil || len(sections) == 0 {
		return ""
	}
	return domain.RenderProfileMarkdown(domain.SelectProfileSections(sections, kind))
}

// ProfileDetail is the settings page's view of the profile: the rendered
// markdown plus the sections behind it (with their evidence and staleness)
// and the settings proposals the last profiling pass produced.
type ProfileDetail struct {
	ProfileMD string                   `json:"profile_md"`
	UpdatedAt *time.Time               `json:"profile_updated_at,omitempty"`
	Sections  []domain.ProfileSection  `json:"sections"`
	Proposals []domain.ProfileProposal `json:"proposals"`
}

// GetProfileDetail returns the full profile view. Section and proposal lookup
// failures degrade to an empty list rather than failing the page: the rendered
// markdown is still worth showing.
func (s *Service) GetProfileDetail(ctx context.Context, repositoryID uuid.UUID) (ProfileDetail, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return ProfileDetail{}, err
	}
	out := ProfileDetail{
		ProfileMD: repo.ProfileMD,
		UpdatedAt: repo.ProfileUpdatedAt,
		Sections:  []domain.ProfileSection{},
		Proposals: []domain.ProfileProposal{},
	}
	if s.profiles == nil {
		return out, nil
	}
	if sections, serr := s.profiles.Sections(ctx, repositoryID); serr != nil {
		log.Warn().Err(serr).Str("repository_id", repositoryID.String()).Msg("profile sections lookup failed")
	} else if sections != nil {
		out.Sections = sections
	}
	if proposals, perr := s.profiles.Proposals(ctx, repositoryID); perr != nil {
		log.Warn().Err(perr).Str("repository_id", repositoryID.String()).Msg("profile proposals lookup failed")
	} else if proposals != nil {
		out.Proposals = proposals
	}
	return out, nil
}

// ApplyProfileProposal writes a proposed setting into the repository.
func (s *Service) ApplyProfileProposal(ctx context.Context, repositoryID, proposalID uuid.UUID) (domain.ProfileProposal, error) {
	if s.profiles == nil {
		return domain.ProfileProposal{}, fmt.Errorf("profile proposals unavailable")
	}
	return s.profiles.ApplyProposal(ctx, repositoryID, proposalID)
}

// DismissProfileProposal records that a human declined a proposed setting.
func (s *Service) DismissProfileProposal(ctx context.Context, repositoryID, proposalID uuid.UUID) (domain.ProfileProposal, error) {
	if s.profiles == nil {
		return domain.ProfileProposal{}, fmt.Errorf("profile proposals unavailable")
	}
	return s.profiles.DismissProposal(ctx, repositoryID, proposalID)
}

// RefreshProfile starts a manual background rebuild of the project profile.
// started=false means one is already running (the caller reports
// "already_running" instead of kicking a second run).
func (s *Service) RefreshProfile(ctx context.Context, repositoryID uuid.UUID) (bool, error) {
	if s.profiles == nil {
		return false, fmt.Errorf("profile refresh unavailable")
	}
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return false, err
	}
	return s.profiles.RefreshAsync(ctx, repositoryID, "manual"), nil
}

func (s *Service) validateColumn(ctx context.Context, col domain.TaskColumn) error {
	if s.columns == nil {
		if !domain.ValidTaskColumn(col) {
			return fmt.Errorf("invalid column: %s", col)
		}
		return nil
	}
	return s.columns.ValidateColumn(ctx, string(col))
}

func (s *Service) emit(ctx context.Context, repo domain.Repository, task domain.BoardTask, eventType domain.BoardEventType, payload map[string]interface{}) error {
	if s.dispatcher == nil {
		return nil
	}
	return s.dispatcher.Dispatch(ctx, board.DispatchInput{
		RepositoryID: repo.ID,
		Task:         task,
		EventType:    eventType,
		Payload:      payload,
	})
}

func assigneeEqual(a *uuid.UUID, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// SetIncidentPolicy changes what a production incident on this repository
// triggers: nothing, a proposal-only diagnosis task, or a full board fix.
func (s *Service) SetIncidentPolicy(ctx context.Context, repositoryID uuid.UUID, policy domain.IncidentPolicy) (domain.Repository, error) {
	if !domain.ValidIncidentPolicy(policy) {
		return domain.Repository{}, fmt.Errorf("invalid incident policy %q", policy)
	}
	return s.repos.UpdateIncidentPolicy(ctx, repositoryID, policy)
}

// ResumeUnfinishedIndexes restarts, once at startup, every repository index a
// previous process left running or failed. Nothing else picks up a failed
// index, and one that failed because the embedder dropped a connection would
// otherwise sit at a few percent until someone pressed reindex. The pass is not
// forced, so files already indexed are skipped by their stored hashes.
func (s *Service) ResumeUnfinishedIndexes(ctx context.Context) {
	if s.indexer == nil {
		return
	}
	repos, err := s.repos.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("index resume: repository list failed")
		return
	}
	for _, repo := range repos {
		idx, err := s.indexer.GetProjectStatus(ctx, repo.ID)
		if err != nil {
			continue
		}
		if idx.Status != domain.IndexStatusRunning && idx.Status != domain.IndexStatusFailed {
			continue
		}
		if s.indexer.IsProjectIndexActive(repo.ID) {
			continue
		}
		log.Info().Str("repository_id", repo.ID.String()).Str("status", string(idx.Status)).
			Int("files_processed", idx.FilesProcessed).Int("files_total", idx.FilesTotal).
			Msg("resuming an index the previous run did not finish")
		s.startIndex(ctx, repo.ID, repo.RootPath)
	}
}
