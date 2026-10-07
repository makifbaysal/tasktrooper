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
	"github.com/makifbaysal/tasktrooper/server/internal/application/htmldoc"
	"github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/toolchain"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
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
	bgMu              sync.Mutex
	bgCtx             context.Context
	bgCancel          context.CancelFunc
	bgStopped         bool
	bgWG              sync.WaitGroup
}

const runHeartbeat = 10 * time.Second

const runLiveWithin = 6 * runHeartbeat

const persistTimeout = 15 * time.Second

func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

type RunnerDeps struct {
	AgentLoop           agent.Runner
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
	r.startBackground(ctx)
	r.wg.Add(1)
	go r.dispatch(ctx)
}

func (r *Runner) Stop() {
	r.closeDrain()
	r.stopBackground()
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

func (r *Runner) Drain(ctx context.Context) {
	r.closeDrain()
	r.stopBackground()
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

	scope := "run:" + run.ID.String()
	ctx = proctree.WithScope(ctx, scope)
	defer proctree.Default.KillScope(scope, 3*time.Second)

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
		if wsErr := r.prepareTaskWorkspace(ctx, job.Task, rootPath, wsPath, branch); wsErr != nil {
			return fail(fmt.Errorf("task workspace could not be prepared (repo clone/branch creation failed), agent was not started: %w", wsErr))
		}
		workDir = wsPath
		taskWorkspace = wsPath
		taskBranch = branch
		run.WorkspacePath = wsPath
		_, _ = r.runs.Update(ctx, run)
	}

	if r.skipUnchangedDiffGate(ctx, job, run, taskWorkspace) {
		return nil
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
	runCtx = registry.ContextWithTaskType(runCtx, string(job.Task.TaskType))
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

	var prMsg string
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
	blocks := r.gatherRunContext(ctx, runCtx, job, wf, agentRec.ID, repoRec.Name, taskWorkspace)
	scoreMsg, kpiMsg, memMsg, diffMsg := blocks.score, blocks.kpi, blocks.memory, blocks.diff
	pipelineMsg, analysisMsg, questionsMsg := blocks.pipeline, blocks.analysis, blocks.questions
	var revisionMsg, reviewMsg, prevFailuresMsg string
	if job.isRevision() {
		prMsg = blocks.revisionPR
		revisionMsg = revisionCommentsMessage(blocks.comments)
		reviewMsg = blocks.review
	}
	humanMsg := humanRequirementsMessage(blocks.comments)
	clarificationsMsg := prompt.AnsweredClarificationsMessage(blocks.comments)
	resumeCLISession, resumePrompt := "", ""
	var prevRuns []domain.TaskAgentRun
	if blocks.prevRunsErr != nil {
		log.Warn().Err(blocks.prevRunsErr).Str("task_id", job.Task.ID.String()).Msg("previous run lookup for failure context failed")
	} else {
		prevRuns = blocks.prevRuns[:min(len(blocks.prevRuns), quotaParkHistoryDepth)]
		prevFailuresMsg = previousRunFailuresMessage(prevRuns, run.ID)
		resumeCLISession = latestCLISession(prevRuns, run.ID, job.Run.AgentID, agentRec.ProviderType)
		if resumeCLISession == "" && job.isRevision() && agentRec.ProviderType == domain.LLMProviderClaudeCode {
			resumeCLISession = revisionCLISession(blocks.prevRuns, run.ID, job.Run.AgentID, workDir)
		}
		// Every resumed revision is handed the feedback, a quota-park resume
		// included: the park can land before the CLI ever started, leaving the
		// parked session the one from before the task was sent back.
		if resumeCLISession != "" && job.isRevision() {
			resumePrompt = revisionResumePrompt(job.Task, triggerMsg,
				humanMsg, prMsg, pipelineMsg, revisionMsg, reviewMsg, clarificationsMsg, prevFailuresMsg)
			log.Info().Str("task_id", job.Task.ID.String()).Str("cli_session_id", resumeCLISession).
				Msg("revision run resumes the agent's previous cli session with the review feedback")
		}
	}
	projectDesc := repoRec.Description

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
	if humanMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: humanMsg})
	}
	if analysisMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: analysisMsg})
	}
	if questionsMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: questionsMsg})
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
	if reviewMsg != "" {
		history = append(history, domain.Message{Role: domain.RoleSystem, Content: reviewMsg})
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
	runCtx = withRunRepository(runCtx, repoRec)
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
		questions := &domain.ClarificationSink{}
		resp, err = r.executor.Execute(domain.WithClarificationSink(runCtx, questions), domain.TaskExecution{
			History:         history,
			Model:           model,
			Provider:        agentRec.ProviderType,
			MaxTurns:        agentRec.MaxTurns,
			Effort:          agentRec.Effort,
			Policy:          cliAskPolicy(upliftedPolicy, job.Task.TaskType),
			WorkDir:         workDir,
			Env:             sessionEnv,
			ResumeSessionID: resumeCLISession,
			Prompt:          resumePrompt,
			TaskKey:         job.Task.Key,
			TaskTitle:       job.Task.Title,
			SkillsOnDisk:    skillDelivery == prompt.SkillsOnDisk,
		})
		if resp.Clarification == nil {
			resp.Clarification = questions.Request()
		}
		cliSession.Set(resp.CLISessionID)
		stampCLISession(&run, cliSession.ID(), agentRec.ProviderType)
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
	sendBackReason := ""
	var advisory advisoryChecks
	if r.verifyEnabled && taskWorkspace != "" && resp.Clarification == nil && resp.ResourceBlock == nil &&
		wf.Has(job.Task.Column, domain.BehaviourBuildVerify) {
		var quotaBlock *domain.QuotaBlock
		resp, buildVerified, quotaBlock, advisory = r.verifyAndFix(runCtx, job, agentRec, history, resp, model, upliftedPolicy, taskWorkspace)
		if quotaBlock != nil {
			stampToolStats(&run, toolUsage)
			stampTokenUsage(&run, tokenUsage)
			return r.quotaOrPark(ctx, job, run, agentRec, prevRuns, cliSession, quotaBlock, fail)
		}
		if !buildVerified {
			sendBackReason = domain.MoveReasonVerificationFailed
		}
	}
	if resp.Verification != nil && !resp.Verification.Passed {
		buildVerified = false
		if sendBackReason == "" {
			sendBackReason = domain.MoveReasonPlanVerificationFailed
		}
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
		pushRefused := false
		if job.Task.TaskType.PublishesBranch() {
			commitMsg := r.writeCommitMessage(ctx, commitDetails{
				TaskKey:   job.Task.Key,
				Title:     job.Task.Title,
				Summary:   run.Summary,
				AgentName: agentRec.Name,
				Writer:    agentWriterModel(agentRec),
			})
			if pushErr := r.git.CommitAndPush(ctx, taskWorkspace, commitMsg); pushErr != nil {
				log.Warn().Err(pushErr).Str("task_id", job.Task.ID.String()).Msg("task workspace commit/push failed")
				// The pull request does not carry this change, so handing it to
				// review would approve something other than what was written.
				if errors.Is(pushErr, domain.ErrGitHubWorkflowScope) {
					pushRefused = true
					r.parkOnWorkflowScope(ctx, job)
				}
			} else {
				advisory.commit = r.handoffCommit(ctx, advisory)
				if r.branchIndexer != nil && taskBranch != "" {
					r.branchIndexer.StartIndexBranch(ctx, job.RepositoryID, taskBranch, taskWorkspace)
				}
			}
		}
		if pushRefused {
			log.Info().Str("task_id", job.Task.ID.String()).Msg("hand-off: push refused for a workflow file, task parked for a token")
		} else if buildVerified {
			if r.advanceToCodeReview(ctx, job, wf, taskWorkspace, toolUsage) {
				r.startAdvisoryChecks(job, advisory)
			}
			if !r.blockOnPendingQuestions(ctx, job) {
				r.advanceToAnalizReview(ctx, job, wf, toolUsage)
			}
		} else if r.sendBackForRevision(ctx, job, wf, "", sendBackReason) {
			log.Info().Str("task_id", job.Task.ID.String()).Str("reason", sendBackReason).
				Msg("hand-off: verification failed, task sent back for revision")
		} else {
			log.Info().Str("task_id", job.Task.ID.String()).Str("reason", sendBackReason).
				Msg("hand-off: verification failed, task stays in the working column")
		}
	}
	stampCLISession(&run, cliSession.ID(), agentRec.ProviderType)
	pctx, cancelPersist := persistCtx(ctx)
	defer cancelPersist()
	_, err = r.runs.Update(pctx, run)
	return err
}

// parkOnWorkflowScope parks a task whose push GitHub refused for want of the
// workflow scope: no agent run can fix a token, so it waits on a human.
func (r *Runner) parkOnWorkflowScope(ctx context.Context, job RunJob) {
	r.commentSystem(ctx, job, prompt.Text(pushWorkflowScopeCommentKey))
	if r.blocker == nil {
		return
	}
	previous, err := r.blocker.BlockOnResource(ctx, job.RepositoryID, job.Task.ID,
		domain.ResourceHumanDecision, prompt.Text(pushWorkflowScopeDetailKey))
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("parking a task whose push lacked the workflow scope failed")
		return
	}
	r.parks.Record(ctx, job.RepositoryID, job.Task, previous, domain.ResourceHumanDecision, domain.MoveReasonPushWorkflowScope)
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
	run.CLISessionID, run.CLIProvider = "", ""
	provider := block.Provider
	if provider == "" {
		provider = agentRec.ProviderType
	}
	stampCLISession(&run, block.CLISessionID, provider)
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

// latestCLISession is the session a quota-parked run left to resume. A park
// recorded before cli_provider existed carries no provider and is trusted, as
// it always was; one recorded under another provider is not this CLI's.
func latestCLISession(prevRuns []domain.TaskAgentRun, currentRunID, agentID uuid.UUID, provider domain.LLMProviderType) string {
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
		if prev.CLIProvider != "" && prev.CLIProvider != provider {
			return ""
		}
		return prev.CLISessionID
	}
	return ""
}

func stampCLISession(run *domain.TaskAgentRun, sessionID string, provider domain.LLMProviderType) {
	if sessionID == "" {
		return
	}
	run.CLISessionID = sessionID
	run.CLIProvider = provider
}

// cliAskPolicy grants an agent CLI's board run ask_user the way the
// orchestrator grants an API run's subtasks: everywhere but analiz, which asks
// through record_open_questions. An empty allow list already allows it.
func cliAskPolicy(p domain.ToolPolicy, taskType domain.TaskType) domain.ToolPolicy {
	if taskType == domain.TaskTypeAnaliz || len(p.AllowTools) == 0 {
		return p
	}
	return domain.EnsureAskUserTool(p)
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

// prepareTaskWorkspace refreshes the task's workspace, except that a merged
// task in done or released runs on the workspace as it is when the refresh
// fails. Its branch already landed (squash-merged and usually deleted), so a
// refresh can only conflict, and the release engineer reads the pull request
// and the release over the API rather than the tree: refusing to start it
// left the release without a verdict and the task in done, re-woken until
// the watchdog gave up (T-64).
func (r *Runner) prepareTaskWorkspace(ctx context.Context, task domain.BoardTask, rootPath, wsPath, branch string) error {
	err := r.git.EnsureTaskWorkspace(ctx, rootPath, wsPath, branch)
	if err == nil {
		return nil
	}
	shipped := task.Column == domain.TaskColumnDone || task.Column == domain.TaskColumnReleased
	if !shipped || strings.TrimSpace(task.MergeCommitSHA) == "" || !r.git.HasGit(wsPath) {
		return err
	}
	log.Warn().Err(err).Str("task_id", task.ID.String()).Str("workspace", wsPath).
		Msg("task workspace: refreshing a merged task's workspace failed, running on it as it is")
	return nil
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

func (r *Runner) advanceToCodeReview(ctx context.Context, job RunJob, wf domain.Workflow, taskWorkspace string, usage *registry.ToolUsage) bool {
	if r.taskUpdater == nil || r.git == nil || taskWorkspace == "" {
		return false
	}
	target, ok := wf.Param(job.Task.Column, domain.BehaviourAdvanceOnDiff, "to")
	if !ok || target == "" {
		return false
	}

	diff, diffErr := r.git.TaskDiff(ctx, taskWorkspace)
	if diffErr != nil {
		log.Warn().Err(diffErr).Str("task_id", job.Task.ID.String()).Msg("hand-off: task diff unreadable, leaving the column to the agent")
		return false
	}
	if strings.TrimSpace(diff) == "" {
		log.Info().Str("task_id", job.Task.ID.String()).Msg("hand-off: run produced no diff, task stays where it is")
		return false
	}

	if reader, ok := r.taskUpdater.(taskColumnReader); ok {
		if fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("hand-off: task re-read failed, using the run's snapshot")
		} else if fresh.Column != job.Task.Column {
			log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(fresh.Column)).
				Msg("hand-off: task already left the column during the run")
			return false
		}
	}

	if usage != nil && !usage.UsedAny(domain.ImplementationVerificationTools...) && !r.ciConfigOnlyDiff(ctx, taskWorkspace) {
		// A run whose every command failed (a red test, a blocked pkill) did
		// execute something; telling it "you ran nothing" sends the next run
		// after the wrong problem.
		if failed := failedVerificationCalls(usage); failed > 0 {
			log.Warn().Str("task_id", job.Task.ID.String()).Int("failed", failed).
				Msg("hand-off: run wrote a diff but none of its commands succeeded, sending the task back for revision")
			r.sendBackForRevision(ctx, job, wf,
				handoffFailedCommandsKey.Render(handoffFailedCommandsInput{Failed: failed}), domain.MoveReasonHandoffUnverified)
			return false
		}
		log.Warn().Str("task_id", job.Task.ID.String()).
			Msg("hand-off: run wrote a diff but never executed a command, sending the task back for revision")
		r.refuseHandoff(ctx, job, wf, handoffUnverifiedRunKey, domain.MoveReasonHandoffUnverified)
		return false
	}

	if usage != nil && r.uiRepo(ctx, job.RepositoryID) && !usage.UsedAny(domain.UIObservationTools...) {
		needsUI := true
		if files, filesErr := r.git.TaskChangedFiles(ctx, taskWorkspace); filesErr == nil {
			needsUI = domain.DiffNeedsUIEvidence(files)
		}
		if needsUI {
			log.Warn().Str("task_id", job.Task.ID.String()).
				Msg("hand-off: UI change never observed, sending the task back for revision")
			r.refuseHandoff(ctx, job, wf, handoffUnseenUIKey, domain.MoveReasonHandoffUnseenUI)
			return false
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
		r.handleRefusedAdvance(ctx, job, wf, err, handoffCodeReviewRefusedKey, handoffCodeReviewRefusedRevisionKey)
		return false
	}
	log.Info().Str("task_id", job.Task.ID.String()).Str("agent_id", agentID.String()).
		Msg("hand-off: implementation run finished with a diff, task moved to code_review")
	return true
}

// ciConfigOnlyDiff reports a diff that only touches CI configuration: there
// is nothing to build locally, and the pipeline its pull request triggers is
// the check that counts — the code_review stage waits for it.
func (r *Runner) ciConfigOnlyDiff(ctx context.Context, taskWorkspace string) bool {
	files, err := r.git.TaskChangedFiles(ctx, taskWorkspace)
	return err == nil && domain.CIConfigOnly(files)
}

func failedVerificationCalls(usage *registry.ToolUsage) int {
	failures := usage.Failures()
	n := 0
	for _, name := range domain.ImplementationVerificationTools {
		n += failures[name]
	}
	return n
}

func (r *Runner) refuseHandoff(ctx context.Context, job RunJob, wf domain.Workflow, key prompt.Key[struct{}], reason string) {
	r.sendBackForRevision(ctx, job, wf, prompt.Text(key), reason)
}

// handleRefusedAdvance answers a refused automatic hand-off. Only a refusal
// the next run can act on (a workflow rule or gate) bounces the task; an
// infrastructure error would fail the same way, so it is commented and left.
func (r *Runner) handleRefusedAdvance(ctx context.Context, job RunJob, wf domain.Workflow, moveErr error,
	stayKey, revisionKey prompt.Key[handoffReasonInput]) {
	data := handoffReasonInput{Reason: moveErr.Error()}
	_, hasRevision := wf.Stage(domain.TaskColumnNeedRevision)
	if hasRevision && domain.IsMoveRefusal(moveErr) {
		r.sendBackForRevision(ctx, job, wf, revisionKey.Render(data), domain.MoveReasonHandoffRefused)
		return
	}
	r.commentSystem(ctx, job, stayKey.Render(data))
}

func (r *Runner) commentSystem(ctx context.Context, job RunJob, content string) {
	if content == "" {
		return
	}
	if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    content,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("hand-off: system comment failed")
	}
}

func (r *Runner) taskLeftColumn(ctx context.Context, job RunJob) bool {
	reader, ok := r.taskUpdater.(taskColumnReader)
	if !ok {
		return false
	}
	fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("hand-off: task re-read failed, using the run's snapshot")
		return false
	}
	return fresh.Column != job.Task.Column
}

// sendBackForRevision moves a task whose run ended without a hand-off back to
// need_revision, which re-dispatches the developer; ReviewLoopGuard parks it
// for a human after repeated entries. A task that already left its column
// during the run is never touched. It reports whether the task was moved.
func (r *Runner) sendBackForRevision(ctx context.Context, job RunJob, wf domain.Workflow, comment, reason string) bool {
	if r.taskUpdater == nil || r.taskLeftColumn(ctx, job) {
		return false
	}
	r.commentSystem(ctx, job, comment)
	if _, ok := wf.Stage(domain.TaskColumnNeedRevision); !ok {
		return false
	}
	column := domain.TaskColumnNeedRevision
	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &column,
		SystemReason: reason,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Str("reason", reason).
			Msg("hand-off: sending the task back to need_revision failed, it stays in the working column")
		return false
	}
	return true
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
		r.handleRefusedAdvance(ctx, job, wf, err, handoffAnalizReviewRefusedKey, handoffAnalizReviewRefusedRevisionKey)
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
		return previousRunFailuresKey.Render(previousRunFailuresInput{ErrorPattern: prev.ErrorPattern})
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
	// Only the columns that produce the analysis document: in done the approved
	// plan is decomposed into tasks, which needs no fresh read of the code.
	if !wf.TypeHas(domain.BehaviourRequireRepoGrounding) || !wf.Has(task.Column, domain.BehaviourAdvanceOnDocument) ||
		resp.Clarification != nil || usage == nil {
		return false
	}
	return !usage.UsedAny(domain.CodeExplorationTools...)
}

func (r *Runner) failRunUngrounded(ctx context.Context, job RunJob, run domain.TaskAgentRun, resp domain.AgentResponse) error {
	reason := ungroundedAnalysisReason

	if r.taskUpdater != nil {
		content := reason
		if summary := strings.TrimSpace(resp.Message.Content); summary != "" {
			content += rejectedDraftNote(summary)
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

// rejectedDraftNote appends the run's own draft to an ungrounded-analysis
// rejection comment — it was never attached as the analysis, but a person
// reviewing the rejection may still want to see what was produced.
func rejectedDraftNote(summary string) string {
	return "\n\n" + prompt.Text(rejectedDraftLabelKey) + "\n\n" + summary
}

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

// rejectedRunReportReplanNote appends a rejected QA run's own report to the
// rejection comment: told to execute it, not to re-plan around it.
func rejectedRunReportReplanNote(summary string) string {
	return "\n\n" + prompt.Text(rejectedRunReportReplanKey) + "\n\n" + summary
}

func (r *Runner) failRunUngroundedQA(ctx context.Context, job RunJob, run domain.TaskAgentRun, resp domain.AgentResponse, reason string) error {
	if r.taskUpdater != nil {
		content := reason
		if summary := strings.TrimSpace(resp.Message.Content); summary != "" {
			content += rejectedRunReportReplanNote(summary)
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

// rejectedRunReportReapproveNote appends a rejected pm_uat run's own report
// to the rejection comment: told to execute it, not to re-approve around it.
func rejectedRunReportReapproveNote(summary string) string {
	return "\n\n" + prompt.Text(rejectedRunReportReapproveKey) + "\n\n" + summary
}

func (r *Runner) failRunUngroundedPMUAT(ctx context.Context, job RunJob, run domain.TaskAgentRun, resp domain.AgentResponse, reason string) error {
	if r.taskUpdater != nil {
		content := reason
		if summary := strings.TrimSpace(resp.Message.Content); summary != "" {
			content += rejectedRunReportReapproveNote(summary)
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
	published := job.Task.TaskType.PublishesBranch()
	if workspace != "" && published {
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
		kept := prompt.Text(outOfBudgetKeptPublishedKey)
		if !published {
			kept = prompt.Text(outOfBudgetKeptLocalKey)
		}
		content := outOfBudgetCommentKey.Render(outOfBudgetCommentInput{
			ErrorMessage: budgetErr.Error(),
			Kept:         kept,
			HasPartial:   budgetErr.Partial != "",
			Partial:      budgetErr.Partial,
		})
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
		// The human's own comments ride in humanRequirementsMessage, on every
		// run; an advisory-checks note describes the change but did not send it back.
		if c.AuthorType != "user" && !prompt.IsClarificationComment(c.Content) && !isAdvisoryChecksComment(c.Content) {
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
	sb.WriteString(prompt.Text(revisionCommentsHeaderKey))
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

// An html report's text rendition is denser than markdown of the same bytes —
// spec and plan in one document — so it gets the larger budget.
const (
	analysisContextLimit     = 12000
	htmlAnalysisContextLimit = 24000
)

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
	sb.WriteString(prompt.Text(analysisContextIntroKey))
	for _, ref := range refs {
		label := domain.RelationLabel(ref.Key, ref.Title, ref.TaskID)
		if len(ref.Documents) == 0 {
			sb.WriteString(analysisContextNoDocsKey.Render(analysisContextNoDocsInput{Label: label}))
			continue
		}
		sb.WriteString(analysisContextRefHeaderKey.Render(analysisContextRefHeaderInput{Label: label, Key: ref.Key}))
		for _, doc := range ref.Documents {
			content, limit := doc.Content, analysisContextLimit
			if doc.Format == domain.DocumentFormatHTML {
				content, limit = htmldoc.Text(doc.Content), htmlAnalysisContextLimit
			}
			if len(content) > limit {
				content = truncateHead(content, limit) + analysisContextTruncNoteKey.Render(analysisContextTruncNoteInput{Key: ref.Key})
			}
			sb.WriteString(analysisContextDocKey.Render(analysisContextDocInput{Title: doc.Title, Content: content}))
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

func reviewCriteriaHeader(column domain.TaskColumn) string {
	switch column {
	case domain.TaskColumnReadyForQA, domain.TaskColumnInQA:
		return prompt.Text(reviewCriteriaHeaderQAPMKey)
	case domain.TaskColumnPMUAT:
		return prompt.Text(reviewCriteriaHeaderPMUATKey)
	case domain.TaskColumnCodeReview:
		return prompt.Text(reviewCriteriaHeaderCodeReviewKey)
	default:
		return prompt.Text(reviewCriteriaHeaderDefaultKey)
	}
}

func criterionStateLine(c domain.AcceptanceCriterion, changedSince map[string][]string) string {
	return criterionLineKey.Render(criterionLineInput{
		ID:          c.ID.String(),
		Text:        c.Text,
		Implementer: criterionImplementerLabelKey.Render(criterionImplementerInput{Completed: c.Completed}),
		QA:          criterionVerdictLabel(c, domain.CriterionReviewRoleQA, changedSince),
		PM:          criterionVerdictLabel(c, domain.CriterionReviewRolePM, changedSince),
	})
}

func criterionVerdictLabel(c domain.AcceptanceCriterion, role domain.CriterionReviewRole, changedSince map[string][]string) string {
	for i := range c.Checks {
		check := c.Checks[i]
		if check.Role != role {
			continue
		}
		if check.Approved {
			hasSHA, hasFiles, sha, files, more := changedSinceFields(check.VerifiedSHA, changedSince)
			return criterionVerdictKey.Render(criterionVerdictInput{
				Found: true, Approved: true,
				HasSHA: hasSHA, HasFiles: hasFiles, SHA: sha, Files: files, More: more,
			})
		}
		return criterionVerdictKey.Render(criterionVerdictInput{Found: true, Approved: false, Note: strings.TrimSpace(check.Note)})
	}
	return criterionVerdictKey.Render(criterionVerdictInput{Found: false})
}

const maxChangedFilesNoted = 15

// changedSinceFields turns a verified SHA and the changed-files-since map
// into the facts board.criterion_verdict needs — no SHA on record, or the
// SHA missing from the map, means no note at all (HasSHA false), not an
// empty one.
func changedSinceFields(sha string, changedSince map[string][]string) (hasSHA, hasFiles bool, shortSHA string, files []string, more int) {
	if sha == "" {
		return false, false, "", nil, 0
	}
	fs, ok := changedSince[sha]
	if !ok {
		return false, false, "", nil, 0
	}
	if len(fs) == 0 {
		return true, false, domain.ShortSHA(sha), nil, 0
	}
	shown := fs
	if len(shown) > maxChangedFilesNoted {
		more = len(fs) - maxChangedFilesNoted
		shown = shown[:maxChangedFilesNoted]
	}
	return true, true, domain.ShortSHA(sha), shown, more
}

func criteriaMessage(wf domain.Workflow, task domain.BoardTask, criteria []domain.AcceptanceCriterion, changedSince map[string][]string) string {
	if len(criteria) == 0 {
		return ""
	}
	var sb strings.Builder
	if listsEveryCriterion(wf, task) {
		sb.WriteString(reviewCriteriaHeader(task.Column))
		for _, c := range criteria {
			sb.WriteString(criterionStateLine(c, changedSince))
		}
		return sb.String()
	}
	sb.WriteString(prompt.Text(criteriaOpenHeaderKey))
	for _, c := range criteria {
		sb.WriteString(criterionOpenLineKey.Render(criterionOpenLineInput{ID: c.ID.String(), Text: c.Text}))
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
		// task-decomposition puts the plan slice (files, interfaces, #step-N)
		// here; without it every derived run only has description, and the
		// slice is otherwise reachable only through task_chat or
		// list_board_tasks.
		"technical_description": job.Task.TechnicalDescription,
		"column":                string(job.Task.Column),
		"event_type":            string(job.Event.EventType),
		"payload":               json.RawMessage(job.Event.Payload),
	})
	runIns := runInstruction(wf, job)
	if job.ColumnInstruction != "" {
		runIns += "\n\n" + prompt.Text(columnSpecificNoteKey) + "\n" + job.ColumnInstruction
	}
	return triggerMessageKey.Render(triggerMessageInput{
		RunInstruction:  runIns,
		ClosingStep:     closingStep(wf, job),
		TaskJSON:        string(taskJSON),
		CriteriaMessage: criteriaMessage(wf, job.Task, criteria, changedSince),
	})
}

func closingStep(wf domain.Workflow, job RunJob) string {
	if wf.Has(job.Task.Column, domain.BehaviourBuildVerify) {
		return prompt.Text(closingStepBuildVerifyKey)
	}
	switch wf.KindOf(job.Task.Column) {
	case domain.StageKindReview:
		// code_review, in_qa, pm_uat: the run judges or tests, never edits —
		// "state what you changed" never applies here.
		return prompt.Text(closingStepReviewKey)
	case domain.StageKindTerminal:
		// done/released: the release engineer's own runs (an analiz task's
		// done/released are the architect's decompose/release step and keep
		// the default — it has no release verdict note to point to).
		if job.Task.TaskType != domain.TaskTypeAnaliz {
			return prompt.Text(closingStepReleaseKey)
		}
	}
	return prompt.Text(closingStepDefaultKey)
}

func runInstruction(wf domain.Workflow, job RunJob) string {
	task := job.Task
	if job.EnteredFrom == domain.TaskColumnNeedRevision {
		task.Column = domain.TaskColumnNeedRevision
	}
	return columnInstruction(wf, task)
}

func columnInstruction(wf domain.Workflow, task domain.BoardTask) string {
	if stage, ok := wf.Stage(task.Column); ok && stage.Instructions != "" {
		return stage.Instructions
	}
	// passTo from the workflow, not a literal: pm_uat for everything except technical, which goes straight to human_uat.
	passTo := "pm_uat"
	if target, ok := wf.Param(task.Column, domain.BehaviourReviewVerdictSweep, "pass_to"); ok && target != "" {
		passTo = target
	}
	analiz := task.TaskType == domain.TaskTypeAnaliz
	switch task.Column {
	case domain.TaskColumnTodo:
		if analiz {
			return prompt.Text(columnTodoAnalizKey)
		}
		return prompt.Text(columnTodoKey)
	case domain.TaskColumnInProgress:
		if analiz {
			return prompt.Text(columnInProgressAnalizKey)
		}
		return prompt.Text(columnInProgressKey)
	case domain.TaskColumnNeedRevision:
		if analiz {
			return prompt.Text(columnNeedRevisionAnalizKey)
		}
		return prompt.Text(columnNeedRevisionKey)
	case domain.TaskColumnCodeReview:
		return prompt.Text(columnCodeReviewKey)
	case domain.TaskColumnReadyForQA:
		// Only reachable when the automatic ready_for_qa -> in_qa move was refused; in_qa is where testing is evidenced.
		return columnReadyForQAKey.Render(columnPassToInput{PassTo: passTo})
	case domain.TaskColumnInQA:
		return columnInQAKey.Render(columnPassToInput{PassTo: passTo})
	case domain.TaskColumnPMUAT:
		return prompt.Text(columnPMUATKey)
	case domain.TaskColumnDone:
		if analiz {
			return prompt.Text(columnDoneAnalizKey)
		}
		// Reachable only through the merge wake or a release hand-back: a done card with an unmerged PR, or a
		// release the sweeper just settled, dispatched to the release engineer and nobody else.
		// done must never read the default: its "move on to the next column" sentence is how done tasks drifted into released with no deploy.
		return prompt.Text(columnDoneKey)
	case domain.TaskColumnReleased:
		// Reachable only through a release hand-back for a health incident inside the release's window: released
		// dispatches the release engineer for nothing else.
		// released has no next column: the generic default must never fire here, or a card is handed to nobody.
		return prompt.Text(columnReleasedKey)
	default:
		return columnDefaultKey.Render(columnDefaultInput{Column: string(task.Column)})
	}
}

func prependProjectContext(history []domain.Message, desc, toolsNote string) []domain.Message {
	var note string
	if desc != "" {
		note = projectContextNoteKey.Render(projectContextInput{Description: desc})
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
