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
	Run          domain.TaskAgentRun
	Event        domain.BoardEvent
	Task         domain.BoardTask
	RepositoryID uuid.UUID
	// EnteredFrom is the column the task was in when this run was dispatched,
	// set only when enterWorkingColumn moved it out of that column. It is what
	// keeps a need_revision run reading as a revision after the card has
	// (correctly) been moved to in_progress: the instruction, the reviewer's
	// comments and the failed pipeline are all chosen from it.
	EnteredFrom domain.TaskColumn
}

// isRevision reports whether this run is fixing review feedback, whether the
// card still says need_revision or has already been moved into in_progress for
// the duration of the run.
func (j RunJob) isRevision() bool {
	return j.Task.Column == domain.TaskColumnNeedRevision || j.EnteredFrom == domain.TaskColumnNeedRevision
}

type RepositoryResolver interface {
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
	ResolveDescription(ctx context.Context, repositoryID uuid.UUID) (string, error)
	ResolveRepository(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error)
	// ProfileForRun renders the project profile narrowed to the sections a
	// run of this kind needs; "" means every section.
	ProfileForRun(ctx context.Context, repositoryID uuid.UUID, kind string) string
}

// TaskUpdater lets the runner push a failed-verification task back, leave a
// comment, and read the comments a reviewer left (implemented by
// repository.Service; interface avoids import cycle).
type TaskUpdater interface {
	UpdateTask(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error)
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
	ListComments(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskComment, error)
}

type IndexInjector interface {
	InjectContext(ctx context.Context, sessionID uuid.UUID, messages []domain.Message, opts domain.InjectOptions) ([]domain.Message, error)
}

// BranchIndexer refreshes a task branch's workspace index after the agent
// pushes, so the next run's retrieval sees the branch's own tree.
//
// ctx is passed for its values, not its deadline: the refresh is asynchronous
// and outlives this run. The indexer strips the cancellation itself.
type BranchIndexer interface {
	StartIndexBranch(ctx context.Context, projectID uuid.UUID, branch, workspacePath string)
}

type Notifier interface {
	Notify(title, message string)
}

// AgentCLIConnections reports whether ONE local agent CLI flavor is connected,
// which is what the runner asks before dispatching a task onto a host
// executor.
//
// Nil-safe, and permissive when nil: a build that never wired the connect flow
// (the desktop bundle, a test) keeps the behaviour it had. A wired one that
// answers "not connected" is a refusal, not a pass — see requireConnectedCLI.
type AgentCLIConnections interface {
	Connected(ctx context.Context, flavor domain.AgentCLIFlavor) (*domain.AgentCLIConnection, error)
}

// BillingGate blocks agent runs once the USD budget is exhausted and
// records the task so it can be auto-resumed when the period renews. Nil-safe:
// a runner with no gate never blocks.
type BillingGate interface {
	Allow(ctx context.Context) (bool, string)
	PauseTask(ctx context.Context, repositoryID, taskID uuid.UUID)
}

// TaskBlocker parks a task on the clarification chat the agent opened, so the
// board shows it as waiting on a human instead of quietly idle. Nil-safe.
//
// BlockOnResource is the other half: the run stopped because a shared resource
// was taken, so the card waits on hardware rather than on a person and the
// device sweeper — not an answer — releases it. It hands back the column the
// task was parked OUT of, which is the from_column of the move the ParkJournal
// then records.
type TaskBlocker interface {
	BlockOnQuestion(ctx context.Context, repositoryID, taskID, sessionID uuid.UUID, question string) error
	BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error)
}

// SetAgentCLIConnections wires the local-CLI connection reader. After
// construction, like SetTaskExecutor and for the same reason: the CLI executor
// and the connect service are both assembled later in platform/runtime, and a
// constructor argument would force one of them to exist before it can.
func (r *Runner) SetAgentCLIConnections(c AgentCLIConnections) { r.agentCLIs = c }

// requireConnectedCLI refuses a run whose agent is on a host-executed provider
// that is not itself connected. Unlike before, another flavor being connected
// at the same time has no bearing on this check — each agent's own provider
// answers the question now, so two agents on two different connected CLIs both
// dispatch.
//
// hostExecutorHint returns the binary name and env var for a host-executed
// provider, so error messages point the user at the right CLI.
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

// A read failure is a refusal, not a pass. The question being answered is "was
// this binary verified", and a database that cannot answer it has not said yes;
// treating the error as permission would defeat the check on exactly the hosts
// where things are already going wrong.
func (r *Runner) requireConnectedCLI(ctx context.Context, agentRec domain.Agent) error {
	if r.agentCLIs == nil {
		return nil
	}
	flavor, isCLI := domain.AgentCLIFlavorFor(agentRec.ProviderType)
	if !isCLI {
		// Not a host-executed provider at all, so there is nothing to connect
		// and nothing this check could refuse.
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

// SetTaskPRRecorder wires the store that remembers which pull request a task's
// branch ended up in. Set after construction like SetTaskBlocker; nil-safe, so a
// build without a board task store still opens PRs and just cannot record them.
func (r *Runner) SetTaskPRRecorder(rec TaskPRRecorder) { r.prRecorder = rec }

// PullRequestReader reads the PR a task is being reviewed in, including the
// review comments left on it. Optional: a build with no GitHub token store has
// no PR to read and simply runs without that block.
type PullRequestReader interface {
	PullRequest(ctx context.Context, repositoryID, taskID uuid.UUID, includeDiff bool) (domain.TaskPullRequest, error)
}

// SetPullRequestReader wires the PR reader used to put a reviewer's PR comments
// in front of the revision run that has to act on them.
func (r *Runner) SetPullRequestReader(reader PullRequestReader) { r.prReader = reader }

type Runner struct {
	// agentLoop is the ROUTER in production (agent.Router), not the bare loop:
	// every run this package starts — the main one, the verify-fix rounds, the
	// criteria and review sweeps — must be able to land on a host executor when
	// the agent's provider is a process rather than an endpoint.
	agentLoop agent.Runner
	// executor runs a task somewhere other than the in-process loop — today,
	// the Claude Code CLI on this host. Nil on every installation that has no
	// such executor, which is the normal case; see runTask for how the choice
	// is made and why a missing executor is a clear failure rather than a
	// silent fallback.
	executor      port.TaskExecutor
	orchSvc       *orchestrator.Service
	catalog       port.CatalogStore
	activityStore port.ActivityStore
	runs          port.TaskAgentRunStore
	sessions      port.SessionStore
	projects      RepositoryResolver
	indexInjector IndexInjector
	branchIndexer BranchIndexer
	perfStore     port.AgentPerformanceStore
	memories      port.AgentMemoryStore
	kpis          port.AgentKPIStore
	notifier      Notifier
	git           port.GitClient
	// llm writes the English commit message; nil-safe (the original title and
	// summary are committed unchanged when it is missing).
	llm           port.LLMClient
	workspaceRoot string
	budget        appcontext.Budget
	indexerCfg    domain.IndexerConfig
	mappingCfg    domain.MappingConfig
	defaultPolicy domain.ToolPolicy
	defaultLang   string
	settings      port.SettingsStore
	// heartbeatEvery is runHeartbeat, overridable so a test can drive the
	// cross-replica stop without waiting ten seconds for a tick. Production
	// never sets it.
	heartbeatEvery    time.Duration
	taskUpdater       TaskUpdater
	verifyEnabled     bool
	verifyFixAttempts int
	taskTypeModels    map[string]string
	pipelines         port.TaskPipelineStore
	billing           BillingGate
	blocker           TaskBlocker
	// toolchains reads the version pins the task's checkout declares for
	// itself. Nil, or Available() false, leaves the session on this machine's
	// own defaults.
	toolchains port.ToolchainDetector
	// parks writes the board event and the column span a park would otherwise
	// leave behind — see ParkJournal for why it is not the dispatcher's job.
	// Nil-safe: without it a park is exactly as (in)visible as it was before.
	parks      *ParkJournal
	prRecorder TaskPRRecorder
	prReader   PullRequestReader
	agentCLIs  AgentCLIConnections
	queue      chan RunJob
	wg         sync.WaitGroup
	cancel     context.CancelFunc
	drain      chan struct{}
	drainOnce  sync.Once
	activeMu   sync.RWMutex
	active     map[uuid.UUID]struct{}
	// queued is every run this process has accepted but not started: sitting in
	// the channel, or parked behind another run on the same task. The
	// reconciler needs it. A pending row heartbeats nothing — Touch only runs
	// while a run is executing — so "pending and old" is the only signal it has,
	// and without knowing which pending runs are queued HERE it could only wait
	// out the full stale window before touching any of them.
	queued map[uuid.UUID]struct{}
	// activeTasks/parked serialize runs per task: the run holding a task, and
	// the jobs waiting for it to let go. See beginTask.
	activeTasks map[uuid.UUID]uuid.UUID
	parked      map[uuid.UUID][]RunJob
	// cancels reaches ONE run's goroutine: a human pressing stop cancels that
	// run's context, not the pool's. Guarded by activeMu together with the maps
	// above — every one of them is written at the same two moments of a run's
	// life, and a second mutex would only invent a lock order to get wrong.
	cancels map[uuid.UUID]context.CancelFunc
}

// runHeartbeat is how often an in-flight run bumps its row's updated_at — and,
// since that write reads the status back, how long a stop can take to reach the
// replica actually executing the run.
//
// It was a minute, chosen only against the reconciler's stale window. That is
// far too long for a stop button now that the beat is also the cancel channel:
// a user who presses stop watches a Claude session on their own Mac keep
// working. Ten seconds costs six UPDATEs a minute per live run — nothing beside
// a run that takes minutes — and is still comfortably inside every staleness
// window that reads it.
const runHeartbeat = 10 * time.Second

// runLiveWithin is how fresh a heartbeat must be for a 'running' row to count
// as somebody's live work: against the claim's three budgets, and against
// "is this task's workspace in use".
//
// Six beats. Generous on purpose — a slow query, a paused container or a
// garbage-collection pause must not make a live run look abandoned and let a
// second replica start a second agent on the same branch — and still an order
// of magnitude shorter than the reconciler's stale window, which is about
// giving up on a run rather than about not colliding with one.
const runLiveWithin = 6 * runHeartbeat

// persistTimeout bounds the writes that must land even while the process is
// shutting down (terminal run status). Short: the pod is on its way out.
const persistTimeout = 15 * time.Second

// persistCtx returns a context that survives cancellation of ctx. A run whose
// context was cancelled — pod terminating, drain deadline hit — must still be
// able to write its final status, or the row stays 'running' forever and the
// reconciler has to guess about it half an hour later.
func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

type RunnerDeps struct {
	AgentLoop     agent.Runner
	OrchSvc       *orchestrator.Service
	Catalog       port.CatalogStore
	ActivityStore port.ActivityStore
	Runs          port.TaskAgentRunStore
	Sessions      port.SessionStore
	Repositories  RepositoryResolver
	IndexInjector IndexInjector
	BranchIndexer BranchIndexer
	PerfStore     port.AgentPerformanceStore
	Memories      port.AgentMemoryStore
	KPIs          port.AgentKPIStore
	Notifier      Notifier
	Git           port.GitClient
	// LLM rewrites commit messages into English. Optional.
	LLM           port.LLMClient
	WorkspaceRoot string
	Budget        appcontext.Budget
	IndexerCfg    domain.IndexerConfig
	MappingCfg    domain.MappingConfig
	DefaultPolicy domain.ToolPolicy
	DefaultLang   string
	// Settings lets the board path honour a language change made in the UI; the
	// runner outlives the setting, so DefaultLang is only the startup fallback.
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
		cancels:           make(map[uuid.UUID]context.CancelFunc),
	}
}

// language resolves the agents' user-facing language per run rather than at
// construction, so switching it in Settings takes effect on the next board run
// instead of requiring a restart. Falls back to the startup default when the
// settings store is absent (desktop) or unreadable.
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

// SetTaskUpdater wires the repository service in after construction.
func (r *Runner) SetTaskUpdater(t TaskUpdater) {
	r.taskUpdater = t
}

func (r *Runner) SetRepositories(resolver RepositoryResolver) {
	r.projects = resolver
}

// SetPipelines wires the pipeline store in after construction. Needed because
// runtime.go constructs the pipeline store (it depends on the postgres pool)
// after the board Runner, mirroring SetTaskUpdater/SetRepositories above.
func (r *Runner) SetPipelines(store port.TaskPipelineStore) {
	r.pipelines = store
}

// SetBilling wires the budget gate (late-set to avoid a construction cycle).
func (r *Runner) SetBilling(gate BillingGate) {
	r.billing = gate
}

// SetTaskExecutor wires an out-of-process executor for the providers it
// supports. Set after construction like the stores above, because whether one
// exists at all is decided at boot by probing the host (is the CLI installed),
// not by the board's own configuration.
//
// Nil-safe: without one, every run takes the agent-loop path exactly as before.
func (r *Runner) SetTaskExecutor(ex port.TaskExecutor) {
	r.executor = ex
}

// SetTaskBlocker wires the store that parks a task on an unanswered question.
func (r *Runner) SetTaskBlocker(b TaskBlocker) {
	r.blocker = b
}

// SetToolchainDetector wires the reader of a checkout's own version pins.
//
// Late-set like the stores above, and nil-safe: without it a session runs on
// whatever versions this machine resolves by itself, which is where every run
// was before the detector existed.
func (r *Runner) SetToolchainDetector(d port.ToolchainDetector) {
	r.toolchains = d
}

// detectToolchain reads what the checkout declares, and never fails the run
// for the answer.
//
// A detection that could not be made leaves the session on the machine's own
// defaults; failing the run instead would turn a repository that pins nothing
// into a task that never starts.
//
// Absence stays absence. An empty answer is returned as nil, which omits the
// parameter entirely; nothing here fills it with a "system" or "latest"
// default, because none of those is something the checkout said.
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

// SetParkJournal wires the writer that makes a park visible on the board. Late-
// set like the stores above, and nil-safe: without it the card still parks and
// still resumes, it just leaves no row in the task's history.
func (r *Runner) SetParkJournal(j *ParkJournal) {
	r.parks = j
}

func (r *Runner) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	r.wg.Add(1)
	go r.dispatch(ctx)
}

// Stop severs: in-flight runs are cancelled immediately. Shutdown paths should
// call Drain instead — this is for tests and for a caller that has already
// decided the work is expendable.
func (r *Runner) Stop() {
	r.closeDrain()
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

// Drain stops taking new jobs and waits for the ones already running to finish.
// An agent run is minutes of real work — a checked-out branch, edits, a build —
// and killing it mid-flight leaves the task_agent_runs row stuck 'running' and
// the branch half-written. Only when ctx expires (the pod's grace period is
// about to run out) does it fall back to cancelling.
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

// IsActive reports whether this run belongs to this process: executing right
// now, or queued/parked waiting for a worker. The reconciler asks before
// declaring a run abandoned, and both answers have to be "yes" — a run waiting
// behind another run on the same task has an untouched row and would otherwise
// look exactly like one whose process died.
func (r *Runner) IsActive(runID uuid.UUID) bool {
	r.activeMu.RLock()
	defer r.activeMu.RUnlock()
	if _, ok := r.active[runID]; ok {
		return true
	}
	_, ok := r.queued[runID]
	return ok
}

// IsTaskActive reports whether a run for this task is executing in this
// process. Asked by anything that would disturb the task's workspace: the row's
// column can already read "done" while the run that moved it there is still
// committing into the checkout.
func (r *Runner) IsTaskActive(taskID uuid.UUID) bool {
	if r == nil {
		return false
	}
	r.activeMu.RLock()
	defer r.activeMu.RUnlock()
	_, ok := r.activeTasks[taskID]
	return ok
}

// ActiveCount is the number of runs this process is executing right now.
func (r *Runner) ActiveCount() int {
	r.activeMu.RLock()
	defer r.activeMu.RUnlock()
	return len(r.active)
}

func (r *Runner) markActive(runID uuid.UUID) func() {
	r.activeMu.Lock()
	r.active[runID] = struct{}{}
	// It has stopped waiting and started running: one set or the other, never
	// both, so the pair reads as "this process owns the run" either way.
	delete(r.queued, runID)
	r.activeMu.Unlock()
	return func() {
		r.activeMu.Lock()
		delete(r.active, runID)
		r.activeMu.Unlock()
	}
}

// registerCancel publishes the run's cancel func for Cancel to find and returns
// the closure that withdraws it. Same shape as markActive, for the same reason:
// the run owns both halves, so it cannot leave a cancel func behind pointing at
// a context nobody is running under any more.
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

// Cancel stops the run this process is executing and reports whether it had one.
// False is an ordinary answer, not an error: the run may still be waiting in the
// in-memory queue (worker() checks its row before starting it), or it may have
// finished a moment ago. The durable half of a stop is the cancelled row, which
// the caller has already written.
func (r *Runner) Cancel(runID uuid.UUID) bool {
	r.activeMu.RLock()
	cancel, ok := r.cancels[runID]
	r.activeMu.RUnlock()
	if !ok {
		return false
	}
	// Outside the lock: cancelling wakes the run's own goroutine, and the first
	// thing it does on its way out is take activeMu to unregister itself.
	cancel()
	return true
}

// stopRequested distinguishes a stop aimed at THIS run — a human pressed the
// button — from the whole process going down, where the cancellation arrives
// through parent. The difference decides who owes the row a terminal status: a
// stopped run's row already says cancelled (the request wrote it), while a run
// killed by shutdown still has to write its own, which is what persistCtx is for.
func stopRequested(parent, runCtx context.Context) bool {
	return runCtx.Err() != nil && parent.Err() == nil
}

// beginTask claims the task for this run, or parks the job behind whoever
// holds it. Reports whether the caller may proceed.
//
// One task at a time, however many workers are free. The runs on a task share
// its branch and its checked-out workspace, and a run does not end when the
// agent stops talking: verification, up to N fix rounds, the commit and the
// push all come after. The architect dispatched into code_review used to start
// during that tail and review a tree the developer was still writing.
func (r *Runner) beginTask(job RunJob) bool {
	taskID := job.Task.ID
	if taskID == uuid.Nil {
		// No task to serialize on (tests, synthetic jobs): let it run.
		return true
	}
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	if holder, busy := r.activeTasks[taskID]; busy {
		r.parked[taskID] = append(r.parked[taskID], job)
		log.Info().Str("task_id", taskID.String()).Str("run_id", job.Run.ID.String()).
			Str("waiting_for_run_id", holder.String()).
			Msg("board run parked: another run holds this task")
		return false
	}
	r.activeTasks[taskID] = job.Run.ID
	return true
}

// endTask releases the task and hands it to the job that has waited longest,
// one at a time — releasing the whole queue at once would recreate the
// concurrency beginTask exists to prevent.
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
	r.activeMu.Unlock()
	if next.Run.ID != uuid.Nil {
		r.Enqueue(next)
	}
}

// startHeartbeat keeps the run's updated_at fresh for as long as it runs, and
// — because that same write reads the row's status back — is how a stop
// crosses processes.
//
// Cancel is the run's own context cancel. Calling it here is the durable half
// of a stop button becoming a real one: the request that pressed stop wrote
// 'cancelled' on the row from whichever replica served it, and Runner.Cancel
// could only ever reach a run in ITS OWN memory. On any other replica the user
// was told the run had stopped while a `claude` session on their Mac carried
// on. The row is the one thing both processes can see, so the row is the
// signal; see the decision note in .ai/architecture.md for why this rather than
// LISTEN/NOTIFY.
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
						// Not treated as a stop. A failed read says nothing
						// about the row, and killing a live run over a
						// transient pool error would be a worse bug than the
						// one this loop closes.
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
		// Dropped, so NOT marked: the row is pending and nothing here will ever
		// start it, which is precisely the case the reconciler must be free to
		// recover.
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

// dispatch starts every queued job in its own goroutine. There is no worker
// pool and no cap on how many runs execute at once: a run only ever waits for
// another run on the SAME task (beginTask), because those share a checkout.
func (r *Runner) dispatch(ctx context.Context) {
	defer r.wg.Done()
	for {
		// Drain is checked before the queue so a job arriving during shutdown
		// is not started.
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

func (r *Runner) runJob(ctx context.Context, job RunJob) {
	// A job can sit in the queue for a moment, and a stop that arrived meanwhile
	// was recorded on the row, not in this queue. Read the row before claiming
	// anything: a run stopped while it waited must not start now.
	if r.alreadyStopped(ctx, job.Run.ID) {
		r.unmarkQueued(job.Run.ID)
		return
	}
	// A job for a task that already has a run is parked and comes back through
	// the queue when that task frees up.
	if !r.beginTask(job) {
		return
	}
	err := r.execute(ctx, job)
	r.endTask(job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("run_id", job.Run.ID.String()).Msg("board agent run failed")
	}
}

// alreadyStopped reports a queued run whose row says it is over. A row that
// could not be read says nothing about the run: dropping the job on a transient
// pool error would silently lose real work, so the run goes ahead and finds out
// for itself.
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

func (r *Runner) execute(parent context.Context, job RunJob) error {
	run := job.Run

	// Everything this run touches hangs off ctx — the agent loop, the tools it
	// spawns, the git work around them — so cancelling it is what makes the run
	// stoppable at all. Registered before the row says 'running': a stop that
	// arrives in the first millisecond must still find the run it is aimed at.
	ctx, cancelRun := context.WithCancel(parent)
	defer cancelRun()
	defer r.registerCancel(run.ID, cancelRun)()

	// The claim, and the only way a run becomes 'running'.
	//
	// It replaces an unconditional UPDATE and answers the one question that used
	// to be a map in this process's memory: is another run already on this task
	// (Runner.activeTasks). There is no concurrency budget; every run whose task
	// is free starts now.
	//
	// A refusal is not an error and leaves the row 'pending', which is exactly
	// what the reconciler's never-started sweep collects a couple of minutes
	// later. That is a durable park: the old behaviour parked the losing job in
	// a Go map that died with the pod.
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
	defer r.startHeartbeat(ctx, run.ID, cancelRun)()

	// A run a human stopped is not a failed run, so every failure below reports
	// through this: wherever the stop caught the run, it leaves no verdict on
	// the row (already written, as 'cancelled', by the request itself) and no
	// explanation on a task nobody is working any more.
	fail := func(cause error) error {
		if stopRequested(parent, ctx) {
			log.Info().Str("run_id", run.ID.String()).Str("task_id", job.Task.ID.String()).
				Msg("board run stopped, dropping the failure it was about to report")
			return nil
		}
		return r.failRun(ctx, run, cause)
	}

	// Budget gate: if the USD budget is exhausted, do not start the
	// agent. Record the task so it auto-resumes when the period renews, and end
	// this run cleanly (not an error — nothing failed, the work is deferred).
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

	// The column now says what is actually happening, before the agent reads a
	// single message. Everything below — the prompt's task snapshot, the column
	// instruction, the branch — is built from the updated task.
	dispatchedIn := job.Task.Column
	job.Task = r.enterWorkingColumn(ctx, job)
	if job.Task.Column != dispatchedIn {
		job.EnteredFrom = dispatchedIn
	}

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
	// Read once for both consumers below: the skill index groups by stack, and
	// agentfs stamps the stack name into each materialised skill file.
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
		// The working copy is a cache, not the source of truth. If it is gone
		// (wiped disk) or was never a repo, restore it from the recorded origin
		// instead of handing the agent an empty directory — that is what made
		// an agent ask the human for the repository path.
		if err := r.ensureWorkingCopy(ctx, repoRec, rootPath); err != nil {
			return fail(err)
		}
	} else if err := workspace.EnsureDir(rootPath); err != nil {
		return fail(err)
	}
	if r.git != nil && r.workspaceRoot != "" && r.git.HasGit(rootPath) {
		// Deterministic pre-LLM setup: clone the repo into an isolated task
		// workspace and check out the task branch BEFORE any agent runs. This is
		// a hard gate — if clone/branch fails we must NOT fall back to running
		// the LLM on the shared project root (it would work on the wrong tree,
		// on someone else's branch, and could push to the default branch). Fail
		// the run instead so nothing runs until the workspace is ready.
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

	// A CLI provider reads its catalog off the workspace instead of out of the
	// prompt: agentfs writes the role, its rules and its skills in the shape
	// the binary discovers on its own. It happens here because this is the only
	// moment when the workspace exists and the session has not started.
	skillDelivery := prompt.SkillsInPrompt
	if flavor, isCLI := cliFlavor(agentRec.ProviderType); isCLI {
		// Both failures end the run rather than being logged and stepped over.
		// A session whose skills silently did not arrive still LOOKS like a
		// working run — it does plausible work on its native tools and returns a
		// closing message, and only the quality gives it away, one task at a
		// time. A workspace that was not excluded is worse than that: the run
		// ends by committing, so the catalog lands in the task's own pull
		// request and, on a public repository, leaves the customer's control.
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
		// Removed when the run ends. The workspace outlives it — the next task
		// on this repository gets the same directory — and this agent's skills
		// offered to the next agent, under names that look like they belong,
		// are worse than none at all.
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
	// The task this run works on. Without it the PR tools' task_id fallback
	// (resolveTaskArg) has nothing to fall back to, so a model that omits the
	// argument gets an error and starts hunting for its own task with
	// list_board_tasks instead of committing.
	runCtx = registry.ContextWithTaskID(runCtx, job.Task.ID)
	// Every tool this run executes — including the ones inside orchestrator
	// subtasks, which get their own agent loops — is counted here, so the run's
	// output can be checked against what it actually did rather than what it
	// claims to have done.
	runCtx, toolUsage := registry.ContextWithToolUsage(runCtx)
	// Same idea for tokens: every LLM call under this context — loop turns,
	// summarizer, orchestrator subtasks — adds its Usage here (see
	// usage.RecordingClient), and the totals are stamped onto the run row.
	runCtx, tokenUsage := usageapp.ContextWithTokenUsage(runCtx)
	if sessionID != uuid.Nil {
		runCtx = registry.ContextWithSessionID(runCtx, sessionID)
	}
	if taskBranch != "" {
		runCtx = registry.ContextWithBranch(runCtx, taskBranch)
	}
	// The toolchain the repository declares for itself, resolved once per run,
	// in two complementary forms.
	//
	// The overlay picks an INSTALL on this machine and rides the context, so
	// every process the agent spawns here gets the same PATH — two concurrent
	// tasks pinning different Go/Node versions must not share the host default.
	// The detector reads the same pin files for the VERSION NAMES a session's
	// own version manager takes (GOTOOLCHAIN, NODE_VERSION, …) and rides the
	// request, because an executor that starts a CLI is the only thing that can
	// apply them.
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
				// The run did not fail; it was called off. The activity timeline
				// is read to explain a run's ending, so it says so.
				rec.Complete(domain.TaskAgentRunStatusCancelled)
			case errors.As(err, &quotaErr):
				// A parked run is not a failed one. err still holds the block —
				// the park path returns nil from execute — so without this arm
				// the three places a human reads the same run disagree: the
				// timeline says failed, the run row says completed, the card says
				// blocked on the quota.
				rec.Complete(domain.TaskAgentRunStatusCompleted)
			case err != nil:
				rec.Complete("failed")
			default:
				rec.Complete("completed")
			}
		}()
	}

	systemPrompt := prompt.BuildSystemPromptFor(agentRec, enabledSkills, techStacks, ruleTexts, r.language(runCtx), skillDelivery)
	triggerMsg := buildTriggerMessage(job, r.criteriaForRun(runCtx, job))

	// Every context block below is computed here, in its original relative
	// order, so every side effect (git reads, the PR lookup's early return,
	// the pipeline/comments/previous-run queries) still happens exactly when
	// it used to. What changes is that the result lands in history only once,
	// at the bottom, in cache-friendly order — instead of each block
	// prepending itself as soon as it was found, which put whichever block was
	// computed LAST FIRST on the wire. That reversal is why the most volatile
	// thing in the run (the RAG chunk, keyed off this task's own text) used to
	// sit in front of the one thing that never changes between runs of the
	// same agent: its persona. Every provider's prefix cache — Anthropic
	// cache_control, OpenAI/Gemini automatic caching, llama.cpp's KV cache —
	// only pays off when the shared prefix is byte-identical run over run, so
	// stable content now leads and this run's own evidence trails it, closest
	// to where generation starts.
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
		// A board run always happens inside a repository, so the agent gets
		// that repository's memories alongside its global ones.
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
			diffMsg = reviewDiffMessage(job.Task.Column, diff)
		}
	}
	// A code review is a review OF A PULL REQUEST. If the developer's run never
	// got one opened, open it here before the reviewer reads anything; a branch
	// that still cannot have a PR is not reviewable, and saying so beats a
	// verdict nobody can trace back to a diff on the remote.
	if job.Task.Column == domain.TaskColumnCodeReview && taskWorkspace != "" && r.git != nil {
		var prErr error
		prMsg, prErr = r.reviewPRContext(ctx, taskWorkspace, job.Task.ID)
		if prErr != nil {
			// Same rule as fail(): a stopped run leaves no comment behind, least
			// of all one blaming the branch for a stop it had nothing to do with.
			if stopRequested(parent, ctx) {
				log.Info().Str("run_id", run.ID.String()).Str("task_id", job.Task.ID.String()).
					Msg("board run stopped while opening the review pull request")
				return nil
			}
			err = r.failRunNoPR(ctx, job, run, prErr)
			return err
		}
	}
	// A revision run reads the PR the reviewer wrote on, not just the card.
	if job.isRevision() {
		prMsg = r.revisionPRComments(ctx, job)
	}
	if job.isRevision() && r.pipelines != nil {
		if pl, plErr := r.pipelines.LatestByTask(ctx, job.Task.ID); plErr != nil {
			// A task with no pipelines yet is expected (e.g. it reached
			// need_revision via the verify gate); only warn on real failures.
			if !errors.Is(plErr, domain.ErrPipelineNotFound) {
				log.Warn().Err(plErr).Str("task_id", job.Task.ID.String()).Msg("fetch latest pipeline for revision context failed")
			}
		} else if pl.Status == domain.PipelineStatusFailed {
			report := pipelineFailureReport(pl)
			pipelineMsg = "## Pipeline failure (fix these before moving back to ready_for_qa)\n" + report
		}
	}
	// The trigger message tells a need_revision agent to "read the comment",
	// but the move event's payload never carried one — DE-4's agent answered
	// by asking the human what the revision was. Feed the reviewer's actual
	// comments into the run so the instruction is satisfiable.
	//
	// Answered clarifications go into EVERY run, not just revisions: they are
	// requirements the human already settled, and a run that cannot see them
	// asks for them again (the same question came back three times in one chat).
	// The analysis behind this task, for every run of it and not only the
	// first: a revision run is fixing work against the same spec, and a
	// reviewer judging the diff is judging it against the same spec too.
	analysisMsg = r.analysisContext(ctx, job)
	comments := r.taskComments(ctx, job)
	if job.isRevision() {
		revisionMsg = revisionCommentsMessage(comments)
	}
	clarificationsMsg = prompt.AnsweredClarificationsMessage(comments)
	// What the last attempt at this task ran into. Read failures degrade the
	// run's context, they do not end it, so a lookup error is logged and skipped.
	//
	// The same rows answer a second question, for host-executed providers only:
	// which CLI session this task already has work in. A run parked on the
	// Claude Code usage limit records its session id on its row, and the run
	// that resumes the task is a NEW row — so the id has to travel through the
	// task's history rather than through memory, which is also what makes the
	// resume survive a restart.
	//
	// They answer a third: how many times in a row this task has already parked
	// on the quota. See quotaParkStreak — the park path reads it before parking
	// again.
	resumeCLISession := ""
	var prevRuns []domain.TaskAgentRun
	if rows, prevErr := r.runs.ListByTask(runCtx, job.Task.ID, quotaParkHistoryDepth); prevErr != nil {
		log.Warn().Err(prevErr).Str("task_id", job.Task.ID.String()).Msg("previous run lookup for failure context failed")
	} else {
		prevRuns = rows
		prevFailuresMsg = previousRunFailuresMessage(prevRuns, run.ID)
		resumeCLISession = latestCLISession(prevRuns, run.ID, job.Run.AgentID)
	}
	var projectDesc, projectProfile string
	if repoCtx, repoCtxErr := r.projects.ResolveRepository(ctx, job.RepositoryID); repoCtxErr == nil {
		// The profile is injected narrowed to the area this agent works in: a
		// mobile developer inherits the invariants and the deploy path, not
		// the web app's layout. On a single-kind repo (or an agent whose role
		// names no area) the narrowing is a no-op and everything goes in.
		projectDesc = repoCtx.Description
		projectProfile = r.projects.ProfileForRun(ctx, job.RepositoryID, profileKindForAgent(agentRec.Name, repoCtx.Kind))
		if projectProfile == "" {
			projectProfile = repoCtx.ProfileMD
		}
	}

	// Stable-to-volatile, left to right: persona, then workspace/project
	// (changes only when the repo does), then score/KPI/memory (small,
	// per-run), then the trigger, then this task's own evidence — nil/empty
	// blocks above are skipped exactly as before, only their position changed.
	history := []domain.Message{{Role: domain.RoleSystem, Content: systemPrompt}}
	history = append(history, domain.Message{Role: domain.RoleSystem, Content: prompt.SubtaskWorkspaceNote(workDir)})
	history = append(history, prependProjectContext(nil, projectDesc, projectProfile)...)
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
	// First of this run's own evidence blocks, ahead of the diff and the PR:
	// those say what has been done to the task, this says what the task is
	// supposed to be. A run that reads them in the other order reviews a change
	// before it knows the specification it is meant to satisfy.
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
		// Semantic code context is an enrichment, not a requirement — an
		// embedding-provider misconfiguration here must not take down the
		// whole task run (matches executor.go's explorer-context handling).
		if injected, injectErr := r.indexInjector.InjectContext(runCtx, sessionID, history, domain.InjectOptions{
			TopK:            topK,
			IncludeTree:     r.mappingCfg.Enabled,
			IncludeSkeleton: r.mappingCfg.Enabled && r.mappingCfg.InjectOnSessionStart,
			MaxChunkTokens:  6000,
		}); injectErr != nil {
			log.Warn().Err(injectErr).Str("task_id", job.Task.ID.String()).Msg("board run: index context injection failed, continuing without it")
		} else if len(injected) == len(history)+1 {
			// InjectContext prepends the chunk it built to the front of
			// whatever it is given, which is right when the caller is a
			// growing conversation but wrong here: this is the initial
			// message list for a fresh run, and the chunk is the most
			// volatile block in it (keyed off this task's own text, so it
			// differs every run) landing in front of history that is now
			// deliberately ordered stable-first. Move it to the tail instead
			// of accepting the prepend.
			history = append(history, injected[0])
		} else {
			// Defensive: length changed by something other than the single
			// system message InjectContext documents itself as adding. Trust
			// its output as given rather than guess which entry to relocate.
			history = injected
		}
	}
	// One pre-run fit, on a history that is nothing but head. It cannot disturb
	// the ordering above: every block here is a system message, which Apply never
	// removes, and the single user message is the trigger, which Apply protects
	// as the last user message (nothing after this point adds another one, so it
	// stays the last for the whole run). What is left for it to do on an
	// oversized fresh head is shed images, which is exactly what it is wanted for.
	// From the first model turn on, the loop's own summarizing trim takes over and
	// treats all of this as the protected head.
	if r.budget.MaxTokens > 0 {
		history = r.budget.Apply(history)
	}

	policy := domain.MergeToolPolicy(r.defaultPolicy, agentRec.ToolPolicy)
	// Role first (what this agent may do), task type second (what this task has
	// any use for): an analiz run loses the file writers, because its deliverable
	// is a document and nothing it wrote to the workspace would ever be committed.
	// Two narrowings, and they answer different questions: the task type says
	// what this DELIVERABLE may touch (an analiz produces a document), the column
	// says what this RUN is for (a review or a QA round produces a verdict about
	// someone else's work, never a change to it).
	upliftedPolicy := domain.RestrictCodeToolsForVerification(
		domain.RestrictToolsForVerdictColumn(
			domain.RestrictToolsForTaskType(domain.UpliftWorkspaceTools(policy), job.Task.TaskType),
			job.Task.Column,
		),
		job.Task.Column,
	)

	var resp domain.AgentResponse
	// Shared with every follow-up step dispatched on runCtx below (verify-fix,
	// the criteria sweep, the review sweep) so each one resumes the CLI
	// session the main call opens instead of replaying the whole history.
	// Only the host-executed branch ever fills it in, so a loop or
	// orchestrator run leaves it empty and every follow-up takes the
	// fresh-history path exactly as before.
	cliSession := &agent.CLISession{}
	runCtx = agent.ContextWithCLISession(runCtx, cliSession)
	switch {
	case domain.RequiresHostExecutor(agentRec.ProviderType):
		// This agent's provider is a process on this host, not an endpoint. The
		// check is on the PROVIDER rather than on "is there an executor",
		// because falling through to the loop here would be the worst outcome
		// available: the loop would call a provider that has no HTTP client and
		// fail with a message about a missing configuration, for an agent whose
		// configuration is fine and whose host simply lacks the binary.
		if r.executor == nil || !r.executor.Supports(agentRec.ProviderType) {
			binary, envVar := hostExecutorHint(agentRec.ProviderType)
			return fail(fmt.Errorf(
				"%s binary not available on this host: agent %q runs on the %s provider, which needs the %q CLI installed where agent-server runs (set %s if it is not on PATH). Move the agent to an API provider or install the CLI",
				binary, agentRec.Name, agentRec.ProviderType, binary, envVar))
		}
		// And this CLI has to be CONNECTED. A registered executor
		// says the binary resolved at boot; it does not say anybody verified it
		// is signed in, and it says nothing at all about which of the two CLIs
		// was chosen — only one may be connected at a time
		// (application/agentcli).
		//
		// Asked here, at the point of dispatch, for the same reason the
		// provider check above is: this is where a task stops being a row and
		// becomes a process. Dispatching without it hands the work to a binary
		// whose session was never proved to exist, and the failure then arrives
		// from inside the CLI — as whatever that program says about its own
		// missing credentials, several layers below the choice that caused it.
		if err := r.requireConnectedCLI(ctx, agentRec); err != nil {
			return fail(err)
		}
		resp, err = r.executor.Execute(runCtx, domain.TaskExecution{
			History:  history,
			Model:    model,
			Provider: agentRec.ProviderType,
			MaxTurns: agentRec.MaxTurns,
			Effort:   agentRec.Effort,
			Policy:   upliftedPolicy,
			// The run's own working directory — the isolated clone with the
			// tt-<key> branch checked out, whenever the repository is in git.
			// It is the same directory the loop's tools are scoped to
			// (ContextWithWorkspaceDir above), so a CLI session and a loop run
			// work in exactly the same tree.
			WorkDir: workDir,
			// The version pins the checkout declares for itself, read off the
			// same directory the session is started in. Nil when it declares
			// nothing, which leaves the session on the host defaults.
			Env: sessionEnv,
			// A previous run on this task that was parked mid-work left its CLI
			// session behind; continuing it is what makes the resume cheaper
			// than a restart.
			ResumeSessionID: resumeCLISession,
			TaskKey:         job.Task.Key,
			TaskTitle:       job.Task.Title,
			// Read off the SAME decision that omitted the skill index from the
			// prompt, several hundred lines up, so the two halves of "the CLI
			// owns skills for this run" cannot come apart: the prompt stops
			// describing the skills and the endpoint stops serving the tool that
			// fetched them, or neither does. Derived from skillDelivery rather
			// than re-deciding from the provider, because a second copy of the
			// condition is how they would drift.
			SkillsOnDisk: skillDelivery == prompt.SkillsOnDisk,
		})
		cliSession.Set(resp.CLISessionID)
	case r.orchSvc != nil:
		resp, err = r.orchSvc.RunSolo(runCtx, triggerMsg, history, model, upliftedPolicy, r.language(runCtx), job.Run.AgentID)
	default:
		// The agent record's own effort/turn cap, so an HTTP-loop board run
		// respects them the same way a host-executed one already does instead
		// of running on the loop's generic defaults.
		// model may be a task_type_models override; this run's own utility
		// calls (history summarization, the wrap-up on a spent budget) stay on
		// the agent's plain configured model. See agent.WithLightModel.
		resp, err = r.agentLoop.RunTask(runCtx, history, model, agentRec.ProviderType, upliftedPolicy,
			agent.WithLightModel(agentRec.Model), agent.WithSessionLimits(agentRec.MaxTurns, agentRec.Effort))
	}
	// Stamp what the tools did before any of the terminal paths write the row.
	// Every one of them — success, failure, out of budget — persists from here,
	// so doing it once is what keeps the KPI counting whole periods instead of
	// only the runs that happened to fail.
	stampToolStats(&run, toolUsage)
	stampTokenUsage(&run, tokenUsage)
	// Everything from here is this run having its say: a verdict on the row, a
	// comment on the task, a commit, a column. A run that was stopped has no say
	// left — the human ended it and its row already reads 'cancelled' — and the
	// quiet is deliberate, not something the row's status guard cleans up after.
	if stopRequested(parent, runCtx) {
		log.Info().Str("run_id", run.ID.String()).Str("task_id", job.Task.ID.String()).
			Msg("board run stopped, leaving the task as it stands")
		return nil
	}
	if err != nil {
		// The subscription behind the local CLI is spent until a known time.
		// Nothing is wrong with the work and there is nothing to retry now, so
		// this is a park, not a failure — the same treatment a held test device
		// gets, and for the same reason: failing here would spend one of the
		// task's three consecutive-failure lives on a billing window.
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

	// The device was taken. Nothing was produced and nothing is wrong, so this
	// is neither a completed run nor a failed one: park the card and let the
	// sweeper start it again when the hardware frees up. Everything downstream
	// keys off resp.Clarification == nil the same way, so parkOnResource
	// borrows that gate by treating both as "no deliverable this run".
	if resp.ResourceBlock != nil {
		r.parkOnResource(ctx, job, agentRec, resp)
	}

	// The build gate belongs to the run that WROTE the code. A reviewer's run is
	// judging someone else's diff: building it, then feeding the errors back for
	// up to N fix rounds, turned the architect into an implementer that then
	// approved its own patch — and bounced the task to in_progress under the
	// reviewer's name. Entry to code_review is already gated on the pipeline;
	// the reviewer reads that result instead of reproducing it.
	// A run the build gate never judged is not thereby broken: the gate is off,
	// or the column/type is one it does not run for. The hand-off keeps its own
	// evidence (a real diff, an executed command, ticked criteria) for those.
	buildVerified := true
	if r.verifyEnabled && taskWorkspace != "" && resp.Clarification == nil && resp.ResourceBlock == nil &&
		job.Task.TaskType != domain.TaskTypeAnaliz && producesADiff(job.Task.Column) {
		var quotaBlock *domain.QuotaBlock
		resp, buildVerified, quotaBlock = r.verifyAndFix(runCtx, job, agentRec, history, resp, model, upliftedPolicy, taskWorkspace)
		if quotaBlock != nil {
			stampToolStats(&run, toolUsage)
			stampTokenUsage(&run, tokenUsage)
			return r.quotaOrPark(ctx, job, run, agentRec, prevRuns, cliSession, quotaBlock, fail)
		}
	}
	// The orchestration verifier's verdict counts for as much as the build gate's.
	// It used to count for nothing here: the plan row was stamped incomplete while
	// the hand-off, which reads only this response, promoted the task anyway — so
	// a card whose own verification panel read FAILED went into code_review and
	// straight through it. Two judges, one hand-off; either one saying no is a no.
	if resp.Verification != nil && !resp.Verification.Passed {
		buildVerified = false
		r.reportPlanVerificationFailure(ctx, job, *resp.Verification)
	}

	// An analiz task's deliverable is a technical understanding of the code. A
	// run that never opened the repository cannot have produced one, whatever
	// its summary says — DE-1's architect wrote a full spec out of eight tool
	// calls, none of which read anything. Fail it here so the reconciler retries
	// instead of leaving an invented analysis on the board as a completed run.
	if resp.ResourceBlock == nil && isUngroundedAnalysis(job.Task, resp, toolUsage) {
		// Assigned to the named err so the deferred activity recorder reports
		// this run as failed too, not just the board row.
		err = r.failRunUngrounded(ctx, job, run, resp)
		return err
	}

	// The same rule from the other end of the board: a QA run's deliverable is
	// an executed test round, and a run that never started the product cannot
	// have produced one. DE-1's QA run wrote its scenario list in the future
	// tense ("I will run these and verify each criterion"), called one board
	// tool, and was stamped completed.
	if resp.ResourceBlock == nil && isUngroundedQA(job.Task, resp, toolUsage) {
		err = r.failRunUngroundedQA(ctx, job, run, resp, ungroundedQAReason)
		return err
	}

	// And the same rule for what the round LOOKED at: on a user-facing repository
	// a QA round that never put the interface in front of itself has not tested
	// the thing the user sees. Commands cannot see a button rendering as a bare
	// "?"; a screenshot can, and the model is on vision.
	if resp.Clarification == nil && resp.ResourceBlock == nil && r.qaSkippedTheUI(ctx, job, toolUsage) {
		err = r.failRunUngroundedQA(ctx, job, run, resp, noUIEvidenceReason)
		return err
	}

	// The same rule again, on PM's own end of the board: a pm_uat run's
	// approval is only worth anything if PM put the product in front of itself,
	// not just read QA's notes and the board.
	if resp.ResourceBlock == nil && isUngroundedPMUAT(job.Task, resp, toolUsage) {
		err = r.failRunUngroundedPMUAT(ctx, job, run, resp, ungroundedPMUATReason)
		return err
	}

	// And the coverage gap this gate exists to close: PM approving a criterion
	// that QA's own case list never actually proves, purely on QA's say-so.
	if resp.Clarification == nil && resp.ResourceBlock == nil && r.pmSkippedCoverageEvidence(ctx, job, run, toolUsage) {
		err = r.failRunUngroundedPMUAT(ctx, job, run, resp, pmUncoveredCriterionReason)
		return err
	}

	// Last call before the work is committed and handed off: the criteria the run
	// did not tick. Runs on the same history, so the agent answers with the work
	// still in context rather than from a cold start on the next event.
	criteriaSettled := true
	if resp.Clarification == nil && resp.ResourceBlock == nil {
		var quotaBlock *domain.QuotaBlock
		resp, criteriaSettled, quotaBlock = r.sweepOpenCriteria(runCtx, job, agentRec, history, resp, model, upliftedPolicy)
		if quotaBlock == nil {
			// The same call for a review run: an implementation run has
			// advanceToCodeReview behind it, a reviewer's only exit is its own
			// move_task and a forgotten one parks the card under a completed run.
			quotaBlock = r.sweepReviewVerdict(runCtx, job, agentRec, history, resp, model, upliftedPolicy)
		}
		if quotaBlock != nil {
			stampToolStats(&run, toolUsage)
			stampTokenUsage(&run, tokenUsage)
			return r.quotaOrPark(ctx, job, run, agentRec, prevRuns, cliSession, quotaBlock, fail)
		}
	}

	if !criteriaSettled {
		// The sweep ran every round and gave up with a criterion still open —
		// this run did not honestly finish, whatever the agent's own closing
		// message says. Failed, not Completed, is what makes the reconciler's
		// existing retry_failed_run path pick the task back up on its own
		// instead of it sitting quietly in this column (see
		// Reconciler.dispatchNeverStarted).
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
	// A run that finished its work behind a green build writes NOTHING on the
	// card any more.
	//
	// It used to publish the agent's closing message here — one comment,
	// gate-approved, instead of the per-round "done" comments the old
	// instruction produced. That fixed the duplication but not the noise: the
	// summary said what the diff, the pull request, the pipeline result and the
	// ticked acceptance criteria already say, on every single task, so the
	// comment thread filled with confirmations of things that went fine and the
	// one comment that mattered — a rejection, a question, a blocker — had to be
	// found among them. The closing message is still required (it is what the
	// build gate and the run summary read); it just stays in the run.
	//
	// A run with something to report still comments, by calling add_task_comment
	// itself: a question it could not answer, work it did not do, a risk for the
	// next person. That is a deliberate act, not a per-run ritual.
	// Same reason the build gate is skipped: a review run produces a verdict,
	// not a commit. Committing from it would put the reviewer's name on the
	// author's branch and push whatever the workspace happened to contain.
	if taskWorkspace != "" && resp.Clarification == nil && resp.ResourceBlock == nil && producesADiff(job.Task.Column) {
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
			// The branch tree just changed; refresh its index asynchronously so
			// the next run retrieves this commit, not the pre-branch snapshot.
			r.branchIndexer.StartIndexBranch(ctx, job.RepositoryID, taskBranch, taskWorkspace)
		}
		// Committed and pushed: whatever this run wrote is now on the branch, which
		// is the only moment the hand-off can be judged on evidence. A run the
		// build gate failed has already been sent back to in_progress with the
		// errors on the card — handing it on as well would contradict that in the
		// same second.
		if buildVerified {
			r.advanceToCodeReview(ctx, job, taskWorkspace, toolUsage)
			r.advanceToAnalizReview(ctx, job, toolUsage)
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

// parkOnResource moves the card into blocked with the resource recorded, so the
// sweeper can find it. A failure to park is logged rather than failed on: the
// run genuinely did nothing, and turning a queueing problem into a failed run
// would spend one of the task's three consecutive-failure lives on it.
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

// parkOnQuota ends a run the local agent CLI could not finish because the
// subscription's usage limit was reached, and leaves behind everything the
// resume needs.
//
// Three writes, in this order, because each one is only useful if the ones
// before it landed:
//
//  1. the run row keeps the CLI session id and the reset time — this is the
//     durable half, so a pod that dies a second later still resumes correctly;
//  2. the card moves to blocked with the quota named — and the move is
//     journalled as a system task.moved — so the board and the task's history
//     both show it waiting on a limit rather than sitting silently in its column;
//  3. the sweeper (quota_sweeper.go) finds it once the reset time has passed.
//
// The run is recorded as completed rather than failed for the same reason the
// resource block is: nothing failed. The summary says what it is waiting for,
// which is what the task detail shows.
func (r *Runner) parkOnQuota(ctx context.Context, job RunJob, run domain.TaskAgentRun, agentRec domain.Agent, block *domain.QuotaBlock, streak int) error {
	resumeAt := block.ResumeAt
	if resumeAt.IsZero() || !resumeAt.After(time.Now().Add(time.Minute)) {
		// An unknown or a stale reset time (already past, or about to be) gets
		// an escalating fallback rather than the flat default: a task on its
		// Nth consecutive park in a row is more likely sitting on a long
		// billing window than freshly hitting a short one, and re-waking it
		// every 30 minutes into the same wall wastes one CLI start per wake.
		resumeAt = time.Now().Add(domain.QuotaParkWindow(streak))
	}
	run.Status = domain.TaskAgentRunStatusCompleted
	run.CLISessionID = block.CLISessionID
	run.QuotaResumeAt = &resumeAt
	run.Summary = truncateHead(fmt.Sprintf("Claude Code usage limit reached — the task resumes automatically after %s. %s",
		resumeAt.Format(time.RFC1123), block.Detail), 500)

	pctx, cancelPersist := persistCtx(ctx)
	defer cancelPersist()
	if _, err := r.runs.Update(pctx, run); err != nil {
		// Without the row there is nothing to resume FROM: the sweeper reads
		// the reset time off it. Fail the run so the reconciler retries the
		// task rather than leaving it parked on a promise nobody recorded.
		return fmt.Errorf("record the claude code quota park: %w", err)
	}

	if r.blocker != nil {
		previous, err := r.blocker.BlockOnResource(pctx, job.RepositoryID, job.Task.ID,
			domain.ResourceClaudeCodeQuota, run.Summary)
		if err != nil {
			// The run row already carries the resume time, so the state is not
			// lost — but an unparked card sits in its working column with no
			// live run, which the reconciler will eventually re-dispatch into
			// the same limit. Logged rather than failed for the reason on
			// parkOnResource: a queueing problem is not the task's fault.
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).
				Msg("parking a task on the claude code usage limit failed")
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
		Msg("task parked on the claude code usage limit")
	return nil
}

// quotaOrPark is the one answer this run gives a *domain.QuotaBlock, wherever
// in the run it surfaces — the main executor call or a follow-up step
// (verify-fix, the criteria sweep, the review sweep) sharing its session.
// Shared so the streak cap and the park itself cannot drift into two
// different answers for the same condition.
//
// block.CLISessionID is defaulted to the run's own holder when the block
// carries none — a follow-up step can hit the limit before the executor ever
// announces a session for that particular call — so the resume still
// continues the session the main call opened rather than starting over.
//
// fail is the run's own failure path (the closure in execute that respects a
// human stop and writes the failed row); it is threaded through rather than
// duplicated so the streak-cap outcome is recorded exactly like any other
// run failure.
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
	// …unless this task has done nothing BUT park. A park costs the task
	// nothing, which is exactly why an endlessly repeating one has to be
	// caught here: the sweeper would resume it, the same thing would park it
	// again, and the card would cycle forever while every individual step
	// looked right. Past the cap it becomes an ordinary failure, so the run is
	// counted, the card stops moving on its own, and a human sees a task that
	// says why.
	streak := quotaParkStreak(prevRuns, run.ID)
	if streak >= maxConsecutiveQuotaParks {
		log.Warn().Str("task_id", job.Task.ID.String()).Int("consecutive_parks", streak).
			Msg("claude code quota park cap reached, failing the run instead of parking it again")
		return fail(fmt.Errorf(
			"this task has parked on the Claude Code usage limit %d times in a row without completing a run (last: %v). "+
				"Failing it instead of parking again: either the subscription has been exhausted for a long stretch, "+
				"or the run keeps reporting a limit it is not actually hitting",
			streak, block))
	}
	return r.parkOnQuota(ctx, job, run, agentRec, block, streak)
}

// maxConsecutiveQuotaParks is how many times in a row one task may park on the
// Claude Code usage limit before the next park becomes a plain failure instead.
//
// It is a livelock brake, not a policy about quotas. Parking is the one outcome
// that costs the task nothing — no failure counted, no attempt spent — so a
// detection that is wrong in a way that REPEATS has nothing to stop it: the
// sweeper resumes the card, the same wrong detection parks it again, and the
// task cycles for as long as the board exists, invisibly, because every
// individual step looks correct. Five is well past any real streak (a genuine
// limit resets in hours, and five real parks means the operator has been out of
// quota for most of a day) and is small enough that a repeating false positive
// surfaces as a failed card the same day instead of never.
const maxConsecutiveQuotaParks = 5

// quotaParkHistoryDepth is how many previous runs are read per run. One more
// than the cap, because the cap counts runs BEFORE the current one.
const quotaParkHistoryDepth = maxConsecutiveQuotaParks + 1

// quotaParkStreak counts how many of the task's most recent runs — newest
// first, current run excluded — parked on the quota, stopping at the first run
// that did anything else.
//
// Consecutive, not total: a task that parked twice months apart with successful
// runs in between is not looping, and counting its whole history would fail a
// task for a limit it kept recovering from.
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

// latestCLISession returns the CLI session this run should CONTINUE, or "" to
// start a fresh one.
//
// Only the task's immediately preceding run counts, and only if it belongs to
// the SAME agent and was parked (QuotaResumeAt set) with a session recorded.
// Three halves, and all of them matter:
//
//   - the same agent, because a column can dispatch to several of them at once.
//     Handing one CLI session id to two runs starts two `claude --resume <same
//     id>` processes in one workspace, editing the same files from two sessions
//     that cannot see each other's writes. A second agent starts fresh;
//
//   - the immediately preceding one, because a resume sends a short "carry on
//     where you stopped" prompt. Handing that to the session of a run that
//     FINISHED — the developer's completed session, when the card has since come
//     back as need_revision — would tell it to continue work it already
//     delivered, instead of telling it what the reviewer asked for;
//
//   - parked, because a park is the only state a continuation makes sense from:
//     the session stopped mid-work with the repository read and the change half
//     written, which is exactly what a restart would pay for twice.
//
// The rows arrive newest first (ListByTask orders by created_at DESC). The
// current run is skipped because it has not written its own id yet — and if it
// had, resuming a session inside the run that owns it is nonsense.
func latestCLISession(prevRuns []domain.TaskAgentRun, currentRunID, agentID uuid.UUID) string {
	for _, prev := range prevRuns {
		if prev.ID == currentRunID {
			continue
		}
		// Another agent's run on the same task: this one has no session here.
		// Deliberately a stop rather than a skip — the run before this one is
		// the only continuable state, whoever it belonged to, and walking past
		// it to an older run of the right agent would resume a session that a
		// newer run has since worked on top of.
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
	// Park the task on this session. Answering there clears the block and
	// re-dispatches the task; until then the board shows it as waiting on a
	// human rather than silently idle, and this worker is free for other jobs.
	//
	// The parked question is the full request — context AND questions. Storing
	// only the context handed the resumed run a preamble instead of the thing
	// that was actually asked.
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

// clarificationSession returns the chat this task asks its questions in. A task
// keeps ONE thread for its whole life: a fresh session per question split the
// exchange across chats, so neither the human nor the next run could see what
// had already been asked and answered, and the same question came back.
//
// A session recorded on the task but since deleted falls back to a new one
// rather than losing the question.
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

// ensureWorkingCopy guarantees rootPath holds a real git working copy OF THIS
// REPOSITORY before an agent is started. Previously the runner called
// os.MkdirAll here, so a stale or never-cloned path silently became an empty
// directory: HasGit was then false, the clone/branch hard gate below was skipped
// entirely, and the agent ran in an empty tree with no way to tell a bug from an
// unbuilt project.
//
// Every repository is expected to live in git. If the working copy is missing we
// restore it from the recorded origin; if there is no origin to restore from,
// the run fails loudly rather than proceeding on nothing.
func (r *Runner) ensureWorkingCopy(ctx context.Context, repo domain.Repository, rootPath string) error {
	if r.git.HasGit(rootPath) {
		// Reusing a checkout is adopting it, so it has to be THIS repository
		// and not merely a repository — the same question the import, the
		// restore and the index mirror now ask before they act on a directory
		// they did not create. Running an agent on someone else's tree is the
		// worst of the four outcomes: it commits and pushes.
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
		// Non-empty but not a repo: cloning into it would fail anyway, and
		// deleting a directory we did not create is not ours to do.
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

// enterWorkingColumn moves a queued task into the column its run actually works
// in, and returns the task as the run should see it.
//
// Two queues, one rule:
//
//	todo         -> in_progress   the assignee's implementation run
//	ready_for_qa -> in_qa         the QA run that was just dispatched
//
// Entering the column was left entirely to the agent's own move_board_task
// call. So the board said `todo` for however long the model spent exploring
// before it got round to that tool — and said it forever when the model never
// did: DE-1 sat in `todo` with a frontend-developer run executing against it,
// which is the one thing the column is supposed to rule out. QA failed the same
// way from the other queue: a run announced "I will take it into in_qa", tested
// nothing, completed, and left the card in `ready_for_qa` — and because the
// review chain counts an in_qa span as the evidence that QA happened, that task
// could never reach done. A board whose truth depends on the model remembering
// a tool call is not a board.
//
// Who may be moved differs per queue, because the two are dispatched
// differently. A `todo` task fans out to every agent the column resolves and
// each is told to take no action if the work is not theirs, so only the
// assignee's own run claims it; claiming on their behalf would hand the task to
// whichever agent the dispatcher reached first, and an unassigned task still
// waits to be claimed, by the agent, deliberately. `ready_for_qa` is a hand-off
// gate column (isHandoffGateColumn): the dispatcher resolves it by column
// subscription, never by assignee — the assignee is still the developer — so
// every run that reaches it is that column's QA agent and moving is right.
//
// The move is attributed to the agent, which is what stops it starting a second
// run: the dispatcher skips the agent whose own tool call produced the event,
// and this move is indistinguishable from the one the agent would have made.
// The run then continues in the column it just entered — the same run, with the
// in_qa instruction, not a fresh QA dispatch.
func (r *Runner) enterWorkingColumn(ctx context.Context, job RunJob) domain.BoardTask {
	task := job.Task
	if r.taskUpdater == nil {
		return task
	}

	var column domain.TaskColumn
	switch task.Column {
	case domain.TaskColumnTodo:
		if task.AssigneeAgentID == nil || *task.AssigneeAgentID != job.Run.AgentID {
			return task
		}
		column = domain.TaskColumnInProgress
	case domain.TaskColumnNeedRevision:
		// A revision run works the code exactly like an in_progress run does, so
		// the card belongs in in_progress while it does. Leaving that move to the
		// agent left the board reading "waiting on the developer" for the whole
		// run — the frontend developer started editing straight out of
		// need_revision and nothing on the board said the work had restarted.
		// The revision framing is not lost: job.EnteredFrom carries it, and the
		// reviewer's comments and the failed pipeline go into the run either way.
		if task.AssigneeAgentID == nil || *task.AssigneeAgentID != job.Run.AgentID {
			return task
		}
		column = domain.TaskColumnInProgress
	case domain.TaskColumnReadyForQA:
		// An analiz task has no QA stage at all; it never reaches this column,
		// and if a human drags one here nothing about it is testable.
		if task.TaskType == domain.TaskTypeAnaliz {
			return task
		}
		column = domain.TaskColumnInQA
	default:
		return task
	}

	agentID := job.Run.AgentID
	updated, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, task.ID, domain.UpdateBoardTaskRequest{
		Column:       &column,
		Actor:        domain.TaskActorAgent,
		ActorAgentID: &agentID,
	})
	if err != nil {
		// The board is wrong, the work is not. Run anyway: the agent's own move
		// still corrects the column, which is exactly where we were before.
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

// taskColumnReader re-reads a task so the hand-off can see the column as it is
// now rather than as the job snapshot remembers it. Optional: repository.Service
// implements it, and a build without it falls back to the snapshot.
type taskColumnReader interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

// advanceToCodeReview hands a finished implementation run's task to review.
//
// It is the other half of enterWorkingColumn, and it exists for the same reason.
// Leaving the exit move to the model produced two failures on DE-1 within one
// afternoon. The plan grew a subtask of its own for it — "Move task to
// code_review" — which costs a model call, its own retries and its own card to
// make one tool call; and nothing verified that call, so the subtask reported
// "completed" twice while the task's history recorded no move at all and the
// card sat in in_progress with a finished branch behind it. A board whose truth
// depends on the model remembering a tool call is not a board.
//
// The guards are what keep this from being a rubber stamp:
//   - Only columns whose exit IS code_review (in_progress, need_revision).
//     A reviewer's run judges someone else's work and moves it on itself; an
//     analiz task has a different chain entirely.
//   - Only with a real diff on the branch. A run that changed nothing has
//     nothing to review, and moving it would open a PR on an empty branch and
//     start a pipeline for it.
//   - Only when the run executed something (domain.ImplementationVerificationTools).
//     A run that wrote code and never built or tested it has not finished the
//     work; handing it to review makes the reviewer's pipeline the first
//     compiler the change ever met.
//   - Only while the task is still where the run found it. An agent that
//     already moved it (or a human who did) has said where the task belongs.
//
// Attributed to the agent, exactly like enterWorkingColumn: the dispatcher skips
// the agent whose own tool call produced a move event, so attributing it to the
// system here would dispatch this same agent again on its own hand-off.
func (r *Runner) advanceToCodeReview(ctx context.Context, job RunJob, taskWorkspace string, usage *registry.ToolUsage) {
	if r.taskUpdater == nil || r.git == nil || taskWorkspace == "" {
		return
	}
	if job.Task.TaskType == domain.TaskTypeAnaliz {
		return
	}
	switch job.Task.Column {
	case domain.TaskColumnInProgress, domain.TaskColumnNeedRevision:
	default:
		return
	}

	diff, diffErr := r.git.TaskDiff(ctx, taskWorkspace)
	if diffErr != nil {
		// Unreadable evidence is not evidence: leave the column alone and let the
		// next run (or the agent's own move) settle it.
		log.Warn().Err(diffErr).Str("task_id", job.Task.ID.String()).Msg("hand-off: task diff unreadable, leaving the column to the agent")
		return
	}
	if strings.TrimSpace(diff) == "" {
		log.Info().Str("task_id", job.Task.ID.String()).Msg("hand-off: run produced no diff, task stays where it is")
		return
	}

	// A diff nobody executed is not finished work. The comment is the point of
	// this branch: the next run reads WHY the card is still in this column and
	// starts by running the build, instead of rediscovering the same wall.
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

	// A diff on a screen nobody looked at. The build gate cannot see what the
	// user sees — a button that renders as a bare "?" compiles perfectly — and
	// the developer prompt has always asked for this, which was not enough: the
	// run that shipped that button never opened the page at all. Same shape as
	// the check above: the evidence is missing, so the work stays where it is
	// with the reason on the card.
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

	column := domain.TaskColumnCodeReview
	agentID := job.Run.AgentID
	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &column,
		Actor:        domain.TaskActorAgent,
		ActorAgentID: &agentID,
	}); err != nil {
		// A gate refusing the move (incomplete acceptance criteria, a disallowed
		// transition) is a real answer about this task, not a runner failure: it
		// goes on the task so the next run reads WHY it is still in_progress
		// instead of discovering the same wall from scratch.
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

// advanceToAnalizReview hands a finished analiz run's task to the human review
// gate, the same way advanceToCodeReview hands an implementation run's task to
// its reviewer.
//
// An analiz task has no automatic hand-off of its own: advanceToCodeReview
// explicitly skips it (its exit is analiz_review, not code_review), so the
// column change from in_progress to analiz_review depended entirely on the
// agent remembering to call move_board_task after writing its spec and plan.
// That is exactly the gap advanceToCodeReview itself was written to close for
// implementation runs — a board whose truth depends on the model remembering a
// tool call is not a board — and analiz work sat in in_progress with a
// finished analysis behind it for the same reason a finished implementation
// used to.
//
// The evidence is add_task_document instead of a diff: an analiz task's
// deliverable is the spec/plan documents attached to the card, not a change to
// the branch, so AnalizDocumentTools is this column's equivalent of
// ImplementationVerificationTools.
func (r *Runner) advanceToAnalizReview(ctx context.Context, job RunJob, usage *registry.ToolUsage) {
	if r.taskUpdater == nil {
		return
	}
	if job.Task.TaskType != domain.TaskTypeAnaliz {
		return
	}
	switch job.Task.Column {
	case domain.TaskColumnInProgress, domain.TaskColumnNeedRevision:
	default:
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

	column := domain.TaskColumnAnalizReview
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

// stampToolStats copies the run's tool counters onto the row about to be
// written. Nil-safe: an unmeasured run stores zeros, which reads as "no data"
// rather than "no errors" because the KPI ignores runs with no tool calls.
func stampToolStats(run *domain.TaskAgentRun, usage *registry.ToolUsage) {
	calls, failures := usage.Totals()
	run.ToolCalls = calls
	run.ToolErrors = failures
	run.ErrorPattern = truncateHead(renderFailurePattern(usage.Failures()), 500)
}

// stampTokenUsage copies the run's accumulated token spend onto the row, the
// same once-for-every-terminal-path spot stampToolStats uses. Nil-safe: an
// unmeasured run stores zeros.
func stampTokenUsage(run *domain.TaskAgentRun, usage *usageapp.TokenUsage) {
	t := usage.Totals()
	run.LLMCalls = t.LLMCalls
	run.PromptTokens = t.PromptTokens
	run.CompletionTokens = t.CompletionTokens
	run.CacheReadTokens = t.CacheReadTokens
	run.CacheWriteTokens = t.CacheWriteTokens
}

// renderFailurePattern names the tools that failed, worst first. This is the
// sentence the NEXT run reads, so it is written as advice about tools rather
// than as a count of errors.
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

// previousRunFailuresMessage tells a run which tools kept failing the last time
// this task was attempted.
//
// Nothing used to cross that boundary. A run that burned its whole budget on a
// tool that rejected every call handed the next run a fresh context and no
// warning, and the next run reached for the same tool immediately. The board
// branch carried the code forward; this carries the lesson.
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
			// The task (and its runs, by cascade) was deleted while the agent
			// was working — a human closing a task mid-run, not a failure.
			log.Info().Str("run_id", run.ID.String()).Msg("run row gone, task deleted mid-run")
			return nil
		}
		return updateErr
	}
	return err
}

// isUngroundedAnalysis reports an analiz run that answered without reading the
// repository. A run that ended in a question is exempt — asking IS the answer,
// and blocking it would trade an invented analysis for an unanswerable loop.
// A nil tracker means the run was never measured (chat, tests) and never blocks.
func isUngroundedAnalysis(task domain.BoardTask, resp domain.AgentResponse, usage *registry.ToolUsage) bool {
	if task.TaskType != domain.TaskTypeAnaliz || resp.Clarification != nil || usage == nil {
		return false
	}
	return !usage.UsedAny(domain.CodeExplorationTools...)
}

// failRunUngrounded ends an analiz run that produced an answer without reading
// the repository. The agent's own text is kept in the comment (it may contain a
// usable question or assumption) but is not published as the analysis, and the
// run is marked failed so the reconciler dispatches a retry — which starts with
// the same task and, this time, the explicit reason the last attempt was
// rejected.
func (r *Runner) failRunUngrounded(ctx context.Context, job RunJob, run domain.TaskAgentRun, resp domain.AgentResponse) error {
	const reason = "Analysis rejected: the run never read the repository " +
		"(no codebase_search / grep_code / get_repo_tree / get_symbol_skeleton / expand_symbol_context call succeeded). " +
		"An analiz answer must name real files and interfaces from the code, not assumed ones."

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

// ErrUngroundedAnalysis reports an analiz run whose output was rejected because
// the run never read the repository.
var ErrUngroundedAnalysis = errors.New("analiz run produced no repository exploration")

// isUngroundedQA reports a QA run that reported on a product it never ran.
//
// Only the QA columns are judged, and only for work that has something to run:
// an analiz task has no QA phase, and a run that ended in a question is exempt
// for the same reason the analiz gate exempts one — asking IS the answer. A nil
// tracker means the run was never measured and never blocks.
//
// review_criterion is not evidence here (see domain.QAExecutionTools): a run
// that approves criteria without executing anything is precisely the failure,
// and letting the claim count as its own proof would reopen it.
func isUngroundedQA(task domain.BoardTask, resp domain.AgentResponse, usage *registry.ToolUsage) bool {
	if task.TaskType == domain.TaskTypeAnaliz || resp.Clarification != nil || usage == nil {
		return false
	}
	switch task.Column {
	case domain.TaskColumnInQA, domain.TaskColumnReadyForQA:
	default:
		return false
	}
	return !usage.UsedAny(domain.QAExecutionTools...)
}

// qaSkippedTheUI reports a QA round on a user-facing repository that never
// looked at the interface.
//
// It is the gap isUngroundedQA leaves open: run_terminal satisfies that check,
// so a web task could be "tested" with a build and a test command and every
// criterion approved without a single frame of the running page reaching the
// model. The agents run on vision-capable models — the miss was never the
// model's, because no image was ever captured for it to look at. A store button
// that renders as a bare "?" is invisible to `npm run build`.
func (r *Runner) qaSkippedTheUI(ctx context.Context, job RunJob, usage *registry.ToolUsage) bool {
	if usage == nil || r.projects == nil {
		return false
	}
	switch job.Task.Column {
	case domain.TaskColumnInQA, domain.TaskColumnReadyForQA:
	default:
		return false
	}
	if usage.UsedAny(domain.UIObservationTools...) {
		return false
	}
	return r.uiRepo(ctx, job.RepositoryID)
}

// uiRepo reports whether this repository's work is work on a screen. Unresolved
// is false: an unknown kind is not evidence of anything, and failing runs over a
// lookup error would punish the repository for the resolver's bad minute.
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

// The two ways a QA round can fail to be one. Both are rejections of the RUN,
// not of the work under test: the retry is told exactly what was missing so it
// executes instead of re-planning.
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

// failRunUngroundedQA ends a QA run that produced a verdict, or a promise of
// one, without executing anything. Written to the task like the analiz
// rejection: the run's own text is kept (the scenario list in it is usually
// sound and the retry can execute exactly that), it is marked failed so the
// reconciler dispatches another QA attempt, and the reason is on the task so
// the retry starts knowing what was rejected instead of repeating it.
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

// ErrUngroundedQA reports a QA run whose verdict was rejected because the run
// never executed the product it was judging.
var ErrUngroundedQA = errors.New("QA run executed nothing")

// isUngroundedPMUAT is isUngroundedQA's counterpart for the PM's own sign-off
// column: a pm_uat run's approval is only evidence if PM put the product in
// front of itself. PM has no shell, so PMUATExecutionTools holds only the
// browser/mobile calls — a run whose entire ledger is list_test_cases,
// read_file and review_criterion calls has approved criteria from QA's notes
// and the board alone, never from touching the running product.
func isUngroundedPMUAT(task domain.BoardTask, resp domain.AgentResponse, usage *registry.ToolUsage) bool {
	if resp.Clarification != nil || usage == nil {
		return false
	}
	if task.Column != domain.TaskColumnPMUAT {
		return false
	}
	return !usage.UsedAny(domain.PMUATExecutionTools...)
}

// pmSkippedCoverageEvidence closes the gap isUngroundedPMUAT leaves open: a PM
// run can pass that gate with a single browser_navigate call and still approve
// a DIFFERENT criterion purely on QA's say-so, one QA never actually proved
// with a passed test case. It reads the task's own current criteria and test
// cases and defers the decision to pmApprovedUncoveredCriterion, scoped to
// approvals this run itself recorded — run.CreatedAt predates any check this
// run's own review_criterion calls could have written, so a check timestamped
// after it is this run's, not a settled earlier one.
func (r *Runner) pmSkippedCoverageEvidence(ctx context.Context, job RunJob, run domain.TaskAgentRun, usage *registry.ToolUsage) bool {
	if usage == nil || job.Task.Column != domain.TaskColumnPMUAT {
		return false
	}
	criteria := r.allCriteria(ctx, job)
	testCases := r.taskTestCases(ctx, job)
	return pmApprovedUncoveredCriterion(job.Task, criteria, testCases, usage, run.CreatedAt)
}

// pmApprovedUncoveredCriterion reports whether this run's PM checks include an
// approval that QA's own recorded test round never backs with a passed case —
// and, if so, whether this run made up for that with its own execution
// evidence.
//
// Only PM checks recorded after runStartedAt count as "this run's" approval:
// a criterion PM approved (correctly, with evidence) in an earlier run must
// not retrigger this gate for a later, unrelated run that never touched it.
//
// review_criterion is not counted as evidence of coverage: a PM approval is
// the CLAIM this gate exists to check, not its own proof. A criterion is
// "covered" only when some domain.TaskTestCase carries the matching
// CriterionID and TestCaseStatusPassed — QA's executed round, not PM's verdict
// about it.
func pmApprovedUncoveredCriterion(
	task domain.BoardTask,
	criteria []domain.AcceptanceCriterion,
	testCases []domain.TaskTestCase,
	usage *registry.ToolUsage,
	runStartedAt time.Time,
) bool {
	if usage == nil || task.Column != domain.TaskColumnPMUAT {
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

// pmApprovedThisRun reports whether a criterion carries an approved PM verdict
// recorded after runStartedAt — i.e. by the run currently being judged, not a
// settled approval from an earlier one.
func pmApprovedThisRun(c domain.AcceptanceCriterion, runStartedAt time.Time) bool {
	for _, check := range c.Checks {
		if check.Role == domain.CriterionReviewRolePM && check.Approved && check.CheckedAt.After(runStartedAt) {
			return true
		}
	}
	return false
}

// taskTestCaseReader reads a task's recorded test round for the coverage-gap
// gate. Optional, like taskCriteriaReader: repository.Service implements it
// under the name ListTestCases, and a build without it simply skips the gate.
type taskTestCaseReader interface {
	ListTestCases(ctx context.Context, taskID uuid.UUID) ([]domain.TaskTestCase, error)
}

// taskTestCases reads every test case recorded on the task, or nil when the
// store is not wired.
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

// failRunUngroundedPMUAT ends a pm_uat run that produced an approval without
// executing anything, mirroring failRunUngroundedQA: the run's own text is
// kept (its walk-through plan is usually sound and the retry can execute
// exactly that), the run is marked failed so the reconciler dispatches another
// attempt, and the reason is on the task so the retry starts knowing what was
// rejected.
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

// ErrUngroundedPMUAT reports a pm_uat run whose approval was rejected because
// the run never checked the product it was judging.
var ErrUngroundedPMUAT = errors.New("pm_uat run approved without execution evidence")

// failRunOutOfBudget ends a run that spent its iteration budget. The work the
// agent already produced is committed and summarised rather than discarded:
// the retry then starts from that branch (execute feeds the branch diff back
// in) instead of from zero, and the summary says what actually happened —
// "30 iterations, 41 tool calls, 12 of them repeats" is a diagnosis, whereas
// the bare "exceeded maximum iterations" was not.
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

// taskComments reads the task's comment history for context injection. Empty
// on any failure — missing context degrades a run, a failed lookup must not
// end it.
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

// revisionCommentsMessage renders the most recent comments on a need_revision
// task so the dispatched agent can read WHAT to revise. Answered clarifications
// are left out — they are injected in full by their own message, and letting
// them fill this window would push the reviewer's actual feedback out of it.
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
	// Latest 5 — the reviewer's hand-back is by definition near the end.
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

// taskCriteriaReader reads a task's acceptance criteria for the run context.
// Optional, like taskColumnReader: repository.Service implements it, and a
// build without it simply runs without the criteria block.
type taskCriteriaReader interface {
	ListTaskCriteria(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error)
}

// analysisReader resolves the analiz tasks an implementation task was opened out
// of, with their documents. Optional in the same way, and asked as ONE question
// on purpose: the runner must not have to know that provenance is stored as a
// derived_from relation, or that an analysis's deliverable is a task document.
type analysisReader interface {
	AnalysisReferences(ctx context.Context, taskID uuid.UUID) ([]domain.AnalysisReference, error)
}

// analysisContextLimit bounds how much of one analysis document is injected.
//
// Generous — a spec and a plan are what this run is meant to implement, and
// truncating them to a summary would recreate the problem this whole path
// exists to solve. It is a ceiling against a document somebody pasted a
// database dump into, not a budget: the truncation note names the tool that
// reads the rest.
const analysisContextLimit = 12000

// analysisContext renders the analysis behind this task, or "" when there is
// none (which is every task nobody derived from an analiz).
//
// It follows the PR/diff blocks rather than inventing anything: a system message
// assembled before the loop starts, placed with this run's own evidence, skipped
// entirely when empty. What makes it different from those is that it is not a
// convenience — since an analysis stopped committing its spec to the repository,
// this block and list_task_documents are the ONLY two ways the implementer ever
// sees the specification written for its task.
func (r *Runner) analysisContext(ctx context.Context, job RunJob) string {
	reader, ok := r.taskUpdater.(analysisReader)
	if !ok {
		return ""
	}
	refs, err := reader.AnalysisReferences(ctx, job.Task.ID)
	if err != nil {
		// Degrades the run, does not end it — the same rule every other context
		// lookup here follows.
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

// openCriteria returns the criteria still unticked on this task.
//
// They belong in the trigger message because the criteria gate
// (repository.Service.criteriaGate) blocks the automatic hand-off to
// code_review while any of them is open, and the implementer had no way to see
// them: the task snapshot carries title and description only, so a run finished
// the work, ended, and the hand-off was refused with "N acceptance criteria
// incomplete" for criteria the agent was never shown. Listing them with their
// IDs makes set_criterion_completed a call the agent can actually make.
func (r *Runner) openCriteria(ctx context.Context, job RunJob) []domain.AcceptanceCriterion {
	items := r.allCriteria(ctx, job)
	open := make([]domain.AcceptanceCriterion, 0, len(items))
	for _, c := range items {
		// Cancelled counts as settled here for the same reason it does in the
		// gate: it carries a written reason, so re-asking about it would drive
		// the sweep loop over a decision that was already made and explained.
		if !c.Settled() {
			open = append(open, c)
		}
	}
	return open
}

// allCriteria reads every criterion on the task, ticked or not.
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

// criteriaForRun is the criteria list this run has to be shown: everything for
// a reviewing run, the still-open ones for an implementing one.
//
// The distinction is the whole point. An implementer only needs what is left to
// do. A reviewer needs the ids — and by the time QA or the PM is dispatched the
// developer has ticked every criterion, so the open list is EMPTY and the
// prompt carried no criterion ids at all. QA then could not call
// review_criterion (it takes a uuid), tried the criterion text and was told
// "invalid criterion_id", and pushed the task to need_revision hoping the ids
// would surface somewhere — restarting a review cycle that had nothing wrong
// with it. The ids are now in the prompt for exactly the roles that must quote
// them back.
func (r *Runner) criteriaForRun(ctx context.Context, job RunJob) []domain.AcceptanceCriterion {
	if listsEveryCriterion(job.Task) {
		return r.allCriteria(ctx, job)
	}
	return r.openCriteria(ctx, job)
}

// listsEveryCriterion reports whether this run's prompt shows every criterion
// with its ids and its verdicts rather than only the unticked ones.
//
// It is deliberately wider than isReviewColumn: ready_for_qa, in_qa and
// human_uat are not "review columns" for the runner's build/commit decisions,
// but they are the columns whose run records verdicts, and a verdict needs the
// id. Analiz tasks are excluded — their criteria are what the analysis must
// answer, ticked by the run that answers them, and that behaviour is unchanged.
func listsEveryCriterion(task domain.BoardTask) bool {
	if task.TaskType == domain.TaskTypeAnaliz {
		return false
	}
	switch task.Column {
	case domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskColumnInQA,
		domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT:
		return true
	default:
		return false
	}
}

// standingCriteriaMessage states the criteria every code task carries whether or
// not anyone wrote them on the card.
//
// Nobody types "and it should compile" into an acceptance criterion, so the
// board's criteria are only ever the feature-specific half — and a run that
// satisfied all of them could still leave the branch red, or add a whole
// behaviour with no test in sight, and read as complete. These three are
// enforced anyway (the post-run gate builds, runs the suite and measures
// coverage), so stating them costs nothing and stops the agent discovering them
// as a surprise after it has already declared itself done.
//
// They are NOT rendered for analiz tasks: an analysis produces documents, has no
// build to keep green, and telling it to write unit tests is telling it to do
// the implementer's job on the wrong task.
func standingCriteriaMessage(task domain.BoardTask) string {
	if task.TaskType == domain.TaskTypeAnaliz {
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

// reviewCriteriaHeader introduces the full criteria list to the role that has to
// pass judgement on it.
//
// The QA/PM wording says three things the transcripts show were all missing:
// every id needs a verdict, the verdict is recorded with review_criterion, and
// the forward move stays refused until they all have one. That refusal is
// criteriaReviewGate, which only runs where require_criteria_complete is set, so
// the sentence names that condition rather than promising a gate half the
// repositories do not have — and still asks for every verdict, because a review
// history is worth having on the repositories nothing forces.
// The last sentence is
// there because the observed workaround was worse than the gap — a QA run with
// no ids in its prompt moved the task back to need_revision "to surface the
// criteria", which sends finished work back to a developer who has nothing to
// fix.
func reviewCriteriaHeader(column domain.TaskColumn) string {
	switch column {
	case domain.TaskColumnReadyForQA, domain.TaskColumnInQA:
		return "\nAcceptance criteria — record YOUR verdict on EACH id below with review_criterion " +
			"(approve only what you executed and observed; reject with expected-vs-actual). " +
			"The forward move is refused while any id lacks your verdict on repositories that require criteria — record them all regardless. " +
			"Never move the task to need_revision just to look for these ids: they are here.\n"
	case domain.TaskColumnPMUAT:
		return "\nAcceptance criteria — record YOUR OWN PM verdict on EACH id below with review_criterion " +
			"(approve only what executed evidence and your own check on stage cover; reject naming the gap). " +
			"The developer's checkmark and QA's check are not your verdict. " +
			"The forward move is refused while any id lacks your verdict on repositories that require criteria — record them all regardless. " +
			"Never move the task to need_revision just to look for these ids: they are here.\n"
	case domain.TaskColumnCodeReview:
		return "\nAcceptance criteria the diff must satisfy (ids for reference):\n"
	default:
		return "\nAcceptance criteria of this task (ids for reference):\n"
	}
}

// criterionStateLine renders one criterion as id, text and who has said what
// about it. A reviewer that cannot see an existing verdict either re-does work
// another role already recorded or reads a ticked box as an approval.
func criterionStateLine(c domain.AcceptanceCriterion) string {
	implementer := "not ticked"
	if c.Completed {
		implementer = "ticked"
	}
	return fmt.Sprintf("- [%s] %s — implementer: %s; qa: %s; pm: %s\n",
		c.ID, c.Text, implementer,
		criterionVerdictLabel(c, domain.CriterionReviewRoleQA),
		criterionVerdictLabel(c, domain.CriterionReviewRolePM))
}

func criterionVerdictLabel(c domain.AcceptanceCriterion, role domain.CriterionReviewRole) string {
	for i := range c.Checks {
		if c.Checks[i].Role != role {
			continue
		}
		if c.Checks[i].Approved {
			return "approved"
		}
		if note := strings.TrimSpace(c.Checks[i].Note); note != "" {
			return "rejected (" + note + ")"
		}
		return "rejected"
	}
	return "—"
}

// criteriaMessage renders the open criteria as the checklist the run is judged
// against. Only implementers are told to tick them: for QA and PM the tick is
// the developer's claim to verify, not theirs to make — they record their own
// verdict with review_criterion.
func criteriaMessage(task domain.BoardTask, criteria []domain.AcceptanceCriterion) string {
	var sb strings.Builder
	sb.WriteString(standingCriteriaMessage(task))
	if len(criteria) == 0 {
		return sb.String()
	}
	if listsEveryCriterion(task) {
		sb.WriteString(reviewCriteriaHeader(task.Column))
		for _, c := range criteria {
			sb.WriteString(criterionStateLine(c))
		}
		return sb.String()
	}
	sb.WriteString("\nOpen acceptance criteria (these define \"done\" for this task):\n")
	for _, c := range criteria {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.ID, c.Text))
	}
	// An analiz task has no automatic hand-off to gate on: its criteria are what
	// the analysis must answer, ticked when the documents answer them.
	if task.TaskType == domain.TaskTypeAnaliz {
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

func buildTriggerMessage(job RunJob, criteria []domain.AcceptanceCriterion) string {
	taskJSON, _ := json.Marshal(map[string]interface{}{
		"task_id": job.Task.ID.String(),
		// task_key is the short handle every board tool also accepts. Without it
		// the snapshot carried the UUID only, and the tool schemas advertise
		// "or its board key (e.g. \"T-1\")" — so a run that did not want to copy a
		// UUID reached for the example and commented on T-1 while working on T-2.
		"task_key": job.Task.Key,
		"title":    job.Task.Title,
		// repository_id is the uuid every repository-scoped tool wants. Without
		// it in the snapshot the only repository identifier a run could see was
		// the workspace's name, so get_deploy_target was called with
		// "agent-server" and answered "invalid repository_id" — with nowhere to
		// look the name up.
		"repository_id": job.RepositoryID.String(),
		// task_type is what says whether this run produces CODE or a DOCUMENT.
		// Without it the snapshot was title/description/column only, so an
		// analiz task whose description did not spell out "this is an analysis"
		// read exactly like an implementation task: the architect was dispatched
		// on one, edited and deleted files in the repo, and reported the feature
		// as built — on a task whose deliverable was a spec and a plan.
		"task_type":   string(job.Task.TaskType),
		"description": job.Task.Description,
		"column":      string(job.Task.Column),
		"event_type":  string(job.Event.EventType),
		"payload":     json.RawMessage(job.Event.Payload),
	})
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
1. Build and test what you changed with run_terminal, and read the output. Red output is fixed in this run, not reported as done.
2. Every acceptance criterion you satisfied: set_criterion_completed with its id — ticked only after step 1 showed it working.
3. Every criterion you did NOT satisfy: leave it open and say why in a comment.
%s
A run that skips any of 1-3 has its hand-off refused and its finished work parked in this column.

Task snapshot:
%s
%s`, runInstruction(job), closingStep(job), string(taskJSON), criteriaMessage(job.Task, criteria))
}

// closingStep is step 4 of the pre-finish checklist. For a run whose hand-off
// the build gate judges, the completion comment is NOT the agent's to post: the
// gate runs after the loop has stopped talking, so a comment written from inside
// the run announces a success nothing has verified yet — T-11 collected four
// "done" comments this way, every one of them ahead of a red verification. The
// runner publishes the closing summary itself once the gate passes. Review and
// analiz runs keep the old instruction: their comment IS the deliverable
// (a verdict, a hand-off note) and the build gate never judges them.
func closingStep(job RunJob) string {
	if job.Task.TaskType == domain.TaskTypeAnaliz || !producesADiff(job.Task.Column) {
		return "4. One add_task_comment with what you changed and the command output that verified it."
	}
	return "4. Do NOT post an add_task_comment announcing completion. Close with your final message instead — what you changed and the command output that verified it. " +
		"The system re-runs build verification after you stop and publishes that summary to the card only once the gate passes; " +
		"a completion comment posted from inside the run can claim success the gate is about to refute."
}

// runInstruction picks the column whose instruction this run should read.
//
// A revision run is moved into in_progress before it starts (enterWorkingColumn),
// and the in_progress instruction says nothing about feedback: the run that was
// dispatched to fix a review read "continue the work" and went straight back to
// coding without looking at what the reviewer wrote. The card's column is the
// truth for the board; the column the run was dispatched from is the truth about
// what the run is for.
func runInstruction(job RunJob) string {
	task := job.Task
	// Only need_revision: the todo -> in_progress move is the one whose old
	// instruction ("claim it and move it to in_progress") is exactly the
	// duplicate step the automatic move exists to delete.
	if job.EnteredFrom == domain.TaskColumnNeedRevision {
		task.Column = domain.TaskColumnNeedRevision
	}
	return columnInstruction(task)
}

// verifyBeforeFinishing is the step the implementer instruction never named.
//
// The hand-off has always spoken of "a green build", but nothing ever told the
// agent to produce one: runs ended with edited files, ticked criteria and a
// summary saying the change "should work", and the pull request's pipeline was
// the first time the code was compiled. The rule is written as commands to run
// rather than a quality reminder, because "verify your work" is what the runs
// that failed this already believed they had done.
const verifyBeforeFinishing = "Before you finish, RUN the code you wrote: build it and run the tests with run_terminal " +
	"(the project's own commands — check package.json / Makefile / go.mod / the README if you do not know them) and READ the output. " +
	"Writing a file is not verifying it and neither is reading it back; \"it should work\" is not a result. " +
	"A red build or a failing test is yours to fix in this same run — never hand off red work. " +
	"If the change cannot be executed here (missing service, no credentials), say exactly that in your closing comment " +
	"and name what you did check instead. A run whose ledger holds no executed command is treated as unverified and is not handed on."

// handoffIsAutomatic is the closing sentence for every column an implementer
// works in. The exit move belongs to the control plane
// (Runner.advanceToCodeReview): telling the agent to make it is what put a
// "Move task to code_review" step in the plan — a whole subtask, with its own
// model call and its own retries, for one tool call that nothing verified.
const handoffIsAutomatic = verifyBeforeFinishing +
	" Do NOT move the task yourself and never plan a step for the move: when this run ends with a green build and a real diff on the branch, " +
	"the system moves the task to code_review, opens the pull request and starts the pipeline. " +
	"That move is REFUSED while an acceptance criterion is still open, so tick each one you satisfy with set_criterion_completed inside the step that satisfied it " +
	"(list_acceptance_criteria gives you the ids if they are not in your context). " +
	"Your run is finished when the work is done and verified — close it with a final MESSAGE saying what you changed and how you verified it. " +
	"That message is your report to the system, NOT a card comment: a run that went green writes nothing on the task, because the diff, the pull request, the pipeline result and the ticked criteria already say it. " +
	"Use add_task_comment only for something the next person has to act on — a question you could not answer yourself, a part of the task you did not do and why, a risk or a follow-up somebody must pick up."

// analizProducesDocuments is the closing sentence for every column an analiz
// task is worked in. It states the deliverable and the two things the run must
// not do — write to the repo, and hand itself to code_review — because both
// were what the column-only instruction told it to do.
const analizProducesDocuments = "Your deliverable is a SPEC and an IMPLEMENTATION PLAN attached to this task with add_task_document, " +
	"grounded in code you actually read (get_repo_tree, codebase_search, grep_code, get_symbol_skeleton, expand_symbol_context) — " +
	"a document attached by a run that explored nothing is rejected and the run is failed. " +
	"If this task already carries a spec or a plan — a revision pass, a need_revision bounce, a change the human asked for — rewrite THAT document with update_task_document instead of attaching another one: the card must end with one current spec and one current plan. " +
	"Never write, edit, move or delete a file in the repository and never commit: an analysis produces documents, not a diff, " +
	"and there is no automatic hand-off to code_review for this task type — a run that ends with file edits has done the implementer's job on the wrong task. " +
	"Finish with a summary comment (approach, the document titles, the task split you intend), then STOP: " +
	"when this run ends with a document attached, the system moves the task to `analiz_review` for you — do NOT move it yourself and never plan a step for the move — " +
	"the human approves there, and no implementation task is created before they do."

// taskTypeInstruction states what the run must produce when the task type — not
// the column — decides it. It returns "" for the types and columns where the
// column alone is the whole story, so columnInstruction's switch stays the
// default path for task/bug work.
//
// Only analiz differs today: it is the one type whose deliverable is a document
// rather than a diff, whose exit gate is analiz_review rather than code_review,
// and whose run has no automatic hand-off behind it (advanceToCodeReview returns
// early for it, and the post-run commit is skipped) — so an analiz run that
// followed the implementer instruction produced nothing the board could carry.
func taskTypeInstruction(task domain.BoardTask) string {
	if task.TaskType != domain.TaskTypeAnaliz {
		return ""
	}
	switch task.Column {
	case domain.TaskColumnTodo:
		return "This is an ANALIZ task (task_type=analiz) in `todo` — an ANALYSIS, not an implementation. If it is not relevant to your role, take no action. " +
			"If it is: claim it and move it to in_progress as the opening action of the step that does the analysis (never a step of its own), " +
			"then investigate in this same run — clone/pull every repository the task names, read the relevant code, and decide WHAT is needed and WHERE. " +
			analizProducesDocuments
	case domain.TaskColumnInProgress:
		return "This is an ANALIZ task (task_type=analiz) ALREADY claimed and ALREADY in `in_progress` — an ANALYSIS, not an implementation, " +
			"and the move you might be tempted to plan first has happened. Continue the investigation from where it stands and finish it in this run. " +
			analizProducesDocuments
	case domain.TaskColumnNeedRevision:
		return "This is an ANALIZ task (task_type=analiz) in `need_revision`: the human rejected the analysis. Their comment is in the task comments in your context. " +
			"Revise the spec/plan at the ROOT of the concern — re-read the code where you are unsure — and attach the corrected documents. " +
			"Create no implementation task from a rejected analysis. " + analizProducesDocuments
	case domain.TaskColumnAnalizReview:
		return "This is an ANALIZ task (task_type=analiz) in `analiz_review`: it is waiting on a HUMAN to approve or reject the spec/plan. " +
			"Nothing is yours to do here — do not move it, do not rewrite the documents, and do not create implementation tasks. Take no action."
	case domain.TaskColumnDone:
		return "This is an ANALIZ task (task_type=analiz) the human moved to `done` — that move IS the approval of your spec and plan. " +
			"Now decompose it: one implementation task per repository and per layer, each with its own plan slice, testable acceptance criteria and an assignee " +
			"(call list_team for the roster; order them by dependency — backend API before the frontend/mobile that consumes it). " +
			"Write no code yourself. List the created tasks in a comment and move this analiz task to `released` as the last action of the step that created them."
	default:
		return ""
	}
}

// qaExecutionInstruction is the how of a QA round, shared by both QA columns.
//
// Without it the instruction said "run the tests" and left the environment to
// the agent, and a round on a web task opened with get_repo_tree, read four
// files under src/app and reported a verdict read off the source. That is a
// second code review, done by the role whose whole purpose is that somebody
// finally runs the thing. Naming the environment first — stage if the task has
// one, otherwise boot the branch locally — is what makes the first tool call an
// execution instead of a file read.
//
// The port and PID sentences are there because QA rounds overlap: two rounds
// on the default dev port test each other's build, and a round that cleaned up
// with `pkill -f vite` ended every CLI session whose command line said "vite".
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
	"rejected and the round is failed."

// columnInstruction states what is left to do FROM the column the task is
// actually in, instead of reciting the whole todo -> in_progress -> code_review
// lifecycle every time.
//
// The lifecycle recital is what produced the duplicate step: a task that had
// already been claimed and moved to in_progress by an earlier run got the same
// "claim it, move it to in_progress" preamble, the planner read that as the
// first deliverable, and the plan opened with a step to move the task into the
// column it was already in — which the step then filled with unrelated work
// because there was nothing else for it to do.
func columnInstruction(task domain.BoardTask) string {
	// Type before column: the same column means different work for different
	// task types. `in_progress` on a task/bug is "write the code"; on an analiz
	// it is "read the code and write the spec". Reading the column alone is what
	// handed an analysis run the implementer instruction — implement it in this
	// run, the system will open your PR — and the architect duly edited and
	// deleted repo files for a deliverable that is a document.
	if s := taskTypeInstruction(task); s != "" {
		return s
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
		// Without this the generic default told the reviewer to "do the work
		// this column asks of your role", and a reviewer reads that as: clone
		// it, build it, run the tests, poke the app. It spent whole runs
		// reproducing a pipeline that had already run, and never got to the
		// diff it was dispatched for.
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
		// Only reachable when enterWorkingColumn's automatic ready_for_qa ->
		// in_qa move was refused: every other QA run reads the in_qa branch
		// below, because the column is already in_qa by the time the prompt is
		// built. ready_for_qa is the hand-off queue (entering it triggers the
		// pipeline and the stage deploy); in_qa is where testing happens, and
		// the review chain reads the in_qa span as the evidence QA ran at all.
		return "This task is still in `ready_for_qa`: the automatic move into in_qa did not go through, " +
			"so testing has NOT started and the board does not show this task as under test. " +
			"Move it to in_qa yourself as the opening action of your first testing step (not as a step of its own), " +
			"then run the tests in this same run. " + qaExecutionInstruction +
			" Record your own verdict on each acceptance criterion with " +
			"review_criterion as you verify it — approve only what you executed, reject with a note saying what failed. " +
			"Finish from in_qa: every acceptance criterion passes → " +
			"move it to pm_uat, with the evidence in the review_criterion notes and NO comment on the card — a pass writes nothing; any criterion fails → move it to need_revision " +
			"and comment the numbered expected-vs-actual per failure."
	case domain.TaskColumnInQA:
		// The normal QA instruction: a task dispatched from ready_for_qa is
		// moved here by the runner before this prompt is built, so planning a
		// move into in_qa would be planning something already done.
		return "This task is ALREADY in `in_qa` — testing is under way and the move you might plan first has happened. " +
			qaExecutionInstruction +
			" Continue and finish the scenarios in this run, recording your verdict per acceptance criterion with " +
			"review_criterion (approve what you executed and observed; reject with an expected-vs-actual note), then " +
			"leave the column: all criteria pass → pm_uat, evidence in the criterion notes and no comment on the card (a pass is not news); " +
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
		// A run only reaches this branch through the dispatcher's merge wake —
		// a task/bug landing in done with an unmerged pull request, dispatched
		// to the QA agent and to nobody else (see doneMergeWake). The words
		// matter more here than anywhere else in this switch: the generic
		// default below ends with "then move the task on to the next column",
		// and the next column after done is `released`. That sentence, handed
		// to an agent sitting on a finished task, is exactly how done tasks
		// drifted into released with no deploy behind them — the failure the
		// done column was closed to dispatch for in the first place.
		return "This task is in `done`: the board has signed it off and its work is finished. " +
			"You are here to LAND the change and then to watch what production does with it — nothing else. " +
			"1) If the pull request is not merged yet: read it (get_task_pull_request) and the pipeline result (get_pipeline_status) — the checks must be green and " +
			"the PR must still be at the commit that was verified — then call merge_task_pull_request, which squash-merges it and deletes the task branch. " +
			"Do NOT retry a refusal and do NOT work around it, and do not comment that the merge worked when it did: the merge commit is recorded on the card by the tool itself. " +
			"A refusal that names a CONFLICT with the base branch (`dirty`) or a branch the base has moved past (`behind`) is the developer's to fix, not yours: " +
			"move the task to need_revision with that reason and stop. Any other refusal (a closed PR, a head commit that is not the verified one, an incomplete review chain) " +
			"means the change is not the change that was approved: put the reason on the task with add_task_comment and stop, because only a human or a new round of review can settle it. " +
			"2) Once it is merged (or if it already was): call get_task_deploy_status. It reports what production did with THAT commit. " +
			"If the deploy is still running the call parks this task and your run ends — that is correct, do not poll or wait, you will be woken with the answer. " +
			"`success` → nothing to write: the board already shows the release, so post no confirmation comment. " +
			"`no_signal` → nothing deployed this commit. Check whether the repository documents a deploy of its own (a deploy script, a Makefile target, the deploy steps in its README or .ai docs) " +
			"and, if it does, follow those steps with run_terminal and verify the environment answers afterwards. If it documents none — or the deploy could not run because GitHub Actions is unavailable " +
			"on this account (billing, spending limit, Actions disabled) and there is no local path either — move the task to `blocked` with that reason. " +
			"`failure` → read the log with get_deploy_logs, post a summary of what failed, then call rollback_task_release with trigger=deploy_failed and report everything it lists under manual_steps. " +
			"Do not test anything here (that happened in in_qa), do not edit or commit code, and do NOT move this task to `released` yourself: " +
			"the release is a production deploy dispatched by its own path, and moving the card there would announce a deploy that never happened."
	case domain.TaskColumnReleased:
		// A run only reaches this branch through the deploy-watch wake — the
		// sweeper handing back a task whose deploy finished, or an incident
		// attributed to this task's release (see deployWatchWake). `released`
		// dispatches nobody for anything else, so the generic default's
		// "move the task on to the next column" must never be reached here:
		// there is no next column, and a card past released is a card nobody
		// can find.
		return "This task is in `released`: its change is in production. You have been woken for the deploy watch and for nothing else. " +
			"Call get_task_deploy_status first — it tells you what production did with this task's merge commit. " +
			"If a rollback runbook was posted on this task, follow it: that is the developer's own instruction and it is the half no tool can perform. " +
			"`failure`, or an incident attributed to this release → get_deploy_logs, then rollback_task_release (trigger=deploy_failed or health_incident), " +
			"and report EVERY step it returns under manual_steps — a migration, a feature flag, anything with a human on the other end. " +
			"A rollback reported as complete when half of it was not is worse than one that says what it could not do. " +
			"If the rollback tool returns `proposed: true`, auto_rollback is off for this environment: post the proposal, say a human must confirm it, and stop. " +
			"Do not edit or commit code, do not move this task anywhere, and do not start any other work here."
	default:
		return fmt.Sprintf("This task is in `%s`. Do the work that column asks of your role in this run, then move the task on to the next column. "+
			"It is already in that column, so do not plan a move into it.", task.Column)
	}
}

// maxInjectedProfileChars caps the agent-maintained profile inside the run
// context: the profile is a brief, and past this size it is eating the budget
// the run needs for actual code.
const maxInjectedProfileChars = 8000

// profileKindForAgent maps the running agent's role onto the repo area whose
// profile sections it needs. Only a monorepo narrows: on a single-kind repo
// every section is already about the one thing the repo is, and dropping
// sections there would cost context for nothing.
func profileKindForAgent(agentName, repoKind string) string {
	if repoKind != domain.RepoKindMonorepo {
		return ""
	}
	name := strings.ToLower(agentName)
	switch {
	case strings.Contains(name, "frontend"), strings.Contains(name, "web"):
		return domain.RepoKindFrontend
	case strings.Contains(name, "mobile"), strings.Contains(name, "ios"), strings.Contains(name, "android"):
		return domain.RepoKindMobile
	case strings.Contains(name, "backend"), strings.Contains(name, "api"):
		return domain.RepoKindBackend
	case strings.Contains(name, "worker"):
		return domain.RepoKindWorker
	}
	return ""
}

func prependProjectContext(history []domain.Message, desc, profile string) []domain.Message {
	var note string
	if desc != "" {
		note = "Project context: " + desc
	}
	if profile != "" {
		if note != "" {
			note += "\n\n"
		}
		note += "## Project profile (maintained by agents)\n" + domain.TruncateHead(profile, maxInjectedProfileChars)
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

// cliFlavor maps a provider onto the agent CLI whose on-disk conventions its
// runs read, and reports false for every provider that is an HTTP endpoint.
//
// It is the one place that decides a run gets a materialised catalog rather
// than a prompt index, so a provider added without a line here keeps the old
// behaviour instead of silently getting an empty workspace.
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
