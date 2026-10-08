package repository

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/htmldoc"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
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

type RevisionNotifier interface {
	NotifyRevision(ctx context.Context, task domain.BoardTask)
}

// ModelRefresher is the project-model service's compat seam for what used to
// be a profile refresh: opening, syncing or pushing to a repository now
// starts a scan instead.
type ModelRefresher interface {
	RefreshAsync(ctx context.Context, repositoryID uuid.UUID, reason string) bool

	RefreshIfStale(ctx context.Context, repositoryID uuid.UUID, reason string)

	RefreshAfterPush(ctx context.Context, repositoryID uuid.UUID, reason string)

	RefreshIfNeverScanned(ctx context.Context, repositoryID uuid.UUID, reason string)
}

// ComponentResolver validates a task's component_id: it must name a
// component of the task's own repository, active, not dismissed.
type ComponentResolver interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
	ComponentByPath(ctx context.Context, repositoryID uuid.UUID, path string) (domain.Component, error)
}

type Service struct {
	repos            port.RepositoryStore
	tasks            port.BoardTaskStore
	criteria         port.AcceptanceCriterionStore
	testCases        port.TaskTestCaseStore
	relations        port.TaskRelationStore
	documents        port.TaskDocumentStore
	annotations      port.TaskDocumentAnnotationStore
	questions        port.TaskQuestionStore
	comments         port.TaskCommentStore
	attachments      port.AttachmentStore
	columns          ColumnValidator
	dispatcher       *board.Dispatcher
	scorer           *board.ScoreTracker
	completion       *board.CompletionStamper
	reviewGate       *board.ReviewGate
	reviewQuorum     *board.ReviewQuorum
	workOrderSweeper *board.WorkOrderSweeper
	evolution        RevisionNotifier
	pipelines        *board.PipelineRunner
	pipelineStore    port.TaskPipelineStore
	spans            StageEvidence
	requireCriteria  bool
	indexer          *indexer.Service
	allowedRoots     []string
	indexMu          sync.Mutex
	indexing         map[uuid.UUID]struct{}
	git              port.GitClient
	deployTargets    port.DeployTargetStore
	mobileStoreApps  port.MobileStoreAppStore
	storeTestBuilds  StoreTestBuilder
	designSystems    DesignSystemApprover
	workspaceRoot    string
	gitWarnMu        sync.Mutex
	gitWarnings      map[uuid.UUID]string

	restoreMu sync.Mutex
	restores  map[uuid.UUID]*domain.RepositoryRestore

	restoreRun func(fn func())

	prAsyncRun func(fn func())

	syncMu        sync.Mutex
	syncWarnings  map[uuid.UUID]string
	syncCheckedAt map[uuid.UUID]time.Time
	syncFailures  map[uuid.UUID]int

	pipelineJobs   port.RepositoryPipelineJobStore
	githubToken    func(ctx context.Context) (string, error)
	modelRefresher ModelRefresher
	components     ComponentResolver

	publicBaseURL       string
	githubAPIBase       string
	pushReindexInterval time.Duration
	pushRunFn           func(repositoryID uuid.UUID)
	pushMu              sync.Mutex
	pushRepos           map[uuid.UUID]*pushRepoState
	pushDeliveries      map[string]time.Time

	deliveries DeliveryLedger

	workflows port.WorkflowReader
	roles     port.RoleResolver

	ciSetupMu sync.Mutex
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
		syncFailures:  make(map[uuid.UUID]int),
	}
}

func (s *Service) SetScorer(st *board.ScoreTracker) {
	s.scorer = st
}

func (s *Service) SetCompletionStamper(cs *board.CompletionStamper) {
	s.completion = cs
}

func (s *Service) SetReviewGate(g *board.ReviewGate) {
	s.reviewGate = g
}

func (s *Service) SetReviewQuorum(q *board.ReviewQuorum) {
	s.reviewQuorum = q
}

// TaskReviews is the per-reviewer verdict history of the task's code_review
// rounds; empty when no quorum ledger is wired.
func (s *Service) TaskReviews(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskReviews, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskReviews{}, err
	}
	return s.reviewQuorum.TaskReviews(ctx, task)
}

func (s *Service) SetAnnotationStore(store port.TaskDocumentAnnotationStore) {
	s.annotations = store
}

func (s *Service) SetQuestionStore(store port.TaskQuestionStore) {
	s.questions = store
}

func (s *Service) SetEvolution(n RevisionNotifier) {
	s.evolution = n
}

func (s *Service) SetWorkOrderSweeper(w *board.WorkOrderSweeper) {
	s.workOrderSweeper = w
}

func (s *Service) SetModelRefresher(m ModelRefresher) {
	s.modelRefresher = m
}

func (s *Service) SetComponentResolver(c ComponentResolver) {
	s.components = c
}

// validateTaskComponent rejects a component_id from a different repository or
// one the scan/human has dismissed — a task must never point at a component
// that no longer exists in this repository's model.
func (s *Service) validateTaskComponent(ctx context.Context, repositoryID, componentID uuid.UUID) error {
	if s.components == nil {
		return fmt.Errorf("component resolver unavailable")
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return fmt.Errorf("component: %w", err)
	}
	if comp.RepositoryID != repositoryID {
		return fmt.Errorf("component belongs to a different repository")
	}
	if comp.Status != domain.ComponentStatusActive {
		return fmt.Errorf("component %q is not active", comp.Path)
	}
	return nil
}

func (s *Service) SetPipelineRunner(pr *board.PipelineRunner) {
	s.pipelines = pr
}

func (s *Service) SetPipelineStore(store port.TaskPipelineStore) {
	s.pipelineStore = store
}

func (s *Service) SetRequireCriteriaComplete(require bool) {
	s.requireCriteria = require
}

func (s *Service) SetAttachmentStore(store port.AttachmentStore) {
	s.attachments = store
}

func (s *Service) ListTaskCriteria(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	if s.criteria == nil {
		return nil, fmt.Errorf("criteria store unavailable")
	}
	return s.criteria.ListByTask(ctx, taskID)
}

func (s *Service) SetTaskCriterionCompleted(ctx context.Context, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error) {
	if s.criteria == nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("criteria store unavailable")
	}
	return s.criteria.UpdateCompleted(ctx, criterionID, completed)
}

func (s *Service) SetTaskCriterionCanceled(ctx context.Context, criterionID uuid.UUID, canceled bool, reason string, authorType, authorID string) (domain.AcceptanceCriterion, error) {
	if s.criteria == nil {
		return domain.AcceptanceCriterion{}, fmt.Errorf("criteria store unavailable")
	}
	reason = strings.TrimSpace(reason)
	if canceled && reason == "" {
		return domain.AcceptanceCriterion{}, errors.New(prompt.Text(criterionCancelReasonRequiredKey))
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

func (s *Service) ReviewTaskCriterion(ctx context.Context, criterionID, agentID uuid.UUID, approved bool, note string) (domain.CriterionCheck, error) {
	if s.criteria == nil {
		return domain.CriterionCheck{}, fmt.Errorf("criteria store unavailable")
	}
	note = strings.TrimSpace(note)
	if !approved && note == "" {
		return domain.CriterionCheck{}, errors.New(prompt.Text(criterionRejectNoteRequiredKey))
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
	wf, err := s.workflow(ctx, task.TaskType)
	if err != nil {
		return domain.CriterionCheck{}, fmt.Errorf("workflow unavailable for task %s: %w", task.Key, err)
	}
	channel, ok := wf.Param(task.Column, domain.BehaviourCriterionVerdict, "channel")
	var role domain.CriterionReviewRole
	switch {
	case ok && channel == string(domain.CriterionReviewRoleQA):
		role = domain.CriterionReviewRoleQA
	case ok && channel == string(domain.CriterionReviewRolePM):
		role = domain.CriterionReviewRolePM
	default:
		return domain.CriterionCheck{}, fmt.Errorf("%s", criterionVerdictWrongColumnKey.Render(criterionVerdictWrongColumnInput{Key: task.Key, Column: string(task.Column)}))
	}
	check := domain.CriterionCheck{CriterionID: criterionID, Role: role, Approved: approved, Note: note}
	if agentID != uuid.Nil {
		check.AgentID = &agentID
	}
	check.VerifiedSHA = s.currentTaskSHA(ctx, task.ID)
	check.VerifiedPatchID = s.currentTaskPatchID(ctx, task.ID)
	return s.criteria.UpsertCheck(ctx, check)
}

func (s *Service) currentTaskSHA(ctx context.Context, taskID uuid.UUID) string {
	if s.git == nil {
		return ""
	}
	path := s.taskWorkspacePath(taskID)
	if path == "" || !s.git.HasGit(path) {
		return ""
	}
	info, err := s.git.TaskGitInfo(ctx, path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(info.HeadSHA)
}

// patchIDGit is the optional half of port.GitClient: adding TaskPatchID to
// the interface itself would force every test fake that embeds it to grow
// the method too, so it is reached with a type-assert instead, and its
// absence (any fake that doesn't implement it) is exactly the "lookup
// failed" case the diff-skip stage already fails open on.
type patchIDGit interface {
	TaskPatchID(ctx context.Context, workspacePath string) (string, error)
}

func (s *Service) currentTaskPatchID(ctx context.Context, taskID uuid.UUID) string {
	if s.git == nil {
		return ""
	}
	pg, ok := s.git.(patchIDGit)
	if !ok {
		return ""
	}
	path := s.taskWorkspacePath(taskID)
	if path == "" || !s.git.HasGit(path) {
		return ""
	}
	id, err := pg.TaskPatchID(ctx, path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(id)
}

func (s *Service) criteriaGate(ctx context.Context, taskID uuid.UUID, taskType domain.TaskType, target domain.TaskColumn) error {
	if !s.requireCriteria || s.criteria == nil {
		return nil
	}
	wf, err := s.workflow(ctx, taskType)
	if err != nil {
		return fmt.Errorf("criteria gate: workflow unavailable for %s (%w)", target, err)
	}
	if !wf.Has(target, domain.BehaviourRequireCriteriaComplete) {
		return nil
	}
	items, err := s.criteria.ListByTask(ctx, taskID)
	if err != nil || len(items) == 0 {
		return nil
	}
	var open []string
	var openCriteria []domain.CriteriaGateCriterion
	for _, c := range items {

		if !c.Settled() && !criterionApprovedByAReviewer(c) {
			open = append(open, criterionRef(c))
			openCriteria = append(openCriteria, domain.CriteriaGateCriterion{ID: c.ID, Text: c.Text})
		}
	}
	if len(open) == 0 {
		return nil
	}

	msg := criteriaIncompleteKey.Render(criteriaIncompleteInput{Target: string(target), Count: len(open), Open: open})
	return domain.NewCriteriaGateError(target, domain.CriteriaGateReasonIncomplete, openCriteria, msg)
}

func criterionRef(c domain.AcceptanceCriterion) string {
	return fmt.Sprintf("[%s] %s", c.ID, c.Text)
}

func criterionApprovedByAReviewer(c domain.AcceptanceCriterion) bool {
	for i := range c.Checks {
		if c.Checks[i].Approved {
			return true
		}
	}
	return false
}

func (s *Service) criteriaReviewGate(ctx context.Context, taskID uuid.UUID, taskType domain.TaskType, prev, target domain.TaskColumn) error {
	if !s.requireCriteria || s.criteria == nil {
		return nil
	}
	wf, err := s.workflow(ctx, taskType)
	if err != nil {
		return fmt.Errorf("criteria review gate: workflow unavailable for %s (%w)", target, err)
	}
	if !wf.Has(target, domain.BehaviourForwardExit) {
		return nil
	}
	channel, ok := wf.Param(prev, domain.BehaviourCriterionVerdict, "channel")
	var role domain.CriterionReviewRole
	switch {
	case ok && channel == string(domain.CriterionReviewRoleQA):
		role = domain.CriterionReviewRoleQA
	case ok && channel == string(domain.CriterionReviewRolePM):
		role = domain.CriterionReviewRolePM
	default:
		return nil
	}
	items, err := s.criteria.ListByTask(ctx, taskID)
	if err != nil || len(items) == 0 {
		return nil
	}
	var unchecked, rejected []string
	var uncheckedCriteria, rejectedCriteria []domain.CriteriaGateCriterion
	for _, c := range items {

		if c.Canceled {
			continue
		}
		verdict := criterionCheckFor(c, role)
		switch {
		case verdict == nil:
			unchecked = append(unchecked, criterionRef(c))
			uncheckedCriteria = append(uncheckedCriteria, domain.CriteriaGateCriterion{ID: c.ID, Text: c.Text})
		case !verdict.Approved:
			rejected = append(rejected, fmt.Sprintf("%s (%s)", criterionRef(c), verdict.Note))
			rejectedCriteria = append(rejectedCriteria, domain.CriteriaGateCriterion{ID: c.ID, Text: c.Text})
		}
	}
	if len(rejected) > 0 {
		msg := criteriaRejectedKey.Render(criteriaRejectedInput{Target: string(target), Count: len(rejected), Role: string(role), Rejected: rejected})
		return domain.NewCriteriaGateError(target, domain.CriteriaGateReasonRejected, rejectedCriteria, msg)
	}
	if len(unchecked) > 0 {
		msg := criteriaUncheckedKey.Render(criteriaUncheckedInput{Target: string(target), Count: len(unchecked), Role: string(role), Unchecked: unchecked})
		return domain.NewCriteriaGateError(target, domain.CriteriaGateReasonUnchecked, uncheckedCriteria, msg)
	}
	return nil
}

func criterionCheckFor(c domain.AcceptanceCriterion, role domain.CriterionReviewRole) *domain.CriterionCheck {
	for i := range c.Checks {
		if c.Checks[i].Role == role {
			return &c.Checks[i]
		}
	}
	return nil
}

func (s *Service) SetDeployTargets(store port.DeployTargetStore) {
	s.deployTargets = store
}

func (s *Service) SetMobileStoreApps(store port.MobileStoreAppStore) {
	s.mobileStoreApps = store
}

// StoreTestBuilder builds a task for its repository's store apps when the task
// enters a stage carrying store_test_build_on_enter.
type StoreTestBuilder interface {
	OnTaskEnteredTestStage(ctx context.Context, repositoryID, taskID uuid.UUID)
}

func (s *Service) SetStoreTestBuilder(b StoreTestBuilder) {
	s.storeTestBuilds = b
}

// DesignSystemApprover approves the design system versions a design task
// proposed when the task enters a stage carrying
// approve_design_system_on_enter.
type DesignSystemApprover interface {
	ApproveForTask(ctx context.Context, taskID uuid.UUID) []domain.DesignSystem
}

func (s *Service) SetDesignSystemApprover(a DesignSystemApprover) {
	s.designSystems = a
}

func (s *Service) SetGit(git port.GitClient, workspaceRoot string) {
	s.git = git
	s.workspaceRoot = workspaceRoot
}

func (s *Service) SetPipelineJobStore(store port.RepositoryPipelineJobStore) {
	s.pipelineJobs = store
}

func (s *Service) SetGitHubTokenSource(src func(ctx context.Context) (string, error)) {
	s.githubToken = src
}

func (s *Service) SetWorkflows(w port.WorkflowReader)  { s.workflows = w }
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

func (s *Service) workflow(ctx context.Context, taskType domain.TaskType) (domain.Workflow, error) {
	if s.workflows == nil {
		return domain.Workflow{}, fmt.Errorf("workflow reader unavailable")
	}
	return s.workflows.Workflow(ctx, taskType)
}

// CreateWorkflowSetupTask opens the board task that authors the repo's CI
// workflows — at most one at a time. While the last one is still on the board
// and not done/released it is returned as is (created == false), so repeated
// clicks on the "no CI workflows" notice cannot pile up duplicates.
func (s *Service) CreateWorkflowSetupTask(ctx context.Context, repositoryID uuid.UUID) (task domain.BoardTask, created bool, err error) {
	s.ciSetupMu.Lock()
	defer s.ciSetupMu.Unlock()

	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	if open, ok, err := s.openWorkflowSetupTask(ctx, repo); err != nil || ok {
		return open, false, err
	}
	var assignee *uuid.UUID
	if s.roles != nil {
		area := domain.RepoArea(repo.Kind, repo.SubProjects)
		if agent, aerr := s.roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, area); aerr == nil {
			assignee = agent
		}
	}
	desc := workflowSetupTaskBrief(repo.Kind)

	task, err = s.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:           "Set up GitHub Actions CI/CD workflows",
		Description:     desc,
		Priority:        domain.TaskPriorityHigh,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: assignee,
	})
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	if err := s.repos.SetCISetupTaskID(ctx, repositoryID, task.ID.String()); err != nil {
		return domain.BoardTask{}, false, err
	}
	return task, true, nil
}

// WorkflowSetupTask reports the CI setup task this repository is still waiting
// on, if any.
func (s *Service) WorkflowSetupTask(ctx context.Context, repositoryID uuid.UUID) (domain.BoardTask, bool, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	return s.openWorkflowSetupTask(ctx, repo)
}

// openWorkflowSetupTask: a deleted task, or one that reached done/released
// while the repo still has no CI, no longer holds the slot — the next click
// opens a fresh one.
func (s *Service) openWorkflowSetupTask(ctx context.Context, repo domain.Repository) (domain.BoardTask, bool, error) {
	id, err := uuid.Parse(strings.TrimSpace(repo.CISetupTaskID))
	if err != nil {
		return domain.BoardTask{}, false, nil
	}
	task, err := s.tasks.Get(ctx, repo.ID, id)
	if errors.Is(err, domain.ErrBoardTaskNotFound) || errors.Is(err, port.ErrNotFound) {
		return domain.BoardTask{}, false, nil
	}
	if err != nil {
		return domain.BoardTask{}, false, err
	}
	if task.Column == domain.TaskColumnDone || task.Column == domain.TaskColumnReleased {
		return domain.BoardTask{}, false, nil
	}
	return task, true, nil
}

func workflowSetupTaskBrief(kind string) string {
	return workflowSetupTaskBriefKey.Render(workflowSetupTaskBriefInput{Kind: kind})
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

		if kind := strings.TrimSpace(req.Kind); kind != "" && kind != existing.Kind {
			updated, err := s.repos.UpdateMeta(ctx, existing.ID, &kind, nil)
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
		if s.modelRefresher != nil {
			s.modelRefresher.RefreshIfNeverScanned(ctx, existing.ID, "import")
		}
		return s.withGitWarning(existing), nil
	}
	return s.registerNew(ctx, absRoot, name, req, true)
}

func (s *Service) registerNew(ctx context.Context, absRoot, name string, req domain.OpenRepositoryRequest, scan bool) (domain.Repository, error) {
	if err := s.ensureGitSync(ctx, absRoot, name, req.Owner); err != nil {
		return domain.Repository{}, fmt.Errorf("git/GitHub setup failed: %w", err)
	}
	remoteURL := strings.TrimSpace(req.CloneURL)
	if remoteURL == "" && s.git != nil {
		remoteURL = s.git.OriginURL(ctx, absRoot)
	}

	// Kind, sub-projects, mobile identity and build targets are no longer
	// detected here: the scan the ModelRefresher kicks off below projects them
	// from the component model once it finishes.
	kind := strings.TrimSpace(req.Kind)

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
	s.startIndex(ctx, repo.ID, absRoot)

	s.setupWebhookAsync(ctx, repo.ID)

	if scan && s.modelRefresher != nil {
		s.modelRefresher.RefreshAsync(ctx, repo.ID, "import")
	}
	return s.withGitWarning(repo), nil
}

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

		CloneURL: cloneURL,

		Kind: strings.TrimSpace(req.Kind),
	})
}

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

	repo.GitRestore = s.restoreState(repo.ID)
	s.gitWarnMu.Lock()
	warning := s.gitWarnings[repo.ID]
	s.gitWarnMu.Unlock()

	if s.git != nil {
		presence := s.git.Presence(repo.RootPath)
		if w := presence.Warning(); w != "" {
			repo.GitWarning = w
		}

		restorable, _ := domain.CanRestoreWorkingCopy(presence, repo.RemoteURL)
		repo.GitRestorable = restorable
	}

	if warning != "" {
		repo.GitWarning = warning
	}
	return repo
}

func (s *Service) Create(ctx context.Context, req domain.CreateRepositoryRequest) (domain.Repository, error) {
	return s.create(ctx, req, true)
}

// CreateWithoutScan is Create for a repository that starts empty: a scan of an
// empty folder can only find a placeholder root component, so the first scan
// waits for the first push to the default branch (webhook or poll).
func (s *Service) CreateWithoutScan(ctx context.Context, req domain.CreateRepositoryRequest) (domain.Repository, error) {
	return s.create(ctx, req, false)
}

func (s *Service) create(ctx context.Context, req domain.CreateRepositoryRequest, scan bool) (domain.Repository, error) {
	if err := validateRequestKind(req.Kind); err != nil {
		return domain.Repository{}, err
	}
	name, err := domain.NewRepoDirName(req.Name)
	if err != nil {
		return domain.Repository{}, err
	}
	parent := strings.TrimSpace(req.ParentDir)
	if parent == "" {

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
	if _, err := os.Lstat(rootPath); err == nil {
		return domain.Repository{}, fmt.Errorf("directory already exists: %s — choose another name, or import the existing folder instead", rootPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return domain.Repository{}, fmt.Errorf("check directory: %w", err)
	}
	if existing, err := s.repos.GetByRootPath(ctx, rootPath); err == nil && existing.ID != uuid.Nil {
		return domain.Repository{}, fmt.Errorf("repository %q is already registered at %s (its folder is missing) — restore or remove it first", existing.Name, rootPath)
	}
	if err := os.Mkdir(rootPath, 0o755); err != nil {
		return domain.Repository{}, fmt.Errorf("create directory: %w", err)
	}
	repo, err := s.registerNew(ctx, rootPath, name, domain.OpenRepositoryRequest{
		RootPath:    rootPath,
		Description: req.Description,
		ProjectIDs:  req.ProjectIDs,
		Owner:       req.Owner,
		Kind:        strings.TrimSpace(req.Kind),
	}, scan)
	if err != nil {
		return domain.Repository{}, s.abandonNewDirectory(ctx, rootPath, err)
	}
	return repo, nil
}

// abandonNewDirectory undoes a create that failed after its folder was made,
// so retrying the same name works. The GitHub repository cannot be taken back
// from here; the error says so, since a retry would collide with it.
func (s *Service) abandonNewDirectory(ctx context.Context, rootPath string, cause error) error {
	cleanupCtx := context.WithoutCancel(ctx)
	remote := ""
	var created *domain.RemoteRepoCreatedError
	if errors.As(cause, &created) {
		remote = created.URL
	} else if s.git != nil {
		remote = s.git.OriginURL(cleanupCtx, rootPath)
	}
	if orphan, err := s.repos.GetByRootPath(cleanupCtx, rootPath); err == nil && orphan.ID != uuid.Nil {
		if err := s.repos.Delete(cleanupCtx, orphan.ID); err != nil {
			log.Warn().Err(err).Str("repository_id", orphan.ID.String()).Msg("create repository: removing the half-registered row failed")
		}
	}
	var notes strings.Builder
	if err := os.RemoveAll(rootPath); err != nil {
		log.Warn().Err(err).Str("root", rootPath).Msg("create repository: removing the new directory failed")
		fmt.Fprintf(&notes, "; the folder %s could not be removed (%v), delete it before retrying", rootPath, err)
	}
	if remote != "" {
		fmt.Fprintf(&notes, "; the GitHub repository %s was already created — delete it on GitHub, or import it, before creating this name again", remote)
	}
	return fmt.Errorf("%w%s", cause, notes.String())
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req domain.UpdateRepositoryRequest) (domain.Repository, error) {
	repo, err := s.repos.Update(ctx, id, req.Name, req.Description, req.VerifyCommand, req.BuildCommand, req.TestCommand, req.RequireHumanReview)
	if err != nil {
		return domain.Repository{}, err
	}

	if req.ReleaseEngine != nil {
		engine := strings.TrimSpace(*req.ReleaseEngine)
		if err := domain.ValidateReleaseEngine(engine, repo.Kind); err != nil {
			return domain.Repository{}, err
		}

		if engine == "" {
			engine = domain.ReleaseEngineAuto
		}
		updated, err := s.repos.UpdateReleaseEngine(ctx, id, engine)
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

	if s.indexer != nil && s.indexer.IsProjectIndexActive(id) {
		log.Warn().Str("repository_id", id.String()).Msg("deleting repository with an active index job; job will run to completion and its rows will cascade-delete")
	}
	return s.repos.Delete(ctx, id)
}

const (
	freshnessCheckInterval = 10 * time.Minute
	// freshnessMaxBackoff caps how far failed pulls push a repository's next
	// check out. A pull that fails is most often a machine with no network, and
	// retrying it every interval only wakes git and the network for nothing.
	freshnessMaxBackoff = time.Hour
)

func freshnessWait(failures int) time.Duration {
	wait := freshnessCheckInterval
	for range failures {
		if wait >= freshnessMaxBackoff {
			break
		}
		wait *= 2
	}
	return min(wait, freshnessMaxBackoff)
}

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
			s.refreshIndex(ctx, repo.ID, repo.RootPath)
		}
	}
	if getErr == nil {
		s.ensureIndexFresh(ctx, repo, idx)
	}
	idx.SyncWarning = s.syncWarning(repositoryID)

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
	s.pullAndReindex(ctx, repo.ID, repo.RootPath, nil, true)
	return nil
}

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

// pullAndReindex pulls the project root and replaces any running pass. Only
// an explicit user Reindex forces every file to be re-embedded; a push or a
// poll runs the incremental pass.
func (s *Service) pullAndReindex(ctx context.Context, repositoryID uuid.UUID, rootPath string, onDone func(), force bool) {
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
		if force {
			s.indexer.RestartIndexProject(passCtx, repositoryID, rootPath, finish)
			return
		}
		s.indexer.RefreshIndexProject(passCtx, repositoryID, rootPath, finish)
	}()
}

func (s *Service) pullProjectRoot(ctx context.Context, repositoryID uuid.UUID, rootPath string) {
	if s.git == nil || !s.git.HasGit(rootPath) {

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

func (s *Service) recordSyncResult(repositoryID uuid.UUID, err error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncWarnings == nil {
		s.syncWarnings = make(map[uuid.UUID]string)
	}
	if s.syncFailures == nil {
		s.syncFailures = make(map[uuid.UUID]int)
	}
	if err == nil {
		delete(s.syncWarnings, repositoryID)
		delete(s.syncFailures, repositoryID)
		return
	}
	s.syncWarnings[repositoryID] = err.Error()
	s.syncFailures[repositoryID]++
}

func (s *Service) syncWarning(repositoryID uuid.UUID) string {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	return s.syncWarnings[repositoryID]
}

func (s *Service) claimFreshnessCheck(repositoryID uuid.UUID) bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if !s.freshnessDueLocked(repositoryID) {
		return false
	}
	if s.syncCheckedAt == nil {
		s.syncCheckedAt = make(map[uuid.UUID]time.Time)
	}
	s.syncCheckedAt[repositoryID] = time.Now()
	return true
}

func (s *Service) freshnessCheckDue(repositoryID uuid.UUID) bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	return s.freshnessDueLocked(repositoryID)
}

func (s *Service) freshnessDueLocked(repositoryID uuid.UUID) bool {
	last, ok := s.syncCheckedAt[repositoryID]
	return !ok || time.Since(last) >= freshnessWait(s.syncFailures[repositoryID])
}

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

		s.refreshIndex(freshCtx, repo.ID, repo.RootPath)

		if s.modelRefresher != nil {
			s.modelRefresher.RefreshAfterPush(freshCtx, repo.ID, "poll")
		}
	}()
}

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
			if !s.freshnessCheckDue(repo.ID) {
				continue
			}
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

func (s *Service) refreshIndex(ctx context.Context, repositoryID uuid.UUID, rootPath string) {
	s.startIndexWith(ctx, repositoryID, rootPath, true)
}

func (s *Service) startIndexWith(ctx context.Context, repositoryID uuid.UUID, rootPath string, replace bool) {
	if s.indexer == nil {
		return
	}
	if replace {
		s.indexMu.Lock()
		delete(s.indexing, repositoryID)
		s.indexing[repositoryID] = struct{}{}
		s.indexMu.Unlock()
		s.indexer.RefreshIndexProject(ctx, repositoryID, rootPath, s.indexDone(repositoryID))
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

// BoardVersion is the task store's change counter (port.BoardVersioner);
// false when the store keeps none. Read it before ListBoardTasks, never after,
// so a list is only ever tagged with a version at least as old as its rows.
func (s *Service) BoardVersion() (uint64, bool) {
	v, ok := s.tasks.(port.BoardVersioner)
	if !ok {
		return 0, false
	}
	return v.BoardVersion(), true
}

// BoardListExpiry is when a ListBoardTasks answer goes stale with no write at
// all: the first released task on it ages out of domain.ReleasedBoardWindow.
// It mirrors the store's COALESCE(entered_at, updated_at). ok is false when
// nothing on the list is time-limited.
func BoardListExpiry(tasks []domain.BoardTask) (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, t := range tasks {
		if t.Column != domain.TaskColumnReleased {
			continue
		}
		since := t.UpdatedAt
		if t.ColumnEnteredAt != nil {
			since = *t.ColumnEnteredAt
		}
		if until := since.Add(domain.ReleasedBoardWindow); !found || until.Before(earliest) {
			earliest, found = until, true
		}
	}
	return earliest, found
}

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
	task, err := s.tasks.GetByID(ctx, taskID)
	if errors.Is(err, domain.ErrBoardTaskNotFound) {
		return uuid.Nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return uuid.Nil, err
	}
	return task.RepositoryID, nil
}

func (s *Service) DefaultRepositoryID(ctx context.Context) (uuid.UUID, error) {
	repos, err := s.repos.List(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if len(repos) == 0 {
		return uuid.Nil, errors.New(prompt.Text(noRepositoriesKey))
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

func (s *Service) resolveTaskType(ctx context.Context, requested domain.TaskType) (domain.TaskType, error) {
	if requested == "" {
		if s.workflows == nil {
			return "", fmt.Errorf("workflow reader unavailable: cannot resolve the default task type")
		}
		def, err := s.workflows.DefaultTaskType(ctx)
		if err != nil {
			return "", fmt.Errorf("default task type unavailable: %w", err)
		}
		return def, nil
	}
	if s.workflows == nil {
		return requested, nil
	}
	exists, err := s.workflows.TaskTypeExists(ctx, requested)
	if err != nil {
		return "", fmt.Errorf("task type check unavailable: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("invalid task type: %s", requested)
	}
	return requested, nil
}

func (s *Service) resolveNewTaskAssignee(ctx context.Context, taskType domain.TaskType, repo domain.Repository, requested *uuid.UUID) (*uuid.UUID, error) {
	if s.roles == nil {
		return requested, nil
	}
	area := domain.RepoArea(repo.Kind, repo.SubProjects)
	resolved, err := s.roles.AssigneeForNewTask(ctx, taskType, area, requested)
	if err != nil {
		return nil, fmt.Errorf("resolve assignee for new task: %w", err)
	}
	if resolved != nil && (requested == nil || *resolved != *requested) {
		pm, err := s.isProductManager(ctx, *resolved)
		if err != nil {
			return nil, err
		}
		if pm {
			return requested, nil
		}
	}
	return resolved, nil
}

func (s *Service) CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	taskType, err := s.resolveTaskType(ctx, req.TaskType)
	if err != nil {
		return domain.BoardTask{}, err
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
	if err := s.validateStageConfigured(ctx, taskType, col); err != nil {
		return domain.BoardTask{}, err
	}
	if err := s.validateMoveAllowed(ctx, uuid.Nil, taskType, col); err != nil {
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
	requested, err := s.requestedAssignee(ctx, req)
	if err != nil {
		return domain.BoardTask{}, err
	}
	assignee, err := s.resolveNewTaskAssignee(ctx, taskType, repo, requested)
	if err != nil {
		return domain.BoardTask{}, err
	}
	if req.ComponentID != nil {
		if err := s.validateTaskComponent(ctx, repositoryID, *req.ComponentID); err != nil {
			return domain.BoardTask{}, err
		}
	}
	task, err := s.tasks.Create(ctx, domain.BoardTask{
		RepositoryID:         repositoryID,
		TaskNumber:           taskNumber,
		Title:                strings.TrimSpace(req.Title),
		ComponentID:          req.ComponentID,
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

		if !domain.ValidTaskRelationType(rel.RelationType) {
			return nil, fmt.Errorf("invalid relation type: %s", rel.RelationType)
		}
		if rel.TargetTaskID != uuid.Nil {
			out = append(out, rel)
			continue
		}
		if rel.TargetKey == "" {
			return nil, errors.New(prompt.Text(relationTargetRequiredKey))
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
		if s.workflows == nil {
			return domain.BoardTask{}, fmt.Errorf("workflow reader unavailable: cannot validate task type %s", *req.TaskType)
		}
		exists, err := s.workflows.TaskTypeExists(ctx, *req.TaskType)
		if err != nil {
			return domain.BoardTask{}, fmt.Errorf("task type check unavailable: %w", err)
		}
		if !exists {
			return domain.BoardTask{}, fmt.Errorf("invalid task type: %s", *req.TaskType)
		}
		task.TaskType = *req.TaskType
		if req.Column == nil {
			if err := s.validateStageConfigured(ctx, task.TaskType, prevColumn); err != nil {
				return domain.BoardTask{}, err
			}
		}
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
		// Any write that changes the text re-arms the gate: a human confirmed the
		// OLD steps, not whatever replaced them.
		if task.BeforeDeploy == nil || *task.BeforeDeploy != *req.BeforeDeploy {
			task.BeforeDeployConfirmedAt = nil
		}
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
		if err := s.validateStageConfigured(ctx, task.TaskType, *req.Column); err != nil {
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
		if err := s.validateMoveAllowed(ctx, task.ID, task.TaskType, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		if err := s.criteriaGate(ctx, task.ID, task.TaskType, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		if err := s.criteriaReviewGate(ctx, task.ID, task.TaskType, prevColumn, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}

		if err := s.testCaseGate(ctx, task.ID, task.TaskType, prevColumn, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}

		if err := s.reviewStageSkipGate(ctx, task, prevColumn, *req.Column, req.Actor); err != nil {
			return domain.BoardTask{}, err
		}
		if err := s.reviewChainGate(ctx, repo, task, prevColumn, *req.Column); err != nil {
			return domain.BoardTask{}, err
		}
		task.Column = *req.Column
	}
	if req.Position != nil {
		task.Position = *req.Position
	}
	if req.AssigneeAgentID.Present {
		if next := req.AssigneeAgentID.Value; next != nil && (prevAssignee == nil || *next != *prevAssignee) {
			if err := s.requireAssignable(ctx, next); err != nil {
				return domain.BoardTask{}, err
			}
		}
		task.AssigneeAgentID = req.AssigneeAgentID.Value
	}
	if req.ComponentID.Present {
		if req.ComponentID.Value != nil {
			if err := s.validateTaskComponent(ctx, repositoryID, *req.ComponentID.Value); err != nil {
				return domain.BoardTask{}, err
			}
		}
		task.ComponentID = req.ComponentID.Value
	}

	if req.Column != nil && *req.Column != prevColumn && s.reviewQuorum != nil {
		decision, err := s.reviewQuorum.Decide(ctx, task, prevColumn, *req.Column, req.Actor, req.ActorAgentID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		if decision.Hold {
			task.Column = prevColumn
			return task, nil
		}
		if decision.Target != *req.Column {
			if s.columns != nil {
				if err := s.columns.ValidateTransition(ctx, string(prevColumn), string(decision.Target)); err != nil {
					return domain.BoardTask{}, err
				}
			}
			target := decision.Target
			req.Column = &target
			task.Column = target
		}
	}

	if req.Column != nil && *req.Column != prevColumn && s.reviewGate != nil {
		if !s.reviewGate.InterceptAgentMove(ctx, task, prevColumn, *req.Column, req.Actor, repo) {
			task.Column = prevColumn
			return task, nil
		}
	}

	if req.Column != nil && *req.Column != prevColumn && prevColumn == domain.TaskColumnCodeReview &&
		*req.Column != domain.TaskColumnNeedRevision {
		s.recordCodeReviewApprovalPatchID(ctx, task.ID)
	}

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

		if req.Actor == domain.TaskActorAgent && req.ActorAgentID != nil {
			movePayload["actor_agent_id"] = req.ActorAgentID.String()
		}

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

		if s.workOrderSweeper != nil &&
			(*req.Column == domain.TaskColumnDone || *req.Column == domain.TaskColumnReleased) {
			s.workOrderSweeper.WakeDependentsOf(ctx, updated.ID)
		}
		if s.scorer != nil {
			s.scorer.OnColumnTransition(ctx, updated, prevColumn, *req.Column)
		}
		if s.completion != nil {
			s.completion.OnColumnTransition(ctx, updated, prevColumn, *req.Column)
		}
		if s.evolution != nil && *req.Column == domain.TaskColumnNeedRevision {
			s.evolution.NotifyRevision(ctx, updated)
		}
		// Approval: analiz_review -> done. The human's answers to any
		// non-blocking question that rode along into analiz_review are told
		// to the agent now and left on the task, same as a submit.
		if prevColumn == domain.TaskColumnAnalizReview && *req.Column == domain.TaskColumnDone {
			s.markQuestionsSubmitted(ctx, updated.RepositoryID, updated.ID)
		}

		wf, wfErr := s.workflow(ctx, updated.TaskType)
		if wfErr != nil {
			log.Warn().Err(wfErr).Str("task_id", updated.ID.String()).
				Msg("update task: workflow unavailable, skipping enter-stage side effects")
		}

		if wfErr == nil && wf.Has(*req.Column, domain.BehaviourEnsurePROnEnter) && updated.TaskType.PublishesBranch() {
			s.ensurePullRequestAsync(ctx, updated)
		}

		if s.pipelines != nil && wfErr == nil && wf.Has(*req.Column, domain.BehaviourWaitForCI) {
			if _, perr := s.pipelines.Trigger(ctx, repositoryID, updated, domain.PipelineTriggerReadyForQA); perr != nil {
				log.Warn().Err(perr).Str("task_id", updated.ID.String()).Msg("pipeline trigger failed")
			}
		}

		if wfErr == nil && wf.Has(*req.Column, domain.BehaviourDetectMigrationOnEnter) {
			s.DetectTaskMigration(ctx, updated)
		}

		if s.designSystems != nil && wfErr == nil && wf.Has(*req.Column, domain.BehaviourApproveDesignSystem) {
			s.designSystems.ApproveForTask(context.WithoutCancel(ctx), updated.ID)
		}

		// Off the request path: allocating a build number reads the store.
		if s.storeTestBuilds != nil && wfErr == nil && wf.Has(*req.Column, domain.BehaviourStoreTestBuildOnEnter) {
			go s.storeTestBuilds.OnTaskEnteredTestStage(context.WithoutCancel(ctx), repositoryID, updated.ID)
		}

		if s.pipelines != nil {
			strategy := s.testStrategy(ctx, repositoryID)
			deployNow := false
			if wfErr == nil && wf.Has(*req.Column, domain.BehaviourStageDeployOnEnter) {
				when, _ := wf.Param(*req.Column, domain.BehaviourStageDeployOnEnter, "when")
				switch when {
				case "qa":
					deployNow = domain.DeploysForQA(strategy)
				case "per_step":
					deployNow = domain.DeploysOnCodeReview(strategy)
				}
			}
			if deployNow {

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

		assignPayload := map[string]interface{}{}
		if prevAssignee != nil {
			assignPayload["from_agent_id"] = prevAssignee.String()
		}
		if updated.AssigneeAgentID != nil {
			assignPayload["to_agent_id"] = updated.AssigneeAgentID.String()
		}

		if req.Actor == domain.TaskActorAgent && req.ActorAgentID != nil {
			assignPayload["actor_agent_id"] = req.ActorAgentID.String()
		}
		_ = s.emit(ctx, repo, updated, domain.BoardEventTaskAssigned, assignPayload)
	}

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

// ConfirmBeforeDeploy records a human's confirmation that BeforeDeploy's
// steps were performed — the release engineer may not write to production
// itself, so nothing ships a task whose steps are written but unconfirmed
// (domain.BoardTask.BeforeDeployPending). Idempotent at the store: a repeated
// confirm keeps the first timestamp rather than restarting the clock the UI
// shows.
func (s *Service) ConfirmBeforeDeploy(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	task, err := s.tasks.ConfirmBeforeDeploy(ctx, repositoryID, taskID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	return s.enrichTask(ctx, task)
}

// BeforeDeployConfirmer adapts ConfirmBeforeDeploy to the error-only
// signature application/release's own Tasks-side dependency expects
// (ConfirmBeforeDeploy(ctx, repositoryID, taskID) error) — Cut stamps every
// task's confirmation through it. *Service cannot satisfy that interface
// directly: it already has a ConfirmBeforeDeploy that returns the updated
// domain.BoardTask for the HTTP handler, and Go has no overloading, so the
// integrator wires BeforeDeployConfirmer{Service: repoSvc} wherever the
// error-only shape is asked for instead.
type BeforeDeployConfirmer struct{ Service *Service }

func (a BeforeDeployConfirmer) ConfirmBeforeDeploy(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	_, err := a.Service.ConfirmBeforeDeploy(ctx, repositoryID, taskID)
	return err
}

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

		if err := s.guardDeployOrderCycle(ctx, taskID, rel.TargetTaskID); err != nil {
			return nil, err
		}
	}
	return s.relations.ReplaceForTaskOfType(ctx, taskID, domain.TaskRelationDeployDependsOn, resolved)
}

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

func validateAgentSelfMove(task domain.BoardTask, req domain.UpdateBoardTaskRequest, prevColumn domain.TaskColumn) error {
	if req.Actor != domain.TaskActorAgent || req.Column == nil || *req.Column == prevColumn {
		return nil
	}
	if req.ActorAgentID == nil || task.AssigneeAgentID == nil || *req.ActorAgentID != *task.AssigneeAgentID {
		return nil
	}
	switch *req.Column {
	case domain.TaskColumnNeedRevision:
		return domain.RefuseMove(fmt.Errorf("%s", prompt.Text(selfMoveNeedRevisionKey)))
	case domain.TaskColumnTodo:
		return domain.RefuseMove(fmt.Errorf("%s", prompt.Text(selfMoveTodoKey)))
	}
	return nil
}

func (s *Service) validateMoveAllowed(ctx context.Context, taskID uuid.UUID, taskType domain.TaskType, target domain.TaskColumn) error {
	if taskID == uuid.Nil || s.relations == nil {
		return nil
	}

	wf, err := s.workflow(ctx, taskType)
	if err != nil {
		return fmt.Errorf("work-order gate: workflow unavailable for %s (%w)", target, err)
	}
	if refuse, _ := wf.Param(target, domain.BehaviourBlockOnDependencies, "refuse_move"); refuse != "true" {
		return nil
	}
	blockers, err := s.relations.ListBlockingSources(ctx, taskID)
	if err != nil {
		return err
	}
	if len(blockers) > 0 {

		labels := make([]string, 0, len(blockers))
		gateBlockers := make([]domain.WorkOrderGateBlocker, 0, len(blockers))
		for _, b := range blockers {
			labels = append(labels, domain.RelationLabel(b.Key, b.Title, b.ID)+" ["+string(b.Column)+"]")
			gateBlockers = append(gateBlockers, domain.WorkOrderGateBlocker{Key: b.Key, Title: b.Title})
		}
		return domain.NewWorkOrderGateError(target, gateBlockers,
			workOrderBlockedKey.Render(workOrderBlockedInput{Labels: labels}))
	}
	return nil
}

func (s *Service) ClaimTask(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) (domain.BoardTask, error) {
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return domain.BoardTask{}, err
	}
	if err := s.requireAssignable(ctx, &agentID); err != nil {
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
	// A deleted blocker takes its blocks relations with it, which frees its
	// dependents without the done/released move WakeDependentsOf listens for.
	s.workOrderSweeper.SweepSoon()

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
	return comments, nil
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
	payload := map[string]interface{}{
		"comment_id":  comment.ID.String(),
		"content":     comment.Content,
		"author_type": comment.AuthorType,
		"author_id":   comment.AuthorID,
		"author_name": comment.AuthorName,
	}
	if req.Informational {
		payload[domain.EventPayloadInformational] = true
	}
	_ = s.emit(ctx, repo, task, domain.BoardEventTaskCommented, payload)
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
	format, err := domain.NormalizeDocumentFormat(req.Format)
	if err != nil {
		return domain.TaskDocument{}, err
	}
	content, err := prepareDocumentContent(format, req.Content)
	if err != nil {
		return domain.TaskDocument{}, err
	}
	authorType := req.CreatedByType
	if authorType == "" {
		authorType = "user"
	}
	return s.documents.Create(ctx, domain.TaskDocument{
		TaskID:        taskID,
		Title:         strings.TrimSpace(req.Title),
		Content:       content,
		Format:        format,
		Position:      req.Position,
		CreatedByType: authorType,
		CreatedByID:   req.CreatedByID,
	})
}

func (s *Service) UpdateDocument(ctx context.Context, repositoryID, taskID, docID uuid.UUID, req domain.UpdateTaskDocumentRequest) (domain.TaskDocument, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return domain.TaskDocument{}, err
	}
	if s.documents == nil {
		return domain.TaskDocument{}, fmt.Errorf("documents not enabled")
	}
	doc, err := s.documents.Get(ctx, taskID, docID)
	if err != nil {
		return domain.TaskDocument{}, err
	}
	if req.Title != nil {
		doc.Title = *req.Title
	}
	if req.Format != nil {
		format, err := domain.NormalizeDocumentFormat(*req.Format)
		if err != nil {
			return domain.TaskDocument{}, err
		}
		doc.Format = format
	}
	if doc.Format == "" {
		doc.Format = domain.DocumentFormatMarkdown
	}
	if req.Content != nil {
		doc.Content = *req.Content
	}
	if req.Content != nil || req.Format != nil {
		content, err := prepareDocumentContent(doc.Format, doc.Content)
		if err != nil {
			return domain.TaskDocument{}, err
		}
		doc.Content = content
	}
	if req.Position != nil {
		doc.Position = *req.Position
	}
	return s.documents.Update(ctx, doc)
}

// prepareDocumentContent is the one door html content passes through on its
// way to the store, from the HTTP API and the agent tools alike: size-checked
// first (the limit is on what was sent, not on what survives), then sanitized.
func prepareDocumentContent(format domain.DocumentFormat, content string) (string, error) {
	if format != domain.DocumentFormatHTML {
		return content, nil
	}
	if len(content) > domain.MaxHTMLDocumentBytes {
		return "", fmt.Errorf("%w (got %d bytes) — %s", domain.ErrDocumentTooLarge, len(content), prompt.Text(documentTooLargeHintKey))
	}
	clean, err := htmldoc.Sanitize(content)
	if err != nil {
		return "", fmt.Errorf("sanitize html document: %w", err)
	}
	return clean, nil
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

func (s *Service) ListTaskEvents(ctx context.Context, repositoryID, taskID uuid.UUID, events port.BoardEventStore, limit int) ([]domain.BoardEvent, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if events == nil {
		return []domain.BoardEvent{}, nil
	}
	return events.ListByTask(ctx, taskID, limit)
}

func (s *Service) ListTaskPipelines(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskPipeline, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.pipelineStore == nil {
		return []domain.TaskPipeline{}, nil
	}
	return s.pipelineStore.ListByTask(ctx, taskID)
}

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

	if pipeline.RepositoryID != repositoryID {
		return domain.TaskPipeline{}, domain.ErrPipelineNotFound
	}
	return pipeline, nil
}

func (s *Service) postPreDeployChecklist(ctx context.Context, task domain.BoardTask) {
	if s.comments == nil {
		return
	}
	var sections []string
	if before := trimmedPtr(task.BeforeDeploy); before != "" {

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

func trimmedPtr(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

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

// ListDirectories backs the "add component" folder picker: it browses real
// directories under the repository's working copy. It no longer classifies
// what it finds — a component's role is a projection of the scan, not a
// guess made while browsing.
func (s *Service) ListDirectories(ctx context.Context, repositoryID uuid.UUID, rel string) (string, []string, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return "", nil, err
	}
	abs, err := workspace.ResolveWithinRoot(repo.RootPath, rel)
	if err != nil {
		return "", nil, err
	}
	absRoot, err := filepath.Abs(repo.RootPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve repository root: %w", err)
	}
	relClean, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return "", nil, fmt.Errorf("resolve %q: %w", rel, err)
	}
	if relClean == "." {
		relClean = ""
	} else {
		relClean = filepath.ToSlash(relClean)
	}
	entries, err := ListChildDirectories(abs)
	if err != nil {
		return "", nil, err
	}
	return relClean, entries, nil
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

func (s *Service) validateColumn(ctx context.Context, col domain.TaskColumn) error {
	if s.columns == nil {
		if !domain.ValidTaskColumn(col) {
			return fmt.Errorf("invalid column: %s", col)
		}
		return nil
	}
	return s.columns.ValidateColumn(ctx, string(col))
}

// validateStageConfigured refuses a column no more than validateColumn does
// for a column the board doesn't have — scoped to the task's own type. It
// runs on every move and creation, including a repository/deployment that
// never wired workflows at all, so unlike the sibling gates that only run
// once relations/criteria are already known to be configured, an
// unavailable workflow reader here is treated the same as a type with zero
// stages: unscoped/legacy-open, not a lock-out.
func (s *Service) validateStageConfigured(ctx context.Context, taskType domain.TaskType, target domain.TaskColumn) error {
	wf, err := s.workflow(ctx, taskType)
	if err != nil || len(wf.Stages) == 0 {
		return nil
	}
	if _, ok := wf.Stage(target); ok {
		return nil
	}
	return domain.NewStageNotOnWorkflowError(taskType, target,
		stageNotConfiguredKey.Render(stageNotConfiguredInput{TaskType: string(taskType), Column: string(target)}))
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

func (s *Service) SetIncidentPolicy(ctx context.Context, repositoryID uuid.UUID, policy domain.IncidentPolicy) (domain.Repository, error) {
	if !domain.ValidIncidentPolicy(policy) {
		return domain.Repository{}, fmt.Errorf("invalid incident policy %q", policy)
	}
	return s.repos.UpdateIncidentPolicy(ctx, repositoryID, policy)
}

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
