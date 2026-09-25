package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agentfs"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/toolchain"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type RunJob struct {
	Run               domain.TaskAgentRun
	Event             domain.BoardEvent
	Task              domain.BoardTask
	RepositoryID      uuid.UUID
	EnteredFrom       domain.TaskColumn
	ColumnInstruction string
}

func (j RunJob) isRevision() bool {
	return j.Task.Column == domain.TaskColumnNeedRevision || j.EnteredFrom == domain.TaskColumnNeedRevision
}

type RepositoryResolver interface {
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
	ResolveDescription(ctx context.Context, repositoryID uuid.UUID) (string, error)
	ResolveRepository(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error)
}

// ProjectModel is the structured project model's read surface the board
// needs for verification: which commands CI runs and which components a
// diff or an agent's area maps to. Agents no longer get this pushed as a
// brief; ToolsNote tells them the model exists and they pull facts through
// the project-model tools instead. A nil ProjectModel is the pre-model
// behaviour — VerifyCommand/detectBuild verification only.
type ProjectModel interface {
	RequiredCommands(ctx context.Context, repositoryID uuid.UUID, componentIDs []uuid.UUID) ([]domain.LocalCommand, error)
	ComponentsForPaths(ctx context.Context, repositoryID uuid.UUID, paths []string) ([]uuid.UUID, error)
	ComponentsForArea(ctx context.Context, repositoryID uuid.UUID, area string) ([]uuid.UUID, error)
}

type TaskUpdater interface {
	UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error)
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
	ListComments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskComment, error)
}

type IndexInjector interface {
	InjectContext(ctx context.Context, sessionID uuid.UUID, messages []domain.Message, opts domain.InjectOptions) ([]domain.Message, error)
}

type BranchIndexer interface {
	StartIndexBranch(ctx context.Context, projectID uuid.UUID, branch, workspacePath string)
}

type Notifier interface {
	Notify(title, message string)
}

type AgentCLIConnections interface {
	Connected(ctx context.Context, flavor domain.AgentCLIFlavor) (*domain.AgentCLIConnection, error)
}

type BillingGate interface {
	Allow(ctx context.Context) (bool, string)
	PauseTask(ctx context.Context, repositoryID, taskID uuid.UUID)
}

type TaskBlocker interface {
	BlockOnQuestion(ctx context.Context, repositoryID, taskID, sessionID uuid.UUID, question string) error
	BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error)
}

func (r *Runner) SetAgentCLIConnections(c AgentCLIConnections) { r.agentCLIs = c }

func hostExecutorHint(p domain.LLMProviderType) (binary, envVar string) {
	switch p {
	case domain.LLMProviderClaudeCode:
		return "claude", "CLAUDE_CODE_BIN"
	case domain.LLMProviderCursorAgent:
		return "cursor-agent", "CURSOR_AGENT_BIN"
	case domain.LLMProviderAntigravity:
		return "agy", "ANTIGRAVITY_BIN"
	case domain.LLMProviderOpencode:
		return "opencode", "OPENCODE_BIN"
	default:
		return string(p), string(p) + "_BIN"
	}
}

func (r *Runner) requireConnectedCLI(ctx context.Context, agentRec domain.Agent) error {
	if r.agentCLIs == nil {
		return nil
	}
	flavor, isCLI := domain.AgentCLIFlavorFor(agentRec.ProviderType)
	if !isCLI {
		return nil
	}
	conn, err := r.agentCLIs.Connected(ctx, flavor)
	if err != nil {
		return fmt.Errorf("the connected local agent CLI could not be read, so agent %q was not dispatched to one: %w", agentRec.Name, err)
	}
	if conn != nil {
		return nil
	}
	return domain.ErrCLIFlavorNotConnected(agentRec.Name, agentRec.ProviderType)
}

func (r *Runner) SetTaskPRRecorder(rec TaskPRRecorder) { r.prRecorder = rec }

type PullRequestReader interface {
	PullRequest(ctx context.Context, repositoryID, taskID uuid.UUID, includeDiff bool) (domain.TaskPullRequest, error)
}

func (r *Runner) SetPullRequestReader(reader PullRequestReader) { r.prReader = reader }

func (r *Runner) SetWorkflows(w port.WorkflowReader)   { r.workflows = w }
func (r *Runner) SetRoleResolver(rr port.RoleResolver) { r.roles = rr }

func (r *Runner) workflowFor(ctx context.Context, taskType domain.TaskType) domain.Workflow {
	if r.workflows == nil {
		return domain.Workflow{}
	}
	wf, err := r.workflows.Workflow(ctx, taskType)
	if err != nil {
		log.Warn().Err(err).Str("task_type", string(taskType)).
			Msg("board run: workflow snapshot unreadable, run continues with every stage/type behaviour off")
		return domain.Workflow{}
	}
	return wf
}

type Runner struct {
	agentLoop         agent.Runner
	executor          port.TaskExecutor
	orchSvc           *orchestrator.Service
	catalog           port.CatalogStore
	activityStore     port.ActivityStore
	runs              port.TaskAgentRunStore
	sessions          port.SessionStore
	projects          RepositoryResolver
	indexInjector     IndexInjector
	branchIndexer     BranchIndexer
	perfStore         port.AgentPerformanceStore
	memories          port.AgentMemoryStore
	kpis              port.AgentKPIStore
	notifier          Notifier
	git               port.GitClient
	llm               port.LLMClient
	workspaceRoot     string
	budget            appcontext.Budget
	indexerCfg        domain.IndexerConfig
	mappingCfg        domain.MappingConfig
	defaultPolicy     domain.ToolPolicy
	defaultLang       string
	settings          port.SettingsStore
	heartbeatEvery    time.Duration
	taskUpdater       TaskUpdater
	verifyEnabled     bool
	verifyFixAttempts int
	taskTypeModels    map[string]string
	pipelines         port.TaskPipelineStore
	billing           BillingGate
	blocker           TaskBlocker
	toolchains        port.ToolchainDetector
	parks             *ParkJournal
	prRecorder        TaskPRRecorder
	prReader          PullRequestReader
	agentCLIs         AgentCLIConnections
	workflows         port.WorkflowReader
	roles             port.RoleResolver
	projectModel      ProjectModel
	queue             chan RunJob
	wg                sync.WaitGroup
	cancel            context.CancelFunc
	drain             chan struct{}
	drainOnce         sync.Once
	activeMu          sync.RWMutex
	active            map[uuid.UUID]struct{}
	queued            map[uuid.UUID]struct{}
	activeTasks       map[uuid.UUID]uuid.UUID
	parked            map[uuid.UUID][]RunJob
	agentSlots        *slotGate
	taskSlots         *slotGate
	taskSlotHeld      map[uuid.UUID]bool
	cancels           map[uuid.UUID]context.CancelFunc
}

const runHeartbeat = 10 * time.Second

const runLiveWithin = 6 * runHeartbeat

const persistTimeout = 15 * time.Second

func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

type RunnerDeps struct {
	AgentLoop           agent.Runner
	OrchSvc             *orchestrator.Service
	Catalog             port.CatalogStore
	ActivityStore       port.ActivityStore
	Runs                port.TaskAgentRunStore
	Sessions            port.SessionStore
	Repositories        RepositoryResolver
	IndexInjector       IndexInjector
	BranchIndexer       BranchIndexer
	PerfStore           port.AgentPerformanceStore
	Memories            port.AgentMemoryStore
	KPIs                port.AgentKPIStore
	Notifier            Notifier
	Git                 port.GitClient
	LLM                 port.LLMClient
	WorkspaceRoot       string
	Budget              appcontext.Budget
	IndexerCfg          domain.IndexerConfig
	MappingCfg          domain.MappingConfig
	DefaultPolicy       domain.ToolPolicy
	DefaultLang         string
	Settings            port.SettingsStore
	VerificationEnabled bool
	VerifyFixAttempts   int
	TaskTypeModels      map[string]string
	Pipelines           port.TaskPipelineStore
}

func NewRunner(deps RunnerDeps) *Runner {
	lang := deps.DefaultLang
	if lang == "" {
		lang = "en"
	}
	return &Runner{
		agentLoop:         deps.AgentLoop,
		orchSvc:           deps.OrchSvc,
		catalog:           deps.Catalog,
		activityStore:     deps.ActivityStore,
		runs:              deps.Runs,
		sessions:          deps.Sessions,
		projects:          deps.Repositories,
		indexInjector:     deps.IndexInjector,
		branchIndexer:     deps.BranchIndexer,
		perfStore:         deps.PerfStore,
		memories:          deps.Memories,
		kpis:              deps.KPIs,
		notifier:          deps.Notifier,
		git:               deps.Git,
		llm:               deps.LLM,
		workspaceRoot:     deps.WorkspaceRoot,
		budget:            deps.Budget,
		indexerCfg:        deps.IndexerCfg,
		mappingCfg:        deps.MappingCfg,
		defaultPolicy:     deps.DefaultPolicy,
		defaultLang:       lang,
		settings:          deps.Settings,
		verifyEnabled:     deps.VerificationEnabled,
		verifyFixAttempts: deps.VerifyFixAttempts,
		taskTypeModels:    deps.TaskTypeModels,
		pipelines:         deps.Pipelines,
		queue:             make(chan RunJob, 256),
		drain:             make(chan struct{}),
		active:            make(map[uuid.UUID]struct{}),
		queued:            make(map[uuid.UUID]struct{}),
		activeTasks:       make(map[uuid.UUID]uuid.UUID),
		parked:            make(map[uuid.UUID][]RunJob),
		agentSlots:        newSlotGate(),
		taskSlots:         newSlotGate(),
		taskSlotHeld:      make(map[uuid.UUID]bool),
		cancels:           make(map[uuid.UUID]context.CancelFunc),
	}
}

func (r *Runner) language(ctx context.Context) string {
	if r.settings == nil {
		return r.defaultLang
	}
	settings, err := r.settings.Get(ctx)
	if err != nil || strings.TrimSpace(settings.DefaultLanguage) == "" {
		return r.defaultLang
	}
	return settings.DefaultLanguage
}

func (r *Runner) concurrencyLimits(ctx context.Context) (agents, tasks int) {
	if r.settings == nil {
		return 0, 0
	}
	settings, err := r.settings.Get(ctx)
	if err != nil {
		return 0, 0
	}
	return max(settings.MaxConcurrentAgents, 0), max(settings.MaxConcurrentTasks, 0)
}

func (r *Runner) SetTaskUpdater(t TaskUpdater) {
	r.taskUpdater = t
}

func (r *Runner) SetRepositories(resolver RepositoryResolver) {
	r.projects = resolver
}

func (r *Runner) SetProjectModel(m ProjectModel) {
	r.projectModel = m
}

func (r *Runner) SetPipelines(store port.TaskPipelineStore) {
	r.pipelines = store
}

func (r *Runner) SetBilling(gate BillingGate) {
	r.billing = gate
}

func (r *Runner) SetTaskExecutor(ex port.TaskExecutor) {
	r.executor = ex
}

func (r *Runner) SetTaskBlocker(b TaskBlocker) {
	r.blocker = b
}

func (r *Runner) SetToolchainDetector(d port.ToolchainDetector) {
	r.toolchains = d
}

func (r *Runner) detectToolchain(ctx context.Context, job RunJob, workDir string) map[string]string {
	if r.toolchains == nil || !r.toolchains.Available() {
		return nil
	}
	tc, err := r.toolchains.Detect(ctx, workDir)
	if err != nil {
		log.Warn().Err(err).
			Str("task_id", job.Task.ID.String()).Str("workspace", workDir).
			Msg("toolchain: this checkout could not be read for version pins; the session runs on the host defaults")
		return nil
	}
	if len(tc.Env) == 0 {
		return nil
	}
	sources := make([]string, 0, len(tc.Pins))
	for _, pin := range tc.Pins {
		sources = append(sources, pin.Language+" "+pin.Version+" ("+pin.Source+")")
	}
	sort.Strings(sources)
	log.Info().Str("task_id", job.Task.ID.String()).Strs("pins", sources).
		Msg("toolchain: resolved from the repository's own pin files")
	return tc.Env
}

func (r *Runner) SetParkJournal(j *ParkJournal) {
	r.parks = j
}

func (r *Runner) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	r.wg.Add(1)
	go r.dispatch(ctx)
}

func (r *Runner) Stop() {
	r.closeDrain()
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

func (r *Runner) Drain(ctx context.Context) {
	r.closeDrain()
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	if n := r.ActiveCount(); n > 0 {
		log.Info().Int("active_runs", n).Msg("board runner draining, waiting for in-flight runs")
	}
	select {
	case <-done:
		return
	case <-ctx.Done():
		log.Warn().Int("active_runs", r.ActiveCount()).Msg("board runner drain deadline hit, cancelling in-flight runs")
		if r.cancel != nil {
			r.cancel()
		}
		r.wg.Wait()
	}
}

func (r *Runner) closeDrain() {
	r.drainOnce.Do(func() {
		if r.drain != nil {
			close(r.drain)
		}
	})
}

func (r *Runner) IsActive(runID uuid.UUID) bool {
	r.activeMu.RLock()
	defer r.activeMu.RUnlock()
	if _, ok := r.active[runID]; ok {
		return true
	}
	_, ok := r.queued[runID]
	return ok
}

func (r *Runner) IsTaskActive(taskID uuid.UUID) bool {
	if r == nil {
		return false
	}
	r.activeMu.RLock()
	defer r.activeMu.RUnlock()
	_, ok := r.activeTasks[taskID]
	return ok
}

func (r *Runner) ActiveCount() int {
	r.activeMu.RLock()
	defer r.activeMu.RUnlock()
	return len(r.active)
}

func (r *Runner) markActive(runID uuid.UUID) func() {
	r.activeMu.Lock()
	r.active[runID] = struct{}{}
	delete(r.queued, runID)
	r.activeMu.Unlock()
	return func() {
		r.activeMu.Lock()
		delete(r.active, runID)
		r.activeMu.Unlock()
	}
}

func (r *Runner) registerCancel(runID uuid.UUID, cancel context.CancelFunc) func() {
	r.activeMu.Lock()
	r.cancels[runID] = cancel
	r.activeMu.Unlock()
	return func() {
		r.activeMu.Lock()
		delete(r.cancels, runID)
		r.activeMu.Unlock()
	}
}

func (r *Runner) Cancel(runID uuid.UUID) bool {
	r.activeMu.RLock()
	cancel, ok := r.cancels[runID]
	r.activeMu.RUnlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

func stopRequested(parent, runCtx context.Context) bool {
	return runCtx.Err() != nil && parent.Err() == nil
}

func (r *Runner) beginTask(ctx context.Context, job RunJob) bool {
	taskID := job.Task.ID
	if taskID == uuid.Nil {
		return true
	}
	r.activeMu.Lock()
	if holder, busy := r.activeTasks[taskID]; busy {
		r.parked[taskID] = append(r.parked[taskID], job)
		log.Info().Str("task_id", taskID.String()).Str("run_id", job.Run.ID.String()).
			Str("waiting_for_run_id", holder.String()).
			Msg("board run parked: another run holds this task")
		r.activeMu.Unlock()
		return false
	}
	r.activeTasks[taskID] = job.Run.ID
	needSlot := !r.taskSlotHeld[taskID]
	r.activeMu.Unlock()

	if needSlot {
		if !r.taskSlots.acquire(ctx) {
			r.endTask(taskID)
			return false
		}
		r.activeMu.Lock()
		r.taskSlotHeld[taskID] = true
		r.activeMu.Unlock()
	}
	return true
}

func (r *Runner) endTask(taskID uuid.UUID) {
	if taskID == uuid.Nil {
		return
	}
	r.activeMu.Lock()
	delete(r.activeTasks, taskID)
	waiting := r.parked[taskID]
	var next RunJob
	if len(waiting) > 0 {
		next, waiting = waiting[0], waiting[1:]
		if len(waiting) == 0 {
			delete(r.parked, taskID)
		} else {
			r.parked[taskID] = waiting
		}
	}
	releaseSlot := len(waiting) == 0 && r.taskSlotHeld[taskID]
	if releaseSlot {
		delete(r.taskSlotHeld, taskID)
	}
	r.activeMu.Unlock()
	if releaseSlot {
		r.taskSlots.release()
	}
	if next.Run.ID != uuid.Nil {
		r.Enqueue(next)
	}
}

func (r *Runner) startHeartbeat(ctx context.Context, runID uuid.UUID, cancel context.CancelFunc) func() {
	if r.runs == nil {
		return func() {}
	}
	hbCtx, stop := context.WithCancel(ctx)
	go func() {
		every := r.heartbeatEvery
		if every <= 0 {
			every = runHeartbeat
		}
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				status, err := r.runs.Touch(hbCtx, runID)
				if err != nil {
					if hbCtx.Err() == nil {
						log.Warn().Err(err).Str("run_id", runID.String()).Msg("run heartbeat failed")
					}
					continue
				}
				if status == domain.TaskAgentRunStatusCancelled {
					log.Info().Str("run_id", runID.String()).
						Msg("run cancelled elsewhere in the fleet, stopping it here")
					cancel()
					return
				}
			}
		}
	}()
	return stop
}

func (r *Runner) Enqueue(job RunJob) {
	select {
	case r.queue <- job:
		r.markQueued(job.Run.ID)
	default:
		log.Warn().Str("task_id", job.Task.ID.String()).Msg("board runner queue full, dropping job")
	}
}

func (r *Runner) markQueued(runID uuid.UUID) {
	if runID == uuid.Nil {
		return
	}
	r.activeMu.Lock()
	r.queued[runID] = struct{}{}
	r.activeMu.Unlock()
}

func (r *Runner) unmarkQueued(runID uuid.UUID) {
	if runID == uuid.Nil {
		return
	}
	r.activeMu.Lock()
	delete(r.queued, runID)
	r.activeMu.Unlock()
}

func (r *Runner) dispatch(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.drain:
			return
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-r.drain:
			return
		case job := <-r.queue:
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.runJob(ctx, job)
			}()
		}
	}
}

func (r *Runner) runJob(parent context.Context, job RunJob) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer r.registerCancel(job.Run.ID, cancel)()

	if r.alreadyStopped(ctx, job.Run.ID) {
		r.unmarkQueued(job.Run.ID)
		return
	}
	agents, tasks := r.concurrencyLimits(ctx)
	r.agentSlots.setLimit(agents)
	r.taskSlots.setLimit(tasks)

	if !r.agentSlots.acquire(ctx) {
		r.unmarkQueued(job.Run.ID)
		return
	}
	if !r.beginTask(ctx, job) {
		r.agentSlots.release()
		return
	}
	err := r.execute(parent, ctx, cancel, job)
	r.agentSlots.release()
	r.endTask(job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("run_id", job.Run.ID.String()).Msg("board agent run failed")
	}
}

func (r *Runner) alreadyStopped(ctx context.Context, runID uuid.UUID) bool {
	if r.runs == nil {
		return false
	}
	row, err := r.runs.GetByID(ctx, runID)
	if err != nil {
		log.Warn().Err(err).Str("run_id", runID.String()).
			Msg("run status could not be read before starting it, starting it anyway")
		return false
	}
	if !domain.TaskAgentRunIsTerminal(row.Status) {
		return false
	}
	log.Info().Str("run_id", runID.String()).Str("status", row.Status).
		Msg("queued board run skipped: it had already stopped")
	return true
}

func (r *Runner) execute(parent, ctx context.Context, cancel context.CancelFunc, job RunJob) error {
	run := job.Run

	if r.runs != nil {
		claim, err := r.runs.ClaimRun(ctx, port.RunClaim{
			RunID:      run.ID,
			TaskID:     job.Task.ID,
			LiveWithin: runLiveWithin,
		})
		if err != nil {
			return err
		}
		if !claim.Claimed {
			log.Info().Str("run_id", run.ID.String()).Str("task_id", job.Task.ID.String()).
				Str("reason", claim.Reason).
				Msg("board run not claimed; it stays pending for the reconciler to re-dispatch")
			return nil
		}
	}
	run.Status = domain.TaskAgentRunStatusRunning
	defer r.markActive(run.ID)()
	defer r.startHeartbeat(ctx, run.ID, cancel)()

	fail := func(cause error) error {
		if stopRequested(parent, ctx) {
			log.Info().Str("run_id", run.ID.String()).Str("task_id", job.Task.ID.String()).
				Msg("board run stopped, dropping the failure it was about to report")
			return nil
		}
		return r.failRun(ctx, run, cause)
	}

	if r.billing != nil {
		if allowed, reason := r.billing.Allow(ctx); !allowed {
			r.billing.PauseTask(ctx, job.RepositoryID, job.Task.ID)
			run.Status = domain.TaskAgentRunStatusFailed
			run.Summary = "Quota exhausted: " + reason + " — the task resumes automatically when the limit renews."
			_, _ = r.runs.Update(ctx, run)
			log.Info().Str("task_id", job.Task.ID.String()).Msg("board run deferred: budget exhausted")
			return nil
		}
	}

	agentRec, err := r.catalog.GetAgent(ctx, job.Run.AgentID)
	if err != nil {
		return fail(err)
	}

	dispatchedIn := job.Task.Column
	job.Task = r.enterWorkingColumn(ctx, job)
	if job.Task.Column != dispatchedIn {
		job.EnteredFrom = dispatchedIn
	}
	wf := r.workflowFor(ctx, job.Task.TaskType)

	skills, err := r.catalog.ListSkillsByAgent(ctx, agentRec.ID)
	if err != nil {
		return fail(err)
	}
	enabledSkills := make([]domain.Skill, 0, len(skills))
	for _, sk := range skills {
		if sk.Enabled {
			enabledSkills = append(enabledSkills, sk)
		}
	}
	techStacks, err := r.catalog.ListTechStacksByAgent(ctx, agentRec.ID)
	if err != nil {
		return fail(err)
	}
	rules, err := r.catalog.ListEnabledRulesByAgent(ctx, agentRec.ID)
	if err != nil {
		return fail(err)
	}
	ruleTexts := make([]string, 0, len(rules))
	for _, rule := range rules {
		ruleTexts = append(ruleTexts, rule.Content)
	}

	repoRec, err := r.projects.ResolveRepository(ctx, job.RepositoryID)
	if err != nil {
		return fail(err)
	}
	rootPath := repoRec.RootPath
	if rootPath == "" {
		return fail(fmt.Errorf("repository %q has no root path configured", repoRec.Name))
	}

	workDir := rootPath
	taskWorkspace := ""
	taskBranch := ""
	if r.git != nil {
		if err := r.ensureWorkingCopy(ctx, repoRec, rootPath); err != nil {
			return fail(err)
		}
	} else if err := workspace.EnsureDir(rootPath); err != nil {
		return fail(err)
	}
	if r.git != nil && r.workspaceRoot != "" && r.git.HasGit(rootPath) {
		wsPath, wsPathErr := workspace.TaskDir(r.workspaceRoot, job.Task.ID)
		if wsPathErr != nil {
			return fail(fmt.Errorf("task workspace path could not be resolved, agent was not started: %w", wsPathErr))
		}
		branch := domain.TaskBranchName(job.Task)
		if wsErr := r.git.EnsureTaskWorkspace(ctx, rootPath, wsPath, branch); wsErr != nil {
			return fail(fmt.Errorf("task workspace could not be prepared (repo clone/branch creation failed), agent was not started: %w", wsErr))
		}
		workDir = wsPath
		taskWorkspace = wsPath
		taskBranch = branch
		run.WorkspacePath = wsPath
		_, _ = r.runs.Update(ctx, run)
	}

	skillDelivery := prompt.SkillsInPrompt
	if flavor, isCLI := cliFlavor(agentRec.ProviderType); isCLI {
		if err := agentfs.Exclude(workDir); err != nil {
			return fail(fmt.Errorf("agent catalog could not be excluded from git, agent was not started: %w", err))
		}
		res, mErr := agentfs.Materialize(workDir, flavor, agentfs.Bundle{
			Agent:      agentRec,
			Skills:     enabledSkills,
			TechStacks: techStacks,
			Rules:      ruleTexts,
		})
		if mErr != nil {
			return fail(fmt.Errorf("agent catalog could not be written to the workspace, agent was not started: %w", mErr))
		}
		defer func() { _ = agentfs.Clean(workDir, flavor) }()
		log.Debug().
			Str("agent", agentRec.Name).
			Int("written", len(res.Written)).
			Int("unchanged", res.Unchanged).
			Int("removed", len(res.Removed)).
			Msg("agent catalog materialised into the task workspace")
		skillDelivery = prompt.SkillsOnDisk
	}

	var sessionID uuid.UUID
	if r.sessions != nil {
		title := fmt.Sprintf("board:%s", job.Task.ID.String()[:8])
		sess, err := r.sessions.Create(ctx, title, agentRec.Model, workDir, &job.RepositoryID, nil, nil)
		if err != nil {
			return fail(err)
		}
		sessionID = sess.ID
	}

	runCtx := registry.ContextWithAgentID(registry.ContextWithRepositoryID(registry.ContextWithWorkspaceDir(ctx, workDir), job.RepositoryID), job.Run.AgentID)
	runCtx = registry.ContextWithTaskID(runCtx, job.Task.ID)
	runCtx, toolUsage := registry.ContextWithToolUsage(runCtx)
	runCtx, tokenUsage := usageapp.ContextWithTokenUsage(runCtx)
	if sessionID != uuid.Nil {
		runCtx = registry.ContextWithSessionID(runCtx, sessionID)
	}
	if taskBranch != "" {
		runCtx = registry.ContextWithBranch(runCtx, taskBranch)
	}
	sessionEnv := r.detectToolchain(runCtx, job, workDir)
	if overlay := toolchain.Default.Overlay(workDir); len(overlay.Env) > 0 || len(overlay.Warnings) > 0 {
		runCtx = registry.ContextWithTaskEnv(runCtx, overlay.Env)
		for _, w := range overlay.Warnings {
			log.Warn().Str("task_id", job.Task.ID.String()).Str("workspace", workDir).Msg("toolchain: " + w)
		}
	}
	requestID := registry.RequestIDFromContext(ctx)
	if requestID == "" {
		requestID = job.Run.ID.String()
	}

	model := agentRec.Model
	if override, ok := r.taskTypeModels[string(job.Task.TaskType)]; ok && override != "" {
		model = override
	}
	runCtx, rec, err := activity.StartRun(runCtx, r.activityStore, sessionIDPtr(sessionID), requestID, model)
	if err != nil {
		return fail(err)
	}
	if rec != nil {
		run.SessionRunID = ptrUUID(rec.RunID())
		_, _ = r.runs.Update(ctx, run)
		defer func() {
			var quotaErr *domain.QuotaBlock
			switch {
			case stopRequested(parent, ctx):
				rec.Complete(domain.TaskAgentRunStatusCancelled)
			case errors.As(err, &quotaErr):
				rec.Complete(domain.TaskAgentRunStatusCompleted)
			case err != nil:
				rec.Complete("failed")
			default:
				rec.Complete("completed")
			}
		}()
	}

	systemPrompt := prompt.BuildSystemPromptFor(agentRec, enabledSkills, techStacks, ruleTexts, r.language(runCtx), skillDelivery)
	criteriaItems := r.criteriaForRun(runCtx, job, wf)
	changedSinceVerdict := r.changedSinceByVerifiedSHA(runCtx, workDir, criteriaItems)
	triggerMsg := buildTriggerMessage(job, wf, criteriaItems, changedSinceVerdict)

	var scoreMsg, kpiMsg, memMsg, diffMsg, prMsg, pipelineMsg, revisionMsg, clarificationsMsg, prevFailuresMsg, analysisMsg string

	if r.perfStore != nil {
		if perfScore, perfErr := r.perfStore.GetScore(runCtx, agentRec.ID); perfErr == nil {
			recent, _ := r.perfStore.RecentEvents(runCtx, agentRec.ID, 5)
			scoreMsg = prompt.ScoreContextMessage(perfScore, recent)
		}
	}
	if r.kpis != nil {
		kpiDefs, kpiErr := r.kpis.ListByAgent(runCtx, agentRec.ID)
		if kpiErr == nil && len(kpiDefs) > 0 {
			latest, _ := r.kpis.LatestResults(runCtx, agentRec.ID)
			kpiMsg = prompt.KPIContextMessage(kpiDefs, latest)
		}
	}
	if r.memories != nil {
		repositoryID := job.RepositoryID
		repoName := ""
		if repo, repoErr := r.projects.ResolveRepository(runCtx, repositoryID); repoErr == nil {
			repoName = repo.Name
		}
		if mems := memory.Recall(runCtx, r.memories, agentRec.ID, &repositoryID, 8); len(mems) > 0 {
			memMsg = prompt.MemoryContextMessage(mems, repoName)
		}
	}
	if taskWorkspace != "" && r.git != nil {
		if diff, diffErr := r.git.TaskDiff(ctx, taskWorkspace); diffErr == nil && diff != "" {
			diffMsg = reviewDiffMessage(wf, job.Task.Column, diff)
		}
	}
	if wf.Has(job.Task.Column, domain.BehaviourRequirePRForReview) && taskWorkspace != "" && r.git != nil {
		var prErr error
		prMsg, prErr = r.reviewPRContext(ctx, taskWorkspace, job.Task.ID)
		if prErr != nil {
			if stopRequested(parent, ctx) {
				log.Info().Str("run_id", run.ID.String()).Str("task_id", job.Task.ID.String()).
					Msg("board run stopped while opening the review pull request")
				return nil
			}
			err = r.failRunNoPR(ctx, job, run, prErr)
			return err
		}
	}
	if job.isRevision() {
		prMsg = r.revisionPRComments(ctx, job)
	}
	if job.isRevision() && r.pipelines != nil {
		if pl, plErr := r.pipelines.LatestByTask(ctx, job.Task.ID); plErr != nil {
			if !errors.Is(plErr, domain.ErrPipelineNotFound) {
				log.Warn().Err(plErr).Str("task_id", job.Task.ID.String()).Msg("fetch latest pipeline for revision context failed")
			}
		} else if pl.Status == domain.PipelineStatusFailed {
			report := pipelineFailureReport(pl)
			pipelineMsg = "## Pipeline failure (fix these before moving back to ready_for_qa)\n" + report
		}
	}
	analysisMsg = r.analysisContext(ctx, job)
	comments := r.taskComments(ctx, job)
	if job.isRevision() {
		revisionMsg = revisionCommentsMessage(comments)
	}
	clarificationsMsg = prompt.AnsweredClarificationsMessage(comments)
	resumeCLISession := ""
	var prevRuns []domain.TaskAgentRun
	if rows, prevErr := r.runs.ListByTask(runCtx, job.Task.ID, quotaParkHistoryDepth); prevErr != nil {
		log.Warn().Err(prevErr).Str("task_id", job.Task.ID.String()).Msg("previous run lookup for failure context failed")
	} else {
		prevRuns = rows
		prevFailuresMsg = previousRunFailuresMessage(prevRuns, run.ID)
		resumeCLISession = latestCLISession(prevRuns, run.ID, job.Run.AgentID)
	}
	var projectDesc string
	if repoCtx, repoCtxErr := r.projects.ResolveRepository(ctx, job.RepositoryID); repoCtxErr == nil {
		projectDesc = repoCtx.Description
	}

	policy := domain.MergeToolPolicy(r.defaultPolicy, agentRec.ToolPolicy)
	stage, _ := wf.Stage(job.Task.Column)
	upliftedPolicy := domain.RestrictToolsForStage(domain.UpliftWorkspaceTools(policy), stage, wf.Type)

	history := []domain.Message{{Role: domain.RoleSystem, Content: systemPrompt}}
	history = append(history, domain.Message{Role: domain.RoleSystem, Content: prompt.SubtaskWorkspaceNote(workDir)})
	history = append(history, prependProjectContext(nil, projectDesc, projectmodel.ToolsNote(upliftedPolicy))...)
	if scoreMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: scoreMsg})
	}
	if kpiMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: kpiMsg})
	}
	if memMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: memMsg})
	}
	history = append(history, domain.Message{Role: domain.RoleUser, Content: triggerMsg})
	if analysisMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: analysisMsg})
	}
	if diffMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: diffMsg})
	}
	if prMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: prMsg})
	}
	if pipelineMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: pipelineMsg})
	}
	if revisionMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: revisionMsg})
	}
	if clarificationsMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: clarificationsMsg})
	}
	if prevFailuresMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: prevFailuresMsg})
	}

	if r.indexInjector != nil && r.indexerCfg.Enabled && sessionID != uuid.Nil {
		topK := r.indexerCfg.TopK
		if topK <= 0 {
			topK = 5
		}
		if injected, injectErr := r.indexInjector.InjectContext(runCtx, sessionID, history, domain.InjectOptions{
			TopK:            topK,
			IncludeTree:     r.mappingCfg.Enabled,
			IncludeSkeleton: r.mappingCfg.Enabled && r.mappingCfg.InjectOnSessionStart,
			MaxChunkTokens:  6000,
		}); injectErr != nil {
			log.Warn().Err(injectErr).Str("task_id", job.Task.ID.String()).Msg("board run: index context injection failed, continuing without it")
		} else if len(injected) == len(history)+1 {
			history = append(history, injected[0])
		} else {
			history = injected
		}
	}
	if r.budget.MaxTokens > 0 {
		history = r.budget.Apply(history)
	}

	var resp domain.AgentResponse
	cliSession := &agent.CLISession{}
	runCtx = agent.ContextWithCLISession(runCtx, cliSession)
	switch {
	case domain.RequiresHostExecutor(agentRec.ProviderType):
		if r.executor == nil || !r.executor.Supports(agentRec.ProviderType) {
			binary, envVar := hostExecutorHint(agentRec.ProviderType)
			return fail(fmt.Errorf(
				"%s binary not available on this host: agent %q runs on the %s provider, which needs the %q CLI installed where agent-server runs (set %s if it is not on PATH). Move the agent to an API provider or install the CLI",
				binary, agentRec.Name, agentRec.ProviderType, binary, envVar))
		}
		if err := r.requireConnectedCLI(ctx, agentRec); err != nil {
			return fail(err)
		}
		resp, err = r.executor.Execute(runCtx, domain.TaskExecution{
			History:         history,
			Model:           model,
			Provider:        agentRec.ProviderType,
			MaxTurns:        agentRec.MaxTurns,
			Effort:          agentRec.Effort,
			Policy:          upliftedPolicy,
			WorkDir:         workDir,
			Env:             sessionEnv,
			ResumeSessionID: resumeCLISession,
			TaskKey:         job.Task.Key,
			TaskTitle:       job.Task.Title,
			SkillsOnDisk:    skillDelivery == prompt.SkillsOnDisk,
		})
		cliSession.Set(resp.CLISessionID)
	case r.orchSvc != nil:
		resp, err = r.orchSvc.RunSolo(runCtx, triggerMsg, history, model, upliftedPolicy, r.language(runCtx), job.Run.AgentID)
	default:
		resp, err = r.agentLoop.RunTask(runCtx, history, model, agentRec.ProviderType, upliftedPolicy,
			agent.WithLightModel(agentRec.Model), agent.WithSessionLimits(agentRec.MaxTurns, agentRec.Effort))
	}
	stampToolStats(&run, toolUsage)
	stampTokenUsage(&run, tokenUsage)
	if stopRequested(parent, runCtx) {
		log.Info().Str("run_id", run.ID.String()).Str("task_id", job.Task.ID.String()).
			Msg("board run stopped, leaving the task as it stands")
		return nil
	}
	if err != nil {
		var quotaErr *domain.QuotaBlock
		if errors.As(err, &quotaErr) {
			return r.quotaOrPark(ctx, job, run, agentRec, prevRuns, cliSession, quotaErr, fail)
		}
		var budgetErr *agent.BudgetExhaustedError
		if errors.As(err, &budgetErr) {
			return r.failRunOutOfBudget(ctx, job, run, taskWorkspace, taskBranch, agentRec, budgetErr)
		}
		return fail(err)
	}

	if resp.Clarification != nil {
		r.openClarificationChat(runCtx, job, agentRec, resp, workDir)
	}

	if resp.ResourceBlock != nil {
		r.parkOnResource(ctx, job, agentRec, resp)
	}

	buildVerified := true
	if r.verifyEnabled && taskWorkspace != "" && resp.Clarification == nil && resp.ResourceBlock == nil &&
		wf.Has(job.Task.Column, domain.BehaviourBuildVerify) {
		var quotaBlock *domain.QuotaBlock
		resp, buildVerified, quotaBlock = r.verifyAndFix(runCtx, job, agentRec, history, resp, model, upliftedPolicy, taskWorkspace)
		if quotaBlock != nil {
			stampToolStats(&run, toolUsage)
			stampTokenUsage(&run, tokenUsage)
			return r.quotaOrPark(ctx, job, run, agentRec, prevRuns, cliSession, quotaBlock, fail)
		}
	}
	if resp.Verification != nil && !resp.Verification.Passed {
		buildVerified = false
		r.reportPlanVerificationFailure(ctx, job, *resp.Verification)
	}

	if resp.ResourceBlock == nil && isUngroundedAnalysis(wf, job.Task, resp, toolUsage) {
		err = r.failRunUngrounded(ctx, job, run, resp)
		return err
	}

	if resp.ResourceBlock == nil && isUngroundedQA(wf, job.Task, resp, toolUsage) {
		err = r.failRunUngroundedQA(ctx, job, run, resp, ungroundedQAReason)
		return err
	}

	if resp.Clarification == nil && resp.ResourceBlock == nil && r.qaSkippedTheUI(ctx, job, wf, toolUsage) {
		err = r.failRunUngroundedQA(ctx, job, run, resp, noUIEvidenceReason)
		return err
	}

	if resp.ResourceBlock == nil && isUngroundedPMUAT(wf, job.Task, resp, toolUsage) {
		err = r.failRunUngroundedPMUAT(ctx, job, run, resp, ungroundedPMUATReason)
		return err
	}

	if resp.Clarification == nil && resp.ResourceBlock == nil && r.pmSkippedCoverageEvidence(ctx, job, wf, run, toolUsage) {
		err = r.failRunUngroundedPMUAT(ctx, job, run, resp, pmUncoveredCriterionReason)
		return err
	}

	criteriaSettled := true
	if resp.Clarification == nil && resp.ResourceBlock == nil {
		var quotaBlock *domain.QuotaBlock
		resp, criteriaSettled, quotaBlock = r.sweepOpenCriteria(runCtx, job, agentRec, history, resp, model, upliftedPolicy)
		if quotaBlock == nil {
			quotaBlock = r.sweepReviewVerdict(runCtx, job, agentRec, history, resp, model, upliftedPolicy)
		}
		if quotaBlock != nil {
			stampToolStats(&run, toolUsage)
			stampTokenUsage(&run, tokenUsage)
			return r.quotaOrPark(ctx, job, run, agentRec, prevRuns, cliSession, quotaBlock, fail)
		}
	}

	if !criteriaSettled {
		run.Status = domain.TaskAgentRunStatusFailed
		run.Summary = unsettledCriteriaSummary(len(r.openCriteria(runCtx, job)))
	} else {
		run.Status = domain.TaskAgentRunStatusCompleted
		run.Summary = strings.TrimSpace(resp.Message.Content)
		if resp.Clarification != nil {
			run.Summary = "Waiting for an answer: " + resp.Clarification.Context
		}
		if resp.ResourceBlock != nil {
			run.Summary = "Waiting for a shared resource: " + resp.ResourceBlock.Detail
		}
	}
	run.Summary = truncateHead(run.Summary, 500)
	if taskWorkspace != "" && resp.Clarification == nil && resp.ResourceBlock == nil && wf.Has(job.Task.Column, domain.BehaviourCommitOnFinish) {
		commitMsg := r.writeCommitMessage(ctx, commitDetails{
			TaskKey:   job.Task.Key,
			Title:     job.Task.Title,
			Summary:   run.Summary,
			AgentName: agentRec.Name,
			Writer:    agentWriterModel(agentRec),
		})
		if pushErr := r.git.CommitAndPush(ctx, taskWorkspace, commitMsg); pushErr != nil {
			log.Warn().Err(pushErr).Str("task_id", job.Task.ID.String()).Msg("task workspace commit/push failed")
		} else if r.branchIndexer != nil && taskBranch != "" {
			r.branchIndexer.StartIndexBranch(ctx, job.RepositoryID, taskBranch, taskWorkspace)
		}
		if buildVerified {
			r.advanceToCodeReview(ctx, job, wf, taskWorkspace, toolUsage)
			r.advanceToAnalizReview(ctx, job, wf, toolUsage)
		} else {
			log.Info().Str("task_id", job.Task.ID.String()).
				Msg("hand-off: build verification failed after every fix round, task stays in the working column")
		}
	}
	pctx, cancelPersist := persistCtx(ctx)
	defer cancelPersist()
	_, err = r.runs.Update(pctx, run)
	return err
}

func (r *Runner) parkOnResource(ctx context.Context, job RunJob, agentRec domain.Agent, resp domain.AgentResponse) {
	if r.blocker == nil {
		return
	}
	detail := strings.TrimSpace(resp.ResourceBlock.Detail)
	if detail == "" {
		detail = "waiting for " + resp.ResourceBlock.Resource
	}
	previous, err := r.blocker.BlockOnResource(ctx, job.RepositoryID, job.Task.ID,
		resp.ResourceBlock.Resource, detail)
	if err != nil {
		log.Warn().Err(err).
			Str("task_id", job.Task.ID.String()).
			Str("resource", resp.ResourceBlock.Resource).
			Msg("parking task on a busy resource failed")
		return
	}
	r.parks.Record(ctx, job.RepositoryID, job.Task, previous,
		resp.ResourceBlock.Resource, domain.MoveReasonResourceBlocked)
	log.Info().
		Str("task_id", job.Task.ID.String()).
		Str("resource", resp.ResourceBlock.Resource).
		Str("agent", agentRec.Name).
		Msg("task parked waiting for a shared resource")
}

func (r *Runner) parkOnQuota(ctx context.Context, job RunJob, run domain.TaskAgentRun, agentRec domain.Agent, block *domain.QuotaBlock, streak int) error {
	resumeAt := block.ResumeAt
	if resumeAt.IsZero() || !resumeAt.After(time.Now().Add(time.Minute)) {
		resumeAt = time.Now().Add(domain.QuotaParkWindow(streak))
	}
	run.Status = domain.TaskAgentRunStatusCompleted
	run.CLISessionID = block.CLISessionID
	run.QuotaResumeAt = &resumeAt
	head := "usage limit reached"
	if label := block.ProviderLabel(); label != "" {
		head = label + " usage limit reached"
	}
	run.Summary = truncateHead(fmt.Sprintf("%s — the task resumes automatically after %s. %s",
		head, resumeAt.Format(time.RFC1123), block.Detail), 500)

	pctx, cancelPersist := persistCtx(ctx)
	defer cancelPersist()
	if _, err := r.runs.Update(pctx, run); err != nil {
		return fmt.Errorf("record the agent cli quota park: %w", err)
	}

	if r.blocker != nil {
		previous, err := r.blocker.BlockOnResource(pctx, job.RepositoryID, job.Task.ID,
			domain.ResourceClaudeCodeQuota, run.Summary)
		if err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).
				Msg("parking a task on the agent cli usage limit failed")
		} else {
			r.parks.Record(pctx, job.RepositoryID, job.Task, previous,
				domain.ResourceClaudeCodeQuota, domain.MoveReasonQuotaExhausted)
		}
	}
	log.Info().
		Str("task_id", job.Task.ID.String()).
		Str("agent", agentRec.Name).
		Str("cli_session_id", block.CLISessionID).
		Time("resume_at", resumeAt).
		Msg("task parked on the agent cli usage limit")
	return nil
}

func (r *Runner) quotaOrPark(
	ctx context.Context,
	job RunJob,
	run domain.TaskAgentRun,
	agentRec domain.Agent,
	prevRuns []domain.TaskAgentRun,
	cliSession *agent.CLISession,
	block *domain.QuotaBlock,
	fail func(error) error,
) error {
	if block.CLISessionID == "" {
		block.CLISessionID = cliSession.ID()
	}
	streak := quotaParkStreak(prevRuns, run.ID)
	if streak >= maxConsecutiveQuotaParks {
		log.Warn().Str("task_id", job.Task.ID.String()).Int("consecutive_parks", streak).
			Msg("agent cli quota park cap reached, failing the run instead of parking it again")
		label := block.ProviderLabel()
		if label == "" {
			label = "agent CLI"
		}
		return fail(fmt.Errorf(
			"this task has parked on the %s usage limit %d times in a row without completing a run (last: %v). "+
				"Failing it instead of parking again: either the limit has been exhausted for a long stretch, "+
				"or the run keeps reporting a limit it is not actually hitting",
			label, streak, block))
	}
	return r.parkOnQuota(ctx, job, run, agentRec, block, streak)
}

const maxConsecutiveQuotaParks = 5

const quotaParkHistoryDepth = maxConsecutiveQuotaParks + 1

func quotaParkStreak(prevRuns []domain.TaskAgentRun, currentRunID uuid.UUID) int {
	streak := 0
	for _, prev := range prevRuns {
		if prev.ID == currentRunID {
			continue
		}
		if prev.QuotaResumeAt == nil {
			return streak
		}
		streak++
	}
	return streak
}

func latestCLISession(prevRuns []domain.TaskAgentRun, currentRunID, agentID uuid.UUID) string {
	for _, prev := range prevRuns {
		if prev.ID == currentRunID {
			continue
		}
		if prev.AgentID != agentID {
			return ""
		}
		if prev.QuotaResumeAt == nil {
			return ""
		}
		return prev.CLISessionID
	}
	return ""
}

func (r *Runner) openClarificationChat(ctx context.Context, job RunJob, agentRec domain.Agent, resp domain.AgentResponse, rootPath string) {
	if r.sessions == nil {
		return
	}
	sessionID, ok := r.clarificationSession(ctx, job, agentRec, rootPath)
	if !ok {
		return
	}
	content := strings.TrimSpace(resp.Message.Content)
	if content == "" {
		content = resp.Clarification.Context
	}
	clarJSON, marshalErr := json.Marshal(resp.Clarification)
	if marshalErr != nil {
		clarJSON = nil
	}
	if _, err := r.sessions.AppendMessage(ctx, sessionID, domain.RoleAssistant, content, nil, clarJSON); err != nil {
		log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("clarification message append failed")
	}
	if r.blocker != nil {
		question := prompt.FormatClarificationQuestions(*resp.Clarification)
		if err := r.blocker.BlockOnQuestion(ctx, job.RepositoryID, job.Task.ID, sessionID, question); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("blocking task on clarification failed")
		}
	}
	if r.notifier != nil {
		r.notifier.Notify(agentRec.Name+" asked a question", job.Task.Title+": "+resp.Clarification.Context)
	}
}

func (r *Runner) clarificationSession(ctx context.Context, job RunJob, agentRec domain.Agent, rootPath string) (uuid.UUID, bool) {
	if existing := job.Task.ClarificationSessionID; existing != nil {
		if _, err := r.sessions.Get(ctx, *existing); err == nil {
			return *existing, true
		}
		log.Warn().Str("session_id", existing.String()).Str("task_id", job.Task.ID.String()).
			Msg("recorded clarification chat is gone, opening a new one")
	}
	agentID := job.Run.AgentID
	title := truncateHead("Question: "+job.Task.Title, 120)
	sess, err := r.sessions.Create(ctx, title, agentRec.Model, rootPath, &job.RepositoryID, &agentID, nil)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("clarification chat session create failed")
		return uuid.Nil, false
	}
	return sess.ID, true
}

func (r *Runner) ensureWorkingCopy(ctx context.Context, repo domain.Repository, rootPath string) error {
	if r.git.HasGit(rootPath) {
		if found := strings.TrimSpace(repo.RemoteURL); found != "" {
			if origin := strings.TrimSpace(r.git.OriginURL(ctx, rootPath)); origin != "" && !domain.SameGitRemote(origin, found) {
				return fmt.Errorf(
					"the working copy at %s is a checkout of a different repository than %q is on record as; refusing to run an agent on it",
					rootPath, repo.Name)
			}
		}
		return nil
	}
	remote := strings.TrimSpace(repo.RemoteURL)
	if remote == "" {
		return fmt.Errorf(
			"repository %q has no working copy at %s and no remote_url on record to restore it from — re-import it from GitHub (or set its remote) before agents can work on it",
			repo.Name, rootPath)
	}
	if entries, err := os.ReadDir(rootPath); err == nil && len(entries) > 0 {
		return fmt.Errorf("repository %q root %s exists but is not a git repository; refusing to run an agent on it", repo.Name, rootPath)
	}
	log.Info().Str("repository", repo.Name).Str("root", rootPath).Msg("working copy missing, restoring from origin")
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := r.git.CloneRepo(cctx, remote, rootPath); err != nil {
		return fmt.Errorf("restore working copy for %q from %s: %w", repo.Name, remote, err)
	}
	if !r.git.HasGit(rootPath) {
		return fmt.Errorf("restore working copy for %q: %s is still not a git repository after clone", repo.Name, rootPath)
	}
	return nil
}

func (r *Runner) enterWorkingColumn(ctx context.Context, job RunJob) domain.BoardTask {
	task := job.Task
	if r.taskUpdater == nil {
		return task
	}

	wf := r.workflowFor(ctx, task.TaskType)
	if !wf.Has(task.Column, domain.BehaviourAutoEnter) {
		return task
	}
	target, _ := wf.Param(task.Column, domain.BehaviourAutoEnter, "to")
	column := domain.TaskColumn(target)
	if column == "" {
		return task
	}
	if assigneeOnly, _ := wf.Param(task.Column, domain.BehaviourAutoEnter, "assignee_only"); assigneeOnly == "true" {
		if task.AssigneeAgentID == nil || *task.AssigneeAgentID != job.Run.AgentID {
			return task
		}
	}

	agentID := job.Run.AgentID
	updated, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, task.ID, domain.UpdateBoardTaskRequest{
		Column:       &column,
		Actor:        domain.TaskActorAgent,
		ActorAgentID: &agentID,
	})
	if err != nil {
		log.Warn().Err(err).
			Str("task_id", task.ID.String()).
			Str("agent_id", agentID.String()).
			Str("column", string(column)).
			Msg("board run: automatic move into the working column failed, leaving it to the agent")
		return task
	}

	log.Info().
		Str("task_id", task.ID.String()).
		Str("agent_id", agentID.String()).
		Str("column", string(column)).
		Msg("board run: task moved into its working column for the run that is working it")
	return updated
}

type taskColumnReader interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

func (r *Runner) advanceToCodeReview(ctx context.Context, job RunJob, wf domain.Workflow, taskWorkspace string, usage *registry.ToolUsage) {
	if r.taskUpdater == nil || r.git == nil || taskWorkspace == "" {
		return
	}
	target, ok := wf.Param(job.Task.Column, domain.BehaviourAdvanceOnDiff, "to")
	if !ok || target == "" {
		return
	}

	diff, diffErr := r.git.TaskDiff(ctx, taskWorkspace)
	if diffErr != nil {
		log.Warn().Err(diffErr).Str("task_id", job.Task.ID.String()).Msg("hand-off: task diff unreadable, leaving the column to the agent")
		return
	}
	if strings.TrimSpace(diff) == "" {
		log.Info().Str("task_id", job.Task.ID.String()).Msg("hand-off: run produced no diff, task stays where it is")
		return
	}

	if usage != nil && !usage.UsedAny(domain.ImplementationVerificationTools...) {
		log.Warn().Str("task_id", job.Task.ID.String()).
			Msg("hand-off: run wrote a diff but never executed a command, staying in the working column")
		if _, cErr := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content: "Otomatik code_review geçişi yapılmadı: bu run kod yazdı ama hiçbir komut çalıştırmadı " +
				"(run_terminal ile build/test kaydı yok). Değişiklik branch'te duruyor. " +
				"Bir sonraki run projenin build ve test komutlarını çalıştırıp çıktıyı okumalı, kırmızıysa bu run içinde düzeltmeli.",
		}); cErr != nil {
			log.Warn().Err(cErr).Str("task_id", job.Task.ID.String()).Msg("hand-off: unverified-run comment failed")
		}
		return
	}

	if usage != nil && r.uiRepo(ctx, job.RepositoryID) && !usage.UsedAny(domain.UIObservationTools...) {
		needsUI := true
		if files, filesErr := r.git.TaskChangedFiles(ctx, taskWorkspace); filesErr == nil {
			needsUI = domain.DiffNeedsUIEvidence(files)
		}
		if needsUI {
			log.Warn().Str("task_id", job.Task.ID.String()).
				Msg("hand-off: UI change never observed, staying in the working column")
			if _, cErr := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
				AuthorType: "system",
				Content: "Otomatik code_review geçişi yapılmadı: bu run arayüzü değiştirdi ama ekrana hiç bakmadı " +
					"(browser_screenshot / browser_read_dom / mobile_screenshot / mobile_read_ui kaydı yok). " +
					"Yeşil build ekranın doğru göründüğünü söylemez — eksik ikon \"?\" olarak render edilir, taşan bir " +
					"öğe telefonda yatay kaydırma yapar, ikisi de derlenir. Bir sonraki run dev server'ı arka planda " +
					"başlatıp değişen sayfayı açmalı, masaüstü ve mobil boyutta ekran görüntüsü almalı ve eklediği " +
					"öğenin DOM'da olduğunu doğrulamalı.",
			}); cErr != nil {
				log.Warn().Err(cErr).Str("task_id", job.Task.ID.String()).Msg("hand-off: unseen-UI comment failed")
			}
			return
		}
	}

	if reader, ok := r.taskUpdater.(taskColumnReader); ok {
		if fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("hand-off: task re-read failed, using the run's snapshot")
		} else if fresh.Column != job.Task.Column {
			log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(fresh.Column)).
				Msg("hand-off: task already left the column during the run")
			return
		}
	}

	column := domain.TaskColumn(target)
	agentID := job.Run.AgentID
	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &column,
		Actor:        domain.TaskActorAgent,
		ActorAgentID: &agentID,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("hand-off: automatic move to code_review failed")
		if _, cErr := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    "Otomatik code_review geçişi reddedildi: " + err.Error(),
		}); cErr != nil {
			log.Warn().Err(cErr).Str("task_id", job.Task.ID.String()).Msg("hand-off: refusal comment failed")
		}
		return
	}
	log.Info().Str("task_id", job.Task.ID.String()).Str("agent_id", agentID.String()).
		Msg("hand-off: implementation run finished with a diff, task moved to code_review")
}

func (r *Runner) advanceToAnalizReview(ctx context.Context, job RunJob, wf domain.Workflow, usage *registry.ToolUsage) {
	if r.taskUpdater == nil {
		return
	}
	target, ok := wf.Param(job.Task.Column, domain.BehaviourAdvanceOnDocument, "to")
	if !ok || target == "" {
		return
	}

	if usage != nil && !usage.UsedAny(domain.AnalizDocumentTools...) {
		log.Info().Str("task_id", job.Task.ID.String()).
			Msg("hand-off: analiz run attached no document, task stays in the working column")
		return
	}

	if reader, ok := r.taskUpdater.(taskColumnReader); ok {
		if fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("hand-off: task re-read failed, using the run's snapshot")
		} else if fresh.Column != job.Task.Column {
			log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(fresh.Column)).
				Msg("hand-off: task already left the column during the run")
			return
		}
	}

	column := domain.TaskColumn(target)
	agentID := job.Run.AgentID
	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &column,
		Actor:        domain.TaskActorAgent,
		ActorAgentID: &agentID,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("hand-off: automatic move to analiz_review failed")
		if _, cErr := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    "Otomatik analiz_review geçişi reddedildi: " + err.Error(),
		}); cErr != nil {
			log.Warn().Err(cErr).Str("task_id", job.Task.ID.String()).Msg("hand-off: refusal comment failed")
		}
		return
	}
	log.Info().Str("task_id", job.Task.ID.String()).Str("agent_id", agentID.String()).
		Msg("hand-off: analiz run finished with a document, task moved to analiz_review")
}

func stampToolStats(run *domain.TaskAgentRun, usage *registry.ToolUsage) {
	calls, failures := usage.Totals()
	run.ToolCalls = calls
	run.ToolErrors = failures
	run.ErrorPattern = truncateHead(renderFailurePattern(usage.Failures()), 500)
}

func stampTokenUsage(run *domain.TaskAgentRun, usage *usageapp.TokenUsage) {
	t := usage.Totals()
	run.LLMCalls = t.LLMCalls
	run.PromptTokens = t.PromptTokens
	run.CompletionTokens = t.CompletionTokens
	run.CacheReadTokens = t.CacheReadTokens
	run.CacheWriteTokens = t.CacheWriteTokens
}

func renderFailurePattern(failures map[string]int) string {
	if len(failures) == 0 {
		return ""
	}
	names := make([]string, 0, len(failures))
	for name := range failures {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if failures[names[i]] != failures[names[j]] {
			return failures[names[i]] > failures[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > 3 {
		names = names[:3]
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s failed %d×", name, failures[name]))
	}
	return strings.Join(parts, ", ")
}

func previousRunFailuresMessage(runs []domain.TaskAgentRun, currentRunID uuid.UUID) string {
	for _, prev := range runs {
		if prev.ID == currentRunID || prev.ErrorPattern == "" {
			continue
		}
		if prev.Status != domain.TaskAgentRunStatusFailed {
			continue
		}
		return "## Previous run on this task\n\nIts tools kept failing: " + prev.ErrorPattern +
			".\nDo not open with the same calls. Read the current state of the files first, and if a tool " +
			"rejected that run repeatedly, reach the same goal another way."
	}
	return ""
}

func (r *Runner) failRun(ctx context.Context, run domain.TaskAgentRun, err error) error {
	run.Status = domain.TaskAgentRunStatusFailed
	run.Summary = truncateHead(err.Error(), 500)
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if _, updateErr := r.runs.Update(pctx, run); updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaskAgentRunNotFound) {
			log.Info().Str("run_id", run.ID.String()).Msg("run row gone, task deleted mid-run")
			return nil
		}
		return updateErr
	}
	return err
}

func isUngroundedAnalysis(wf domain.Workflow, task domain.BoardTask, resp domain.AgentResponse, usage *registry.ToolUsage) bool {
	if !wf.TypeHas(domain.BehaviourRequireRepoGrounding) || resp.Clarification != nil || usage == nil {
		return false
	}
	return !usage.UsedAny(domain.CodeExplorationTools...)
}

const ungroundedAnalysisReason = "Analysis rejected: the run never read the repository " +
	"(no codebase_search / grep_code / get_repo_tree / get_symbol_skeleton / expand_symbol_context call succeeded). " +
	"An analiz answer must name real files and interfaces from the code, not assumed ones."

func (r *Runner) failRunUngrounded(ctx context.Context, job RunJob, run domain.TaskAgentRun, resp domain.AgentResponse) error {
	const reason = ungroundedAnalysisReason

	if r.taskUpdater != nil {
		content := reason
		if summary := strings.TrimSpace(resp.Message.Content); summary != "" {
			content += "\n\nRejected draft (not attached as the analysis):\n\n" + summary
		}
		if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    truncateHead(content, 3000),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("ungrounded analysis comment failed")
		}
	}

	log.Warn().Str("task_id", job.Task.ID.String()).Str("run_id", run.ID.String()).
		Msg("analiz run rejected: no repository exploration")

	run.Status = domain.TaskAgentRunStatusFailed
	run.Summary = truncateHead(reason, 500)
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if _, updateErr := r.runs.Update(pctx, run); updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaskAgentRunNotFound) {
			log.Info().Str("run_id", run.ID.String()).Msg("run row gone, task deleted mid-run")
			return nil
		}
		return updateErr
	}
	return ErrUngroundedAnalysis
}

var ErrUngroundedAnalysis = errors.New("analiz run produced no repository exploration")

func isUngroundedQA(wf domain.Workflow, task domain.BoardTask, resp domain.AgentResponse, usage *registry.ToolUsage) bool {
	if resp.Clarification != nil || usage == nil || !wf.Has(task.Column, domain.BehaviourRequireExecutionEvidence) {
		return false
	}
	return !usage.UsedAny(domain.QAExecutionTools...)
}

func (r *Runner) qaSkippedTheUI(ctx context.Context, job RunJob, wf domain.Workflow, usage *registry.ToolUsage) bool {
	if usage == nil || r.projects == nil || !wf.Has(job.Task.Column, domain.BehaviourRequireExecutionEvidence) {
		return false
	}
	if usage.UsedAny(domain.UIObservationTools...) {
		return false
	}
	return r.uiRepo(ctx, job.RepositoryID)
}

func (r *Runner) uiRepo(ctx context.Context, repositoryID uuid.UUID) bool {
	if r.projects == nil {
		return false
	}
	repo, err := r.projects.ResolveRepository(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).
			Msg("UI-evidence check: repository unresolved, skipping the check")
		return false
	}
	return domain.RepoHasUI(repo)
}

const ungroundedQAReason = "QA round rejected: this run never ran the product " +
	"(no run_terminal, browser_* or mobile_* call succeeded). A scenario plan, a summary of the diff, or a criterion " +
	"verdict is not a test — boot the task branch (or the stage target from get_deploy_target) and execute the " +
	"scenarios, then record each criterion with review_criterion citing the command you ran and what you observed."

const noUIEvidenceReason = "QA round rejected: this repository has a user interface and the run never looked at it " +
	"(no browser_screenshot / browser_read_dom / mobile_screenshot / mobile_read_ui call succeeded). Build and test " +
	"commands cannot see what the user sees — a button that renders as a bare \"?\", a section that did not disappear, " +
	"a layout that overflows on a phone all pass every command and fail on screen. Open the changed screens, capture a " +
	"screenshot at desktop and at phone size, read the DOM/UI where a picture is not enough, and cite that evidence on " +
	"each criterion you approve."

func (r *Runner) failRunUngroundedQA(ctx context.Context, job RunJob, run domain.TaskAgentRun, resp domain.AgentResponse, reason string) error {
	if r.taskUpdater != nil {
		content := reason
		if summary := strings.TrimSpace(resp.Message.Content); summary != "" {
			content += "\n\nWhat the rejected run reported (execute this, do not re-plan it):\n\n" + summary
		}
		if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    truncateHead(content, 3000),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("ungrounded QA comment failed")
		}
	}

	log.Warn().Str("task_id", job.Task.ID.String()).Str("run_id", run.ID.String()).
		Str("column", string(job.Task.Column)).Msg("QA run rejected: nothing was executed")

	run.Status = domain.TaskAgentRunStatusFailed
	run.Summary = truncateHead(reason, 500)
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if _, updateErr := r.runs.Update(pctx, run); updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaskAgentRunNotFound) {
			log.Info().Str("run_id", run.ID.String()).Msg("run row gone, task deleted mid-run")
			return nil
		}
		return updateErr
	}
	return ErrUngroundedQA
}

var ErrUngroundedQA = errors.New("QA run executed nothing")

func isUngroundedPMUAT(wf domain.Workflow, task domain.BoardTask, resp domain.AgentResponse, usage *registry.ToolUsage) bool {
	if resp.Clarification != nil || usage == nil {
		return false
	}
	if !wf.Has(task.Column, domain.BehaviourRequireProductCheck) {
		return false
	}
	return !usage.UsedAny(domain.PMUATExecutionTools...)
}

func (r *Runner) pmSkippedCoverageEvidence(ctx context.Context, job RunJob, wf domain.Workflow, run domain.TaskAgentRun, usage *registry.ToolUsage) bool {
	if usage == nil || !wf.Has(job.Task.Column, domain.BehaviourRequireProductCheck) {
		return false
	}
	criteria := r.allCriteria(ctx, job)
	testCases := r.taskTestCases(ctx, job)
	return pmApprovedUncoveredCriterion(wf, job.Task, criteria, testCases, usage, run.CreatedAt)
}

func pmApprovedUncoveredCriterion(
	wf domain.Workflow,
	task domain.BoardTask,
	criteria []domain.AcceptanceCriterion,
	testCases []domain.TaskTestCase,
	usage *registry.ToolUsage,
	runStartedAt time.Time,
) bool {
	if usage == nil || !wf.Has(task.Column, domain.BehaviourRequireProductCheck) {
		return false
	}
	passedByCriterion := make(map[uuid.UUID]bool, len(testCases))
	for _, tc := range testCases {
		if tc.CriterionID != nil && tc.Status == domain.TestCaseStatusPassed {
			passedByCriterion[*tc.CriterionID] = true
		}
	}
	uncovered := false
	for _, c := range criteria {
		if !pmApprovedThisRun(c, runStartedAt) {
			continue
		}
		if !passedByCriterion[c.ID] {
			uncovered = true
			break
		}
	}
	if !uncovered {
		return false
	}
	return !usage.UsedAny(domain.PMUATExecutionTools...)
}

func pmApprovedThisRun(c domain.AcceptanceCriterion, runStartedAt time.Time) bool {
	for _, check := range c.Checks {
		if check.Role == domain.CriterionReviewRolePM && check.Approved && check.CheckedAt.After(runStartedAt) {
			return true
		}
	}
	return false
}

type taskTestCaseReader interface {
	ListTestCases(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error)
}

func (r *Runner) taskTestCases(ctx context.Context, job RunJob) []domain.TaskTestCase {
	reader, ok := r.taskUpdater.(taskTestCaseReader)
	if !ok {
		return nil
	}
	items, err := reader.ListTestCases(ctx, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("list test cases for coverage-gap gate failed")
		return nil
	}
	return items
}

const ungroundedPMUATReason = "pm_uat rejected: this run never checked the product itself " +
	"(no browser_* or mobile_* call succeeded). Approving from QA's notes, the board or the diff is not verification — " +
	"open the changed screens (or run the mobile flow) yourself, then record each criterion with review_criterion " +
	"citing what you observed."

const pmUncoveredCriterionReason = "pm_uat rejected: this run approved a criterion QA's own recorded test round never " +
	"proves (no passed TaskTestCase links to it) without checking it yourself " +
	"(no browser_* or mobile_* call succeeded). Trusting QA's review_criterion note is not enough when nothing in " +
	"list_test_cases actually backs it — walk that criterion's flow yourself before approving it."

func (r *Runner) failRunUngroundedPMUAT(ctx context.Context, job RunJob, run domain.TaskAgentRun, resp domain.AgentResponse, reason string) error {
	if r.taskUpdater != nil {
		content := reason
		if summary := strings.TrimSpace(resp.Message.Content); summary != "" {
			content += "\n\nWhat the rejected run reported (execute this, do not re-approve it):\n\n" + summary
		}
		if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    truncateHead(content, 3000),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("ungrounded pm_uat comment failed")
		}
	}

	log.Warn().Str("task_id", job.Task.ID.String()).Str("run_id", run.ID.String()).
		Str("column", string(job.Task.Column)).Msg("pm_uat run rejected: approval without execution evidence")

	run.Status = domain.TaskAgentRunStatusFailed
	run.Summary = truncateHead(reason, 500)
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if _, updateErr := r.runs.Update(pctx, run); updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaskAgentRunNotFound) {
			log.Info().Str("run_id", run.ID.String()).Msg("run row gone, task deleted mid-run")
			return nil
		}
		return updateErr
	}
	return ErrUngroundedPMUAT
}

var ErrUngroundedPMUAT = errors.New("pm_uat run approved without execution evidence")

func (r *Runner) failRunOutOfBudget(
	ctx context.Context,
	job RunJob,
	run domain.TaskAgentRun,
	workspace, branch string,
	agentRec domain.Agent,
	budgetErr *agent.BudgetExhaustedError,
) error {
	if workspace != "" {
		commitMsg := r.writeCommitMessage(ctx, commitDetails{
			TaskKey:   job.Task.Key,
			Title:     job.Task.Title,
			Summary:   budgetErr.Partial,
			AgentName: agentRec.Name,
			Writer:    agentWriterModel(agentRec),
			Prefix:    "wip: ",
		})
		if pushErr := r.git.CommitAndPush(ctx, workspace, commitMsg); pushErr != nil {
			log.Warn().Err(pushErr).Str("task_id", job.Task.ID.String()).Msg("out-of-budget partial commit failed")
		} else if r.branchIndexer != nil && branch != "" {
			r.branchIndexer.StartIndexBranch(ctx, job.RepositoryID, branch, workspace)
		}
	}

	if r.taskUpdater != nil {
		content := "Run stopped before finishing — " + budgetErr.Error() +
			"\n\nWork completed so far is committed to the task branch; the next run continues from there."
		if budgetErr.Partial != "" {
			content += "\n\nAgent's own summary:\n\n" + budgetErr.Partial
		}
		if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    truncateHead(content, 3000),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("out-of-budget comment failed")
		}
	}

	summary := budgetErr.Error()
	if budgetErr.Partial != "" {
		summary = budgetErr.Partial + " [" + budgetErr.Stats.Summary() + "]"
	}
	run.Status = domain.TaskAgentRunStatusFailed
	run.Summary = truncateHead(summary, 500)
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	if _, updateErr := r.runs.Update(pctx, run); updateErr != nil {
		if errors.Is(updateErr, domain.ErrTaskAgentRunNotFound) {
			log.Info().Str("run_id", run.ID.String()).Msg("run row gone, task deleted mid-run")
			return nil
		}
		return updateErr
	}
	return budgetErr
}

func (r *Runner) taskComments(ctx context.Context, job RunJob) []domain.TaskComment {
	if r.taskUpdater == nil {
		return nil
	}
	comments, err := r.taskUpdater.ListComments(ctx, job.RepositoryID, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("list task comments for run context failed")
		return nil
	}
	return comments
}

func revisionCommentsMessage(comments []domain.TaskComment) string {
	feedback := make([]domain.TaskComment, 0, len(comments))
	for _, c := range comments {
		if !prompt.IsClarificationComment(c.Content) {
			feedback = append(feedback, c)
		}
	}
	if len(feedback) == 0 {
		return ""
	}
	if len(feedback) > 5 {
		feedback = feedback[len(feedback)-5:]
	}
	var sb strings.Builder
	sb.WriteString("## Task comments (the revision feedback is here — act on it, do not ask the human to repeat it)\n")
	for _, c := range feedback {
		content := c.Content
		if len(content) > 2000 {
			content = truncateHead(content, 2000) + "…"
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.AuthorType, content))
	}
	return sb.String()
}

type taskCriteriaReader interface {
	ListTaskCriteria(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error)
}

type analysisReader interface {
	AnalysisReferences(ctx context.Context, taskID uuid.UUID) ([]domain.AnalysisReference, error)
}

const analysisContextLimit = 12000

func (r *Runner) analysisContext(ctx context.Context, job RunJob) string {
	reader, ok := r.taskUpdater.(analysisReader)
	if !ok {
		return ""
	}
	refs, err := reader.AnalysisReferences(ctx, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("analysis reference lookup for run context failed")
		return ""
	}
	if len(refs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## The analysis this task came out of\n")
	sb.WriteString("This task was opened from the analysis below, and these documents are its specification — " +
		"they are attached to that analiz task, NOT committed anywhere in the repository, so this is where the spec and the plan live. " +
		"Implement what they say; where they and the task description disagree, the task description is the narrower scope and wins for THIS task. " +
		"list_task_documents re-reads any of them at any time.\n")
	for _, ref := range refs {
		label := domain.RelationLabel(ref.Key, ref.Title, ref.TaskID)
		if len(ref.Documents) == 0 {
			sb.WriteString(fmt.Sprintf("\n### %s — no documents attached\n"+
				"The analysis this task names has nothing attached to it. Say so in a comment rather than inventing the missing spec.\n", label))
			continue
		}
		sb.WriteString(fmt.Sprintf("\n### %s (analiz task — read it with list_task_documents %s)\n", label, ref.Key))
		for _, doc := range ref.Documents {
			content := doc.Content
			if len(content) > analysisContextLimit {
				content = truncateHead(content, analysisContextLimit) +
					"\n…[truncated — call list_task_documents with task_id " + ref.Key + " to read the whole document]"
			}
			sb.WriteString(fmt.Sprintf("\n#### %s\n%s\n", doc.Title, content))
		}
	}
	return sb.String()
}

func (r *Runner) openCriteria(ctx context.Context, job RunJob) []domain.AcceptanceCriterion {
	items := r.allCriteria(ctx, job)
	open := make([]domain.AcceptanceCriterion, 0, len(items))
	for _, c := range items {
		if !c.Settled() {
			open = append(open, c)
		}
	}
	return open
}

func (r *Runner) allCriteria(ctx context.Context, job RunJob) []domain.AcceptanceCriterion {
	reader, ok := r.taskUpdater.(taskCriteriaReader)
	if !ok {
		return nil
	}
	items, err := reader.ListTaskCriteria(ctx, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("list acceptance criteria for run context failed")
		return nil
	}
	return items
}

func (r *Runner) criteriaForRun(ctx context.Context, job RunJob, wf domain.Workflow) []domain.AcceptanceCriterion {
	if listsEveryCriterion(wf, job.Task) {
		return r.allCriteria(ctx, job)
	}
	return r.openCriteria(ctx, job)
}

func (r *Runner) changedSinceByVerifiedSHA(ctx context.Context, workspacePath string, criteria []domain.AcceptanceCriterion) map[string][]string {
	if r.git == nil || workspacePath == "" {
		return nil
	}
	out := make(map[string][]string)
	for _, c := range criteria {
		for _, check := range c.Checks {
			if check.VerifiedSHA == "" {
				continue
			}
			if _, seen := out[check.VerifiedSHA]; seen {
				continue
			}
			files, err := r.git.ChangedFilesSince(ctx, workspacePath, check.VerifiedSHA)
			if err != nil {
				log.Warn().Err(err).Str("task_id", c.TaskID.String()).Str("sha", check.VerifiedSHA).
					Msg("changed-files-since lookup for criterion review failed")
				continue
			}
			out[check.VerifiedSHA] = files
		}
	}
	return out
}

// A stage that judges work needs the settled criteria in front of it — that
// is what it is being asked to check. Everywhere else gets the open list and
// the instruction to tick them off, which is only actionable while the work
// is still being done. Derived, not a knob: a stage that records criterion
// verdicts is reviewing by definition, whatever its kind says.
func listsEveryCriterion(wf domain.Workflow, task domain.BoardTask) bool {
	return isReviewColumn(wf, task.Column) || wf.Has(task.Column, domain.BehaviourCriterionVerdict)
}

func standingCriteriaMessage(task domain.BoardTask) string {
	if task.TaskType == "analiz" {
		return ""
	}
	switch task.Column {
	case domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision:
	default:
		return ""
	}
	return "\nStanding acceptance criteria — they apply to every code task, they are not written on the card, and they are checked automatically before this task can be handed on:\n" +
		"1. The project builds. A red build is not a finished task, whatever else is done.\n" +
		"2. The whole test suite passes — including the tests you did not write. A test your change broke is your change's problem, not a pre-existing failure to report.\n" +
		"3. New or changed behaviour comes with unit tests. A new function, endpoint, branch or bug fix without a test that would fail without your change is incomplete work; write the test in this run, next to the project's existing tests and in its style. Pure config, copy or asset edits are the exception — say so in your closing comment rather than inventing a test for them.\n" +
		"Ticking the task's own criteria while any of these three is unmet is a false claim: the build gate re-checks all of it after you stop, and a red result sends the task back with your name on it.\n"
}

func reviewCriteriaHeader(column domain.TaskColumn) string {
	const carryOver = " A verdict already on an id survives a revision round — it is not asked again from zero. " +
		"Where a line below names files changed since that verdict, re-verify and call review_criterion again only if one of those files could plausibly affect that specific criterion; leave the rest as they are. " +
		"An id with no verdict yet still needs one.\n"
	switch column {
	case domain.TaskColumnReadyForQA, domain.TaskColumnInQA:
		return "\nAcceptance criteria — record YOUR verdict on EACH id below with review_criterion " +
			"(approve only what you executed and observed; reject with expected-vs-actual). " +
			"The forward move is refused while any id lacks your verdict on repositories that require criteria. " +
			"Never move the task to need_revision just to look for these ids: they are here.\n" + carryOver
	case domain.TaskColumnPMUAT:
		return "\nAcceptance criteria — record YOUR OWN PM verdict on EACH id below with review_criterion " +
			"(approve only what executed evidence and your own check on stage cover; reject naming the gap). " +
			"The developer's checkmark and QA's check are not your verdict. " +
			"The forward move is refused while any id lacks your verdict on repositories that require criteria. " +
			"Never move the task to need_revision just to look for these ids: they are here.\n" + carryOver
	case domain.TaskColumnCodeReview:
		return "\nAcceptance criteria the diff must satisfy (ids for reference):\n"
	default:
		return "\nAcceptance criteria of this task (ids for reference):\n"
	}
}

func criterionStateLine(c domain.AcceptanceCriterion, changedSince map[string][]string) string {
	implementer := "not ticked"
	if c.Completed {
		implementer = "ticked"
	}
	return fmt.Sprintf("- [%s] %s — implementer: %s; qa: %s; pm: %s\n",
		c.ID, c.Text, implementer,
		criterionVerdictLabel(c, domain.CriterionReviewRoleQA, changedSince),
		criterionVerdictLabel(c, domain.CriterionReviewRolePM, changedSince))
}

func criterionVerdictLabel(c domain.AcceptanceCriterion, role domain.CriterionReviewRole, changedSince map[string][]string) string {
	for i := range c.Checks {
		check := c.Checks[i]
		if check.Role != role {
			continue
		}
		if check.Approved {
			return "approved" + changedSinceNote(check.VerifiedSHA, changedSince)
		}
		if note := strings.TrimSpace(check.Note); note != "" {
			return "rejected (" + note + ")"
		}
		return "rejected"
	}
	return "—"
}

const maxChangedFilesNoted = 15

func changedSinceNote(sha string, changedSince map[string][]string) string {
	if sha == "" {
		return ""
	}
	files, ok := changedSince[sha]
	if !ok {
		return ""
	}
	if len(files) == 0 {
		return fmt.Sprintf(" (as of %s, nothing changed since)", domain.ShortSHA(sha))
	}
	shown, suffix := files, ""
	if len(shown) > maxChangedFilesNoted {
		shown = shown[:maxChangedFilesNoted]
		suffix = fmt.Sprintf(" +%d more", len(files)-maxChangedFilesNoted)
	}
	return fmt.Sprintf(" (as of %s, changed since: %s%s)", domain.ShortSHA(sha), strings.Join(shown, ", "), suffix)
}

func criteriaMessage(wf domain.Workflow, task domain.BoardTask, criteria []domain.AcceptanceCriterion, changedSince map[string][]string) string {
	var sb strings.Builder
	sb.WriteString(standingCriteriaMessage(task))
	if len(criteria) == 0 {
		return sb.String()
	}
	if listsEveryCriterion(wf, task) {
		sb.WriteString(reviewCriteriaHeader(task.Column))
		for _, c := range criteria {
			sb.WriteString(criterionStateLine(c, changedSince))
		}
		return sb.String()
	}
	sb.WriteString("\nOpen acceptance criteria (these define \"done\" for this task):\n")
	for _, c := range criteria {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.ID, c.Text))
	}
	if task.TaskType == "analiz" {
		switch task.Column {
		case domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision:
			sb.WriteString("These are what your spec and plan must answer. Tick each one your documents cover with set_criterion_completed, " +
				"in the same step that covered it. Never tick one the documents do not answer — say so in your summary comment instead.\n")
		}
		return sb.String()
	}
	switch task.Column {
	case domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision:
		sb.WriteString("Each one you satisfy in this run: call set_criterion_completed with its id, in the same step that did the work. " +
			"The automatic hand-off to code_review is REFUSED while any criterion is still open, so an unticked criterion leaves your finished work parked in this column. " +
			"Never tick a criterion you did not implement — if one is out of scope or blocked, say so in a comment instead.\n")
	}
	return sb.String()
}

func buildTriggerMessage(job RunJob, wf domain.Workflow, criteria []domain.AcceptanceCriterion, changedSince map[string][]string) string {
	taskJSON, _ := json.Marshal(map[string]interface{}{
		"task_id":       job.Task.ID.String(),
		"task_key":      job.Task.Key,
		"title":         job.Task.Title,
		"repository_id": job.RepositoryID.String(),
		"task_type":     string(job.Task.TaskType),
		"description":   job.Task.Description,
		"column":        string(job.Task.Column),
		"event_type":    string(job.Event.EventType),
		"payload":       json.RawMessage(job.Event.Payload),
	})
	runIns := runInstruction(wf, job)
	if job.ColumnInstruction != "" {
		runIns += "\n\nColumn-specific instructions for you, for a task that arrives in this column:\n" + job.ColumnInstruction
	}
	return fmt.Sprintf(`A kanban board event occurred. Evaluate the task and take action using board tools when appropriate.

%s

Board bookkeeping is not work and is never a step of its own:
- Claiming a task and moving it between columns takes seconds and announces what you are doing. It produces nothing. Do it inside the step that does the work, not as a separate step, and never as the first item of a plan.
- The task is ALREADY in the column named below. Never move it to the column it is already in — that move is a no-op the board rejects, and planning it wastes a whole step.
- The hand-off at the end is the system's: an implementation run that finishes with a green build and a real diff is moved to code_review for you. A step whose only content is "move the task to code_review" is rejected before the plan runs.
- A run whose entire output is a claim and a move has done nothing and is recorded as incomplete.
- Read a file once. Repeating the same read, grep or build to re-confirm something already in your context is the single most common way a run burns its budget without producing a change. If two passes over the code told you the same thing, the answer is not in another pass — make the edit.

Event rules:
- For need_revision: the reviewer's feedback is in the task comments provided in your context. Fix the work accordingly in this run; the hand-off back to code_review is automatic. Do not ask the human to repeat feedback that is already in the comments.
- If the payload has resumed=question_answered: you previously stopped on the question in payload.question and the human replied in payload.answer. Continue the work from where you stopped using that answer; do not ask it again.
- Never move your own task to need_revision or back to todo — need_revision is how REVIEWERS hand work back to you. If you are missing information, use ask_user; the system parks the task as blocked until the human answers.
- Only claim tasks that are unassigned or already assigned to you.
- Use the exact tool names available to you (claim_board_task, move_board_task, add_task_comment, etc.). Do not invent tool names.
- Every board tool call in this run is about THIS task: pass the task_id or task_key from the snapshot below verbatim. The keys in the tool descriptions ("T-1", "B-1", "A-1") are format examples, never the task you are working on.
- Tools that take a repository_id (get_deploy_target, update_deploy_target, list_incidents, create_board_task) want the repository_id UUID from the snapshot below — never the repository name. Tools without that field — list_board_tasks among them — are already scoped to this run's repository; passing one an extra field is a schema error.

Before you finish, in this order — these are calls, not prose in your summary:
1. Build and test what you changed with run_terminal, and read the output. Red output is fixed in this run, not reported as done. Call list_component_checks and run the local command of every required check for the components this diff touches — passing them locally is what this step means.
2. Every acceptance criterion you satisfied: set_criterion_completed with its id — ticked only after step 1 showed it working.
3. Every criterion you did NOT satisfy: leave it open and say why in a comment.
%s
A run that skips any of 1-3 has its hand-off refused and its finished work parked in this column.

Task snapshot:
%s
%s`, runIns, closingStep(wf, job), string(taskJSON), criteriaMessage(wf, job.Task, criteria, changedSince))
}

func closingStep(wf domain.Workflow, job RunJob) string {
	if !wf.Has(job.Task.Column, domain.BehaviourBuildVerify) {
		return "4. One add_task_comment with what you changed and the command output that verified it."
	}
	return "4. Do NOT post an add_task_comment announcing completion. Close with your final message instead — what you changed and the command output that verified it. " +
		"The system re-runs build verification after you stop and publishes that summary to the card only once the gate passes; " +
		"a completion comment posted from inside the run can claim success the gate is about to refute."
}

func runInstruction(wf domain.Workflow, job RunJob) string {
	task := job.Task
	if job.EnteredFrom == domain.TaskColumnNeedRevision {
		task.Column = domain.TaskColumnNeedRevision
	}
	return columnInstruction(wf, task)
}

const verifyBeforeFinishing = "Before you finish, RUN the code you wrote: build it and run the tests with run_terminal " +
	"(call list_component_checks or get_project_brief for the project's own commands when those tools are available; " +
	"otherwise check package.json / Makefile / go.mod / the README) and READ the output. " +
	"Writing a file is not verifying it and neither is reading it back; \"it should work\" is not a result. " +
	"A red build or a failing test is yours to fix in this same run — never hand off red work. " +
	"If the change cannot be executed here (missing service, no credentials), say exactly that in your closing comment " +
	"and name what you did check instead. A run whose ledger holds no executed command is treated as unverified and is not handed on."

const handoffIsAutomatic = verifyBeforeFinishing +
	" Do NOT move the task yourself and never plan a step for the move: when this run ends with a green build and a real diff on the branch, " +
	"the system moves the task to code_review, opens the pull request and starts the pipeline. " +
	"That move is REFUSED while an acceptance criterion is still open, so tick each one you satisfy with set_criterion_completed inside the step that satisfied it " +
	"(list_acceptance_criteria gives you the ids if they are not in your context). " +
	"Your run is finished when the work is done and verified — close it with a final MESSAGE saying what you changed and how you verified it. " +
	"That message is your report to the system, NOT a card comment: a run that went green writes nothing on the task, because the diff, the pull request, the pipeline result and the ticked criteria already say it. " +
	"Use add_task_comment only for something the next person has to act on — a question you could not answer yourself, a part of the task you did not do and why, a risk or a follow-up somebody must pick up."

const qaExecutionInstruction = "Test it as a black box, on a RUNNING product. " +
	"Start by resolving the environment, before anything else: call get_deploy_target — if it returns a stage " +
	"base_url, that is where you test (the deploy for this task already ran on entry to ready_for_qa; verify the " +
	"target answers, and record the address with update_deploy_target if it is missing). If there is no stage " +
	"target, boot the task branch yourself with run_terminal (install, then the project's dev/start command in the " +
	"background) and test on 127.0.0.1. Other tasks boot their own copies on this machine at the same time, so " +
	"the project's default port may already be another task's build: start yours on a free port you pick, open the " +
	"address YOUR process printed, and when you are done stop exactly the PID you started — pkill/killall by name " +
	"is refused, because it takes down every other task's servers too. Then walk the scenarios with the browser tools (browser_navigate → " +
	"browser_wait_for → browser_fill/browser_click, browser_screenshot as evidence, browser_set_viewport for the " +
	"phone width) or the mobile_* tools for a device app. Never test against production. " +
	"Reading source is NOT testing: read_file/grep_code/get_repo_tree are there to find the start command, the " +
	"port or the route you have to open — a verdict whose evidence is the code rather than an executed run is " +
	"rejected and the round is failed. For a bound environment's own live logs or grouped errors, use " +
	"query_runtime_logs / list_runtime_errors instead of get_deploy_logs, which stays for a CI job's output."

func columnInstruction(wf domain.Workflow, task domain.BoardTask) string {
	if stage, ok := wf.Stage(task.Column); ok && stage.Instructions != "" {
		return stage.Instructions
	}
	// passTo from the workflow, not a literal: pm_uat for everything except technical, which goes straight to human_uat.
	passTo := "pm_uat"
	if target, ok := wf.Param(task.Column, domain.BehaviourReviewVerdictSweep, "pass_to"); ok && target != "" {
		passTo = target
	}
	switch task.Column {
	case domain.TaskColumnTodo:
		return "This task is in `todo`. If it is not relevant to your role, take no action. " +
			"If it is: claim it, move it to in_progress as the opening action of the step that does the work (never a step of its own), " +
			"and implement the work IN THIS SAME RUN. " + handoffIsAutomatic
	case domain.TaskColumnInProgress:
		return "This task is ALREADY claimed and ALREADY in `in_progress` — the move you might be tempted to plan first has happened. " +
			"Continue the implementation from where it stands (the task branch and its diff are in your context) and finish it in this run. " +
			handoffIsAutomatic
	case domain.TaskColumnNeedRevision:
		return "This task came back from review. The feedback is in your context: the task comments, and — when the review happened on a pull request — " +
			"the PR review comments. Read BOTH before you touch the code (list_task_comments and get_task_pull_request re-read them at any time), " +
			"and apply every point in this run. " + handoffIsAutomatic
	case domain.TaskColumnCodeReview:
		return "This task is in `code_review`: a developer finished it and you are the reviewer. " +
			"The pull request and its complete diff are in your context — READ the diff and review those changes. " +
			"Do not run the app, do not run builds or tests, and do not fix anything yourself: the build/test pipeline " +
			"already ran on entry (get_pipeline_status is its result) and fixes are the developer's to make. " +
			"Judge three things: (1) do the changes deliver what the task and its acceptance criteria asked for, " +
			"(2) is the code itself sound (correctness, layer boundaries, error handling, security, tests), " +
			"(3) does the change break anything elsewhere in the domain — for that, read the surrounding code the diff " +
			"touches (grep_code, expand_symbol_context, codebase_search) as much as you need. " +
			"Three things hold for every change whether or not the card names them, and a diff that misses one is a finding: " +
			"the project builds, the whole suite passes, and new or changed behaviour carries a unit test that would fail without the change " +
			"(pure config, copy or asset edits excepted). A diff that adds a function, an endpoint or a branch with no test beside it is Important, not a nit. " +
			"Finish with a verdict: clean and pipeline green → ready_for_qa, and write NO comment — an approval that says \"looks good\" is noise on the card; any Critical/Important finding or a red " +
			"pipeline → need_revision with a numbered comment citing file:line."
	case domain.TaskColumnReadyForQA:
		// Only reachable when the automatic ready_for_qa -> in_qa move was refused; in_qa is where testing is evidenced.
		return "This task is still in `ready_for_qa`: the automatic move into in_qa did not go through, " +
			"so testing has NOT started and the board does not show this task as under test. " +
			"Move it to in_qa yourself as the opening action of your first testing step (not as a step of its own), " +
			"then run the tests in this same run. " + qaExecutionInstruction +
			" Record your own verdict on each acceptance criterion with " +
			"review_criterion as you verify it — approve only what you executed, reject with a note saying what failed. " +
			"Finish from in_qa: every acceptance criterion passes → " +
			"move it to " + passTo + ", with the evidence in the review_criterion notes and NO comment on the card — a pass writes nothing; any criterion fails → move it to need_revision " +
			"and comment the numbered expected-vs-actual per failure."
	case domain.TaskColumnInQA:
		return "This task is ALREADY in `in_qa` — testing is under way and the move you might plan first has happened. " +
			qaExecutionInstruction +
			" Continue and finish the scenarios in this run, recording your verdict per acceptance criterion with " +
			"review_criterion (approve what you executed and observed; reject with an expected-vs-actual note), then " +
			"leave the column: all criteria pass → " + passTo + ", evidence in the criterion notes and no comment on the card (a pass is not news); " +
			"any failure → need_revision with a comment giving expected-vs-actual per failure. Never leave a task parked in in_qa."
	case domain.TaskColumnPMUAT:
		return "This task is in `pm_uat`: acceptance control. Compare the original request and every acceptance criterion " +
			"against QA's executed evidence — which lives in the review_criterion note of each criterion, not in a comment (a QA round that passed writes none) — AND verify the critical flows yourself on the stage " +
			"environment with the browser tools (get_deploy_target resolves the stage base_url; browser_navigate → " +
			"browser_wait_for → browser_fill/browser_click, browser_screenshot as evidence, browser_set_viewport to walk the " +
			"same flow on a phone — never against production). " +
			"Record YOUR verdict per criterion with review_criterion — " +
			"the developer's checkmark and QA's check are not yours. Approve a criterion only when executed evidence covers it; " +
			"reject with a note naming the gap. All approved → move to human_uat and write no comment: the move and the approved criteria are the verdict; " +
			"any gap → move to need_revision with a numbered gap-list comment. Never approve by reading code — reading source is not verification."
	case domain.TaskColumnDone:
		// Reachable only through the merge wake or a release hand-back: a done card with an unmerged PR, or a
		// release the sweeper just settled, dispatched to the release engineer and nobody else.
		// done must never read the default: its "move on to the next column" sentence is how done tasks drifted into released with no deploy.
		return "This task is in `done`: the board has signed it off and its work is finished. " +
			"You are here to LAND the change, ship it, and verify production — nothing else. " +
			"1) If the pull request is not merged yet: read it (get_task_pull_request) and the pipeline result (get_pipeline_status) — the checks must be green and " +
			"the PR must still be at the commit that was verified — then call merge_task_pull_request, which squash-merges it, deletes the task branch, and opens (or joins) a release. " +
			"Do NOT retry a refusal and do NOT work around it, and do not comment that the merge worked when it did: the merge commit is recorded on the card by the tool itself. " +
			"A refusal that names a CONFLICT with the base branch (`dirty`) or a branch the base has moved past (`behind`) is the developer's to fix, not yours: " +
			"move the task to need_revision with that reason and stop. Any other refusal (a closed PR, a head commit that is not the verified one, an incomplete review chain) " +
			"means the change is not the change that was approved: put the reason on the task with add_task_comment and stop, because only a human or a new round of review can settle it. " +
			"2) Read the merge result's `release` field for what happens next: mode `none` (or `unconfirmed: true`) → nothing to do, stop. A merge refused because the delivery profile is unconfirmed, a deploy dependency is not released, or before-deploy steps are unconfirmed is already commented on the card — stop, you are woken when it clears. " +
			"Mode `batch` → the merge joined the component's draft release; nothing to do, stop — a human cuts it later on the Deploy tab. " +
			"Mode `on_merge` → call watch_release. Mode `dispatch` → call deploy_release, then watch_release. " +
			"watch_release parks this task while a system sweeper watches the deploy and the post-deploy soak window — do not poll or wait, you are woken when there is something to decide. " +
			"3) When woken with `pending` and mode `batch`: a human just cut this release — call deploy_release (it creates the tag, runs the local command, or starts the store build, per the component's executor), then watch_release. " +
			"4) When woken with `awaiting_verdict`: get_release, then read query_runtime_logs (since deployed_at) and list_runtime_errors WHEREVER the component has a bound runtime environment — a release is never finished on a green deploy alone. " +
			"A batch release with no bound runtime environment (most desktop/mobile components) has no logs to read: its evidence is the build/publish result (the workflow run, local_run, or store_builds) plus any smoke checks — say explicitly in the finish note that no runtime environment is bound rather than treating the gap as a pass. " +
			"Clean evidence → finish_release with a note stating what you checked. A failed smoke check, a failing health sample, a failed build/publish, or new runtime error groups tied to the change → rollback_release " +
			"(reason and a note stating the evidence), then report every step under `rollback.manual_steps` and call watch_release again to follow the redeploy. " +
			"For a batch release, rollback_release only reverts the default branch — nothing is redeployed, because a published desktop build or a store build cannot be unpublished by a revert; `rollback.manual_steps` leads with unpublishing or halting that artifact, and you must perform or report that step first. " +
			"When woken with `failed`: get_release; for a batch local run read local_run.tail (and its log path), for github_actions call get_deploy_logs if a job failed; then rollback_release (reason deploy_failed) if the bad code is live or on the default branch; otherwise report what failed and stop. " +
			"Do not test anything here (that happened in in_qa), do not edit or commit code, and do NOT move this task to `released` yourself: " +
			"only finish_release does that, and moving the card there by hand would announce a release that was never verified."
	case domain.TaskColumnReleased:
		// Reachable only through a release hand-back for a health incident inside the release's window: released
		// dispatches the release engineer for nothing else.
		// released has no next column: the generic default must never fire here, or a card is handed to nobody.
		return "This task is in `released`: its change is in production. You have been woken for a health incident inside this release's window and for nothing else. " +
			"get_release for the release this task belongs to, then query_runtime_logs and list_runtime_errors since deployed_at, then get_incident or list_incidents for what actually opened. " +
			"If the incident is genuinely this release's doing — new error groups or a health failure tied to the change, inside the window — call rollback_release with reason health_incident and a note stating the evidence, " +
			"then report EVERY step it returns under `rollback.manual_steps` — a migration, a feature flag, anything with a human on the other end — and call watch_release to follow the redeploy. " +
			"A rollback reported as complete when half of it was not is worse than one that says what it could not do. " +
			"If rollback_release returns `proposed: true`, auto_rollback is off for this component: post the proposal, say a human must confirm it, and stop. " +
			"If the incident predates this release or is unrelated to what it changed, say so in one comment and leave the release alone. " +
			"Do not edit or commit code, do not move this task anywhere, and do not start any other work here."
	default:
		return fmt.Sprintf("This task is in `%s`. Do the work that column asks of your role in this run, then move the task on to the next column. "+
			"It is already in that column, so do not plan a move into it.", task.Column)
	}
}

func prependProjectContext(history []domain.Message, desc, toolsNote string) []domain.Message {
	var note string
	if desc != "" {
		note = "Project context: " + desc
	}
	if toolsNote != "" {
		if note != "" {
			note += "\n\n"
		}
		note += toolsNote
	}
	if note == "" {
		return history
	}
	return append([]domain.Message{{Role: domain.RoleSystem, Content: note}}, history...)
}

func sessionIDPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}

func cliFlavor(t domain.LLMProviderType) (agentfs.Flavor, bool) {
	switch t {
	case domain.LLMProviderClaudeCode:
		return agentfs.FlavorClaude, true
	case domain.LLMProviderAntigravity:
		return agentfs.FlavorAntigravity, true
	case domain.LLMProviderCursorAgent:
		return agentfs.FlavorCursor, true
	case domain.LLMProviderOpencode:
		return agentfs.FlavorOpencode, true
	default:
		return "", false
	}
}
