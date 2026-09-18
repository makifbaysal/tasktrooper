// Package claudecode delegates one board task to a headless Claude Code CLI
// session running on this host, instead of driving the task through the
// in-process agent loop.
//
// What is delegated is only the middle of a run. The board runner still does
// the deterministic setup before it — clone the repository into the task
// workspace and check out the tt-<key> branch — and still does every step
// after it: the grounding checks, the verify gate, the commit and push, the
// pull request, the column advance. The CLI is handed a prepared workspace and
// gives back a closing message; nothing else about a board run changes.
//
// Why a CLI at all: an agent on this provider runs on the operator's own Claude
// subscription rather than on a metered API key, and that subscription is
// something the `claude` binary already holds. There is no HTTP client this
// process could write that would have it.
//
// # Tool policy
//
// The run's domain.ToolPolicy governs BOTH halves of the session's tool
// surface, by two different mechanisms. The split is worth stating plainly,
// because only one of them is enforced at the point of execution.
//
// TaskTrooper's own tools reach the session over MCP (see mcp.go and
// internal/adapter/mcpserver), and that endpoint serves exactly
// DefinitionsForPolicy(run policy) — so a policy that denies move_board_task
// denies it here as well, at the point of execution and not just in the
// advertised list.
//
// The CLI's NATIVE tools are governed through the CLI's own mechanism rather
// than through the MCP endpoint: --tools selects from its built-in set, and
// buildArgs derives that selection from the same policy
// (domain.NativeToolsForPolicy). A policy that grants no terminal produces a
// session without Bash; one that grants no writes produces a session without
// Write or Edit.
//
// The translation is coarse, and worth stating plainly. TaskTrooper's tool
// names describe capabilities (read_file, run_terminal) and the CLI's describe
// implementations (Read, Bash), so the mapping is a judgement about which
// built-in grants the same reach — not an identity. Two consequences follow.
// Read, Glob and Grep are kept under every policy, because a session that
// cannot list a directory spends its turns guessing at paths and can read
// through its MCP tools anyway. Task (subagent spawning) is granted by no
// capability at all: it costs another whole session, and no TaskTrooper tool
// means "you may fan out".
//
// An empty policy still means unrestricted, here as everywhere else in the
// package: --tools is omitted and the CLI keeps its own default of every
// built-in.
package claudecode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// DefaultBinary is what CLAUDE_CODE_BIN falls back to: the name, resolved on
// PATH, rather than an absolute path. Where the binary lives differs per
// install (Homebrew, npm global, a version manager's shim) and PATH is the one
// thing all of them agree on.
const DefaultBinary = "claude"

// DefaultMaxTurns bounds one CLI session the way llm.task_max_iterations bounds
// a loop run. 100 is generous on purpose — the CLI does in one turn what the
// loop needs several for — and it is a circuit breaker, not a target: a session
// that hits it has stopped making progress, and the run comes back with what it
// had rather than spending the subscription on a loop.
const DefaultMaxTurns = 100

// DefaultRunTimeout bounds ONE session end to end.
//
// A subprocess has no equivalent of a provider's HTTP timeout: nothing else in
// this path ever gives up. A `claude` that wedges — a hung tool, a dead network
// mid-turn — holds its board run open forever,
// and the runner's heartbeat keeps stamping the row so the stale-run reconciler
// never sees an abandoned run either. Every recovery mechanism in the system is
// blind to it, which is why the bound has to be here.
//
// An hour is deliberately far above a real task (minutes) and far below "never".
// Config key: claude_code.run_timeout.
const DefaultRunTimeout = time.Hour

// DefaultMaxConcurrentSessions bounds how many CLI sessions this executor runs
// at once. The subscription's usage limit is shared by every session on this
// account, so N sessions that all hit it mid-work all park at once, and a
// burst on resume re-hits it; 3 keeps the burn sequential enough that the ones
// that started actually finish instead of all being cut off together.
const DefaultMaxConcurrentSessions = 3

// stderrTailMax is how much of the child's stderr is kept for the failure
// message. The tail, not the head: a CLI that dies says why on its last lines.
const stderrTailMax = 8 << 10

// DefaultSettingSources is which of the CLI's settings files a session loads.
//
// The operator's user-level settings (~/.claude) are deliberately absent. A
// local runner is somebody's working machine: their settings carry their hooks,
// their enabled plugins and their own permission rules, and all of it would run
// inside a board task. A SessionStart hook that injects a house style rewrites
// the agent's instructions; a PreToolUse hook that demands a confirmation blocks
// a tool call nobody is sitting there to approve. None of that is configuration
// anyone chose for TaskTrooper, and the failures it causes look like the agent
// misbehaving.
//
// Project and local settings DO load: they belong to the repository the task is
// in, which is the task's own context. Config key claude_code.setting_sources
// restores "user,project,local" for the one install that needs it — auth through
// a user-level apiKeyHelper. A subscription login keeps its credentials
// elsewhere and is unaffected.
const DefaultSettingSources = "project,local"

// knownSettingSources are the values the CLI's --setting-sources accepts.
var knownSettingSources = map[string]bool{"user": true, "project": true, "local": true}

// normalizeSettingSources keeps the sources the CLI knows, in the order given,
// without duplicates. Anything unrecognised is dropped rather than passed
// through: the flag is validated by the CLI at startup, so a typo in config
// would otherwise fail every run on this host with an error about an argument
// no operator typed.
func normalizeSettingSources(raw string) string {
	seen := map[string]bool{}
	kept := make([]string, 0, len(knownSettingSources))
	for _, part := range strings.Split(raw, ",") {
		source := strings.ToLower(strings.TrimSpace(part))
		if !knownSettingSources[source] || seen[source] {
			continue
		}
		seen[source] = true
		kept = append(kept, source)
	}
	if len(kept) == 0 {
		return DefaultSettingSources
	}
	return strings.Join(kept, ",")
}

type Config struct {
	// Binary is the CLI to run; empty means DefaultBinary. Resolved on PATH at
	// construction, so a typo is a boot-time log line rather than a failed run
	// an hour later.
	Binary string
	// MaxTurns: <= 0 means DefaultMaxTurns.
	MaxTurns int
	// RunTimeout bounds one session; <= 0 means DefaultRunTimeout.
	RunTimeout time.Duration
	// SettingSources selects the CLI settings files a session loads; empty means
	// DefaultSettingSources. See that constant for why the operator's own are
	// left out.
	SettingSources string
	// MCP is a FIXED endpoint for every run. Unset by default; used by the
	// tests and by any caller that has one endpoint and one credential. See
	// mcp.go.
	MCP MCPConfig
	// MCPProvider mints a per-RUN endpoint and credential instead, and is what
	// platform/runtime wires: the token is the run's identity at the endpoint,
	// so it cannot be shared and must not outlive the session. Set, it wins
	// over MCP.
	MCPProvider MCPProvider
	// MaxConcurrentSessions bounds how many CLI sessions run at once. 0 means
	// DefaultMaxConcurrentSessions; negative means unlimited — see
	// domain.ClaudeCodeConfig.MaxConcurrentSessions for why the cap exists.
	MaxConcurrentSessions int
}

// Executor runs board tasks through the Claude Code CLI. It satisfies
// port.TaskExecutor.
type Executor struct {
	bin        string
	maxTurns   int
	runTimeout time.Duration
	// settingSources is already normalised: New resolves the empty case to
	// DefaultSettingSources, so buildArgs never has to decide.
	settingSources string
	mcp            MCPConfig
	mcpProvider    MCPProvider
	// now is injectable so the quota park's fallback window is testable without
	// a clock.
	now func() time.Time

	// sem bounds concurrent sessions; nil means unlimited (MaxConcurrentSessions
	// configured negative). slotCap mirrors its capacity, or -1 when unlimited,
	// so SlotsInUse has an answer either way without reading cap(nil).
	sem     chan struct{}
	slotCap int
	// active is how many sessions currently hold a slot, tracked separately from
	// len(sem) so it still means something when sem is nil.
	active int64

	// gateMu guards the account-wide usage-limit gate: one session's 429 tells
	// every OTHER session about to start not to bother, rather than each of up
	// to MaxConcurrentSessions discovering the same spent subscription on its
	// own spawn.
	gateMu         sync.Mutex
	quotaUntil     time.Time
	quotaDetail    string
	quotaNextRetry time.Time
}

// quotaRetryInterval is how often a gated executor lets one session actually
// try the CLI instead of parking on the guessed reset. Sleeping until
// quotaUntil is right for a single account: nothing else would tell the gate
// to lift early. It is wrong for anyone swapping Claude Code accounts under
// this executor (e.g. a credential-store switcher) — this package has no way
// to know an account changed, so it never shortens quotaUntil for that, but a
// cheap, cadenced retry finds out empirically: the CLI's own success or
// failure is what actually clears or re-arms the gate (finish, above).
const quotaRetryInterval = 15 * time.Minute

var (
	_ port.TaskExecutor = (*Executor)(nil)
	// The same object serves both seams: a board task and a chat turn run
	// through the same CLI and the same subscription.
	_ port.ChatExecutor = (*Executor)(nil)
)

// ResolveBinary turns a configured binary name into an absolute path, or
// reports that this host does not have the CLI.
//
// Extracted from New so there is exactly ONE answer to "where is claude on this
// host". The connect flow (application/agentcli) has to resolve the same binary
// the executor will actually run, and a second lookup written next to it would
// be free to disagree — a connect that verified /usr/local/bin/claude while the
// executor ran a shim from a version manager is a verification that proved
// nothing about the thing being verified.
//
// An empty name means DefaultBinary; see that constant for why the default is a
// name on PATH rather than a path.
func ResolveBinary(configured string) (string, error) {
	bin := strings.TrimSpace(configured)
	if bin == "" {
		bin = DefaultBinary
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("claude code binary %q not found on PATH: %w", bin, err)
	}
	return resolved, nil
}

// New resolves the binary and returns the executor, or an error when the binary
// is not on PATH.
//
// An error rather than a degraded executor: the caller (platform/runtime) skips
// registration entirely on one, so an installation without the CLI has NO
// claude_code executor, and the board runner turns a claude_code agent's run
// into one clear failure — "claude code binary not available on this host" —
// instead of a session that dies at exec time with a message about a file
// nobody named.
func New(cfg Config) (*Executor, error) {
	resolved, err := ResolveBinary(cfg.Binary)
	if err != nil {
		return nil, err
	}
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	runTimeout := cfg.RunTimeout
	if runTimeout <= 0 {
		runTimeout = DefaultRunTimeout
	}
	slotCap := cfg.MaxConcurrentSessions
	if slotCap == 0 {
		slotCap = DefaultMaxConcurrentSessions
	} else if slotCap < 0 {
		slotCap = -1
	}
	var sem chan struct{}
	if slotCap > 0 {
		sem = make(chan struct{}, slotCap)
	}
	return &Executor{
		bin:            resolved,
		maxTurns:       maxTurns,
		runTimeout:     runTimeout,
		settingSources: normalizeSettingSources(cfg.SettingSources),
		mcp:            cfg.MCP,
		mcpProvider:    cfg.MCPProvider,
		now:            time.Now,
		sem:            sem,
		slotCap:        slotCap,
	}, nil
}

// Supports answers for the one provider this executor exists for. Nil-safe so a
// runner holding a nil executor asks the same question and gets "no".
func (e *Executor) Supports(provider domain.LLMProviderType) bool {
	return e != nil && provider == domain.LLMProviderClaudeCode
}

// armQuotaGate records that a session on this executor hit the usage limit, so
// every OTHER session about to spawn learns it from this in-memory check
// instead of independently paying for a 429 of its own. Only extends the
// gate, never shortens it: a session that started before the first park can
// still fail on the same window and must not overwrite an already-later
// estimate with an earlier one.
func (e *Executor) armQuotaGate(block *domain.QuotaBlock) {
	if block == nil {
		return
	}
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	if block.ResumeAt.After(e.quotaUntil) {
		e.quotaUntil = block.ResumeAt
		e.quotaDetail = block.Detail
	}
	e.quotaNextRetry = e.now().Add(quotaRetryInterval)
}

// clearQuotaGate lifts the gate. A session that just succeeded is proof the
// limit is no longer in force — including when the gate was armed on a false
// positive, such as an agent's own prose matching the text pattern — and
// nothing else would ever tell the gate to stop holding other sessions back.
func (e *Executor) clearQuotaGate() {
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	e.quotaUntil = time.Time{}
	e.quotaDetail = ""
	e.quotaNextRetry = time.Time{}
}

// QuotaGate reports the gate's current state, for observability.
func (e *Executor) QuotaGate() (until time.Time, armed bool) {
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	return e.quotaUntil, !e.quotaUntil.IsZero()
}

// quotaGateState is QuotaGate plus the detail Execute needs to word its own
// early return; kept unexported and separate so QuotaGate's public signature
// stays the two values callers outside the package actually want.
func (e *Executor) quotaGateState() (until time.Time, detail string, nextRetry time.Time, armed bool) {
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	return e.quotaUntil, e.quotaDetail, e.quotaNextRetry, !e.quotaUntil.IsZero()
}

// gatedQuotaBlock is the park Execute returns while the gate is armed, or nil.
// req.ResumeSessionID rides along so a re-parked task does not lose the CLI
// session it would resume.
func (e *Executor) gatedQuotaBlock(req domain.TaskExecution) *domain.QuotaBlock {
	until, detail, nextRetry, armed := e.quotaGateState()
	if !armed || !e.now().Before(until) {
		return nil
	}
	if !nextRetry.IsZero() && !e.now().Before(nextRetry) {
		// The retry cadence is due: let this session spawn for real rather than
		// park on the guessed reset. Its own outcome, through finish, is what
		// clears the gate (success — including a different, now-unspent account
		// swapped in underneath) or re-arms it with a fresh nextRetry (still
		// spent). Not consumed here so a second gatedQuotaBlock call for the
		// same request (after the concurrency slot is acquired) sees the same
		// "go" answer instead of finding the window already pushed forward.
		return nil
	}
	log.Info().
		Str("task_key", req.TaskKey).
		Time("resume_at", until).
		Msg("claude code usage limit gate is armed; parking without spawning")
	return &domain.QuotaBlock{
		ResumeAt:     until,
		CLISessionID: req.ResumeSessionID,
		Detail:       "another Claude Code session hit the usage limit: " + detail,
		Provider:     domain.LLMProviderClaudeCode,
	}
}

// SlotsInUse reports the concurrency cap's occupancy, for observability. cap
// is -1 when MaxConcurrentSessions was configured negative (unlimited).
func (e *Executor) SlotsInUse() (used, cap int) {
	return int(atomic.LoadInt64(&e.active)), e.slotCap
}

// acquireSlot blocks until a concurrency slot is free or ctx is cancelled.
// Skipped entirely when the cap is unlimited (sem is nil). It must run BEFORE
// resolveMCP: minting a per-run MCP token and then waiting on the semaphore
// would leave that credential alive and unused for however long the queue
// takes.
func (e *Executor) acquireSlot(ctx context.Context, taskKey string) error {
	if e.sem != nil {
		start := e.now()
		select {
		case e.sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		if waited := e.now().Sub(start); waited > time.Second {
			log.Info().
				Str("task_key", taskKey).
				Dur("waited", waited).
				Int("cap", e.slotCap).
				Msg("claude code session waited for a concurrency slot")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("claude_code_slot_wait", map[string]any{
					"waited_ms":      waited.Milliseconds(),
					"max_concurrent": e.slotCap,
				})
			}
		}
	}
	atomic.AddInt64(&e.active, 1)
	return nil
}

// releaseSlot is acquireSlot's counterpart. Callers defer it only after
// acquireSlot has returned successfully — releasing a slot that was never
// acquired would let one extra session through the semaphore and, on an
// unlimited executor, would drive active negative.
func (e *Executor) releaseSlot() {
	atomic.AddInt64(&e.active, -1)
	if e.sem != nil {
		<-e.sem
	}
}

// Execute runs the task in a CLI session and maps the session's outcome onto
// the response shape the board runner already reads.
func (e *Executor) Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	if e == nil {
		return domain.AgentResponse{}, errors.New("claude code executor is not configured")
	}
	// Never fall back to a default directory. The workspace is the ONLY place
	// this run's branch is checked out; running anywhere else would edit the
	// shared project root, on whatever branch it happens to be on.
	if strings.TrimSpace(req.WorkDir) == "" {
		return domain.AgentResponse{}, errors.New("claude code executor: no task workspace to run in")
	}

	// The account-wide gate, checked before anything else costs a subprocess or
	// an MCP token: one session's 429 means every other session on this
	// executor is spending against the same spent subscription, and there is
	// nothing a fresh spawn would learn that this session did not already pay
	// to find out. req.ResumeSessionID rides along so a re-parked task does not
	// lose the CLI session it would resume.
	if block := e.gatedQuotaBlock(req); block != nil {
		return domain.AgentResponse{}, block
	}

	if err := e.acquireSlot(ctx, req.TaskKey); err != nil {
		return domain.AgentResponse{}, err
	}
	defer e.releaseSlot()
	// Checked again once the slot is held: a session that queued behind the
	// cap for minutes may have watched every running session park meanwhile,
	// and spawning it now would only rediscover the same spent window.
	if block := e.gatedQuotaBlock(req); block != nil {
		return domain.AgentResponse{}, block
	}

	// Both defers run on every exit
	// path this function has — a finished session, a failure, and the quota
	// park, which returns a typed error like any other.
	// RequiresTools is unconditional — every task needs the board tools — while
	// SkillsOnDisk comes off the request, because only the caller that
	// materialised the workspace knows whether it did. The router's own CLI runs
	// (application/agent/router.go) go through here too, in a scratch directory
	// nothing was written into, and hardcoding true would take load_skill away
	// from them for nothing.
	mcpCfg, releaseMCP, err := e.resolveMCP(ctx, MCPRun{
		Policy:        req.Policy,
		Label:         req.TaskKey,
		RequiresTools: true,
		SkillsOnDisk:  req.SkillsOnDisk,
	})
	defer releaseMCP()
	if err != nil {
		return domain.AgentResponse{}, err
	}

	mcpPath, cleanupMCP, err := writeMCPConfigFile(mcpCfg)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	defer cleanupMCP()

	sessionEnv, refusedEnv := domain.SessionEnv(req.Env)
	if len(refusedEnv) > 0 {
		log.Warn().Strs("names", refusedEnv).Str("task", req.TaskKey).
			Msg("claude code: dropped session environment outside the allowlist")
	}

	fresh := func() invocation {
		systemPrompt, prompt := flattenHistory(req.History)
		// Last, so the names sit under the persona and the task rather than
		// above them.
		systemPrompt = withToolManifest(systemPrompt, mcpCfg.Tools)
		return invocation{
			workDir:      req.WorkDir,
			systemPrompt: systemPrompt,
			prompt:       prompt,
			model:        req.Model,
			maxTurns:     req.MaxTurns,
			effort:       req.Effort,
			tools:        domain.NativeToolsForPolicy(req.Policy),
			label:        req.TaskKey,
			mcpPath:      mcpPath,
			env:          sessionEnv,
		}
	}

	inv := fresh()
	resumeSessionID := strings.TrimSpace(req.ResumeSessionID)
	if resumeSessionID != "" {
		// The session already holds the persona, the project context and the
		// task: replaying them would spend the tokens again and, worse, read as
		// a NEW instruction on top of half-finished work. What it needs is the
		// one thing it does not know — that the wait is over.
		inv.systemPrompt = ""
		if followUp := strings.TrimSpace(req.Prompt); followUp != "" {
			// A caller that already knows what the resumed session should do next
			// (a criteria sweep, a fix round) sends that instruction verbatim
			// instead of the generic "the wait is over" — the session does not
			// need to be told twice that it was parked.
			inv.prompt = followUp
		} else {
			inv.prompt = continuePrompt(req)
		}
		inv.resumeSessionID = req.ResumeSessionID
	}

	s, err := e.spawn(ctx, inv)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	// A resume the CLI cannot honour. The quota park window can span hours, and
	// the CLI's own session storage on this host is pruned on its own schedule
	// — none of which makes the task's work disposable. Falling straight
	// through would hand the board a generic "failed" run, spending one of the
	// task's three consecutive-failure lives on a park the sweeper faithfully
	// resumed; three of those and the reconciler gives up silently, and the
	// card is stuck for good even after the quota is back. See chat.go, which
	// solved the same refusal for a conversation.
	if resumeSessionID != "" && resumeRefused(s) {
		log.Info().
			Str("task_key", req.TaskKey).
			Str("cli_session_id", resumeSessionID).
			Msg("claude code could not resume this task's cli session; starting a fresh one from the stored history")
		retry := fresh()
		retry.trace = s.trace
		if s, err = e.spawn(ctx, retry); err != nil {
			return domain.AgentResponse{}, err
		}
	}

	return e.finish(ctx, req.TaskKey, s)
}

// invocation is one spawn of the CLI: everything that goes on the command line
// plus the two things the stream needs (who to attribute it to, where to
// forward the text).
//
// It exists because a board task and a chat turn differ only in how these
// fields are filled — the spawn, the parse, the drain, the wait and the
// deadline are identical, and were worth having exactly once. A chat turn also
// spawns TWICE on the resume-refused path, which is only affordable when a
// spawn is a value away.
type invocation struct {
	workDir string
	// env is the checkout's own version pins, already allowlisted. It goes
	// after childEnv because exec keeps the last value for a repeated name,
	// so the repository's pin beats the host's value for the same name.
	env             []string
	systemPrompt    string
	prompt          string
	model           string
	resumeSessionID string
	// maxTurns overrides the executor-wide ceiling for this one spawn. 0 keeps
	// the executor's own value, which is what a caller with no per-agent
	// setting passes.
	maxTurns int
	// effort is the CLI --effort level for this spawn. Empty omits the flag and
	// leaves the CLI on its default.
	effort string
	// tools narrows the CLI's BUILT-IN tool surface for this spawn. nil omits
	// --tools, which leaves every built-in available — see
	// domain.NativeToolsForPolicy for why an unrestricted policy maps to nil
	// rather than to the full list spelled out.
	tools []string
	// label names the caller in logs and in the trace: a board task's key, or a
	// chat session's id.
	label string
	// mcpPath is the per-run --mcp-config file, already written. Empty means the
	// session runs on the CLI's native tools only.
	mcpPath string
	// trace, when set, is reused instead of a fresh sink.
	//
	// The chat path spawns TWICE on the resume-refused fallback, and both spawns
	// write into the same run's trace. A second sink would restart the turn
	// numbering at 1, and a reader that groups steps by turn number — which the
	// SPA's timeline does — would fold the fresh session's rounds into the
	// refused attempt's instead of showing them after it.
	trace *traceSink
	// stream forwards assistant text as it arrives. The zero value reports
	// nothing, which is what a board run wants: its answer is read once, when
	// the session has finished.
	stream port.ChatStream
}

// spawn runs one CLI session to completion and returns everything it produced.
//
// The error return is only for failures BEFORE the child was running (a pipe
// that could not be opened, a binary that would not start). Everything the
// session itself did — including dying — comes back in the session value, so
// that finish is the single place that decides what an outcome means.
func (e *Executor) spawn(ctx context.Context, inv invocation) (session, error) {
	// The session's own deadline. Nothing else in this path ever gives up —
	// see DefaultRunTimeout.
	runCtx, cancelRun := context.WithTimeout(ctx, e.runTimeout)
	defer cancelRun()

	systemPromptPath, cleanupSystemPrompt, err := writeSystemPromptFile(inv.systemPrompt)
	if err != nil {
		return session{}, err
	}
	defer cleanupSystemPrompt()

	args := e.buildArgs(inv, systemPromptPath)
	cmd := exec.CommandContext(runCtx, e.bin, args...)
	cmd.Dir = inv.workDir
	cmd.Env = append(childEnv(ctx), inv.env...)
	cmd.Stdin = strings.NewReader(inv.prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return session{}, fmt.Errorf("claude code stdout: %w", err)
	}
	stderr := &tailWriter{max: stderrTailMax}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return session{}, fmt.Errorf("start claude code: %w", err)
	}

	// The sink reports on the PARENT context, not runCtx: its writes are the
	// run's trace and tool ledger, and they must still be attributable to the
	// run after this session's deadline has fired.
	trace := inv.trace
	if trace == nil {
		trace = &traceSink{ctx: ctx, taskKey: inv.label}
	}
	// streamingSink is a pass-through when nobody is listening, so a board run
	// pays nothing for the chat path existing. The guard wraps it to read the
	// session's opening inventory — everything else passes through untouched.
	guard := &initGuard{
		sink:    newStreamingSink(trace, inv.stream),
		label:   inv.label,
		require: inv.mcpPath != "",
		cancel:  cancelRun,
	}
	out, parseErr := parseStream(stdout, guard)
	// A parse that stopped early (a line past the ceiling, a read error) left
	// the pipe with unread bytes in it. Wait blocks until the child exits, and
	// the child blocks writing into a full pipe nobody is draining — so the two
	// wait for each other until the run's deadline. Drain first; the bytes are
	// discarded because the parse has already given up on them.
	if parseErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	// Wait AFTER the stream is drained: waiting first closes the pipe and loses
	// whatever had not been read yet.
	waitErr := cmd.Wait()

	// A deadline this executor imposed, not one the caller did: the caller's
	// cancellation (stop button, pod drain) is reported as itself further down.
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil

	return session{
		out:        out,
		trace:      trace,
		stderrTail: stderr.String(),
		parseErr:   parseErr,
		waitErr:    waitErr,
		timedOut:   timedOut,
		initFault:  guard.fault,
	}, nil
}

// initGuard reads the session's opening inventory and stops a session that
// started without the tools its run needs.
//
// It embeds sink, so every other event passes straight through to the trace and
// the chat stream underneath and this type stays the size of the one question it
// answers: did the CLI actually load THIS run's tool endpoint?
//
// The check is absence, not status. A server the CLI lists as "pending" is
// mid-handshake — the init event routinely outruns it — while a server named in
// --mcp-config that the CLI does not list at all was never loaded, which is a
// configuration fault and not a race. Killing on "pending" would fail healthy
// runs; killing on absence fails exactly the runs that would otherwise spend a
// whole session on the CLI's native tools, return a plausible summary, be
// refused by the criteria gate for an untouched checklist, and be dispatched
// again — the silent, unbounded loop MCPRun.RequiresTools exists to prevent.
type initGuard struct {
	sink
	label   string
	require bool
	cancel  context.CancelFunc
	// fault is what the run failed with, read by spawn after the parse has
	// finished. Written from the parse, which runs in the caller's goroutine, so
	// there is nothing here to synchronise.
	fault error
}

func (g *initGuard) OnInit(init sessionInit) {
	// One line per session, and it is the line to read when a run behaved as
	// though the board did not exist: which servers the CLI loaded, and how many
	// native tools it had beside them.
	log.Info().
		Str("task_key", g.label).
		Str("cli_session_id", init.SessionID).
		Int("native_tools", len(init.Tools)).
		Strs("mcp_servers", init.serverNames()).
		Msg("claude code session started")

	// A CLI that did not report its servers is not evidence of anything, so it
	// is never grounds for a kill: the check would then fail every run on a
	// build whose init event simply carries less.
	if !g.require || !init.ServersReported {
		return
	}
	if _, listed := init.server(mcpServerName); listed {
		return
	}
	g.fault = fmt.Errorf(
		"the claude code session started without the %s tool server (it loaded %v), so it could not move its card, "+
			"tick an acceptance criterion or record a verdict; the run was stopped instead of being left to finish blind",
		mcpServerName, init.serverNames())
	log.Error().
		Str("task_key", g.label).
		Str("cli_session_id", init.SessionID).
		Strs("mcp_servers", init.serverNames()).
		Msg("claude code session did not load the tasktrooper tool server; stopping the run")
	g.cancel()
}

// session is everything one CLI invocation produced. Grouped into a struct
// because finish reads all of it together and a seventh positional argument
// would be one more thing to get in the wrong order.
type session struct {
	out        outcome
	trace      *traceSink
	stderrTail string
	parseErr   error
	waitErr    error
	timedOut   bool
	// initFault is set when the guard stopped the session at its init event. It
	// is reported ahead of everything else, because the cancellation it caused
	// would otherwise surface as a nondescript "ended without a result".
	initFault error
}

// failed reports whether the session ended badly.
//
// It is what gates quota detection, and that gate is the whole point: the limit
// is recognised from TEXT (there is no distinct status for it), and a
// SUCCESSFUL run whose final answer merely quotes the phrase — an agent
// reporting "the previous attempt stopped because the usage limit was reached",
// which is exactly what a resumed run is likely to write — would have its work
// thrown away and the card parked. Worse, the resumed run would write the same
// sentence again, so the card would park forever on its own summary. A finished
// session's text is never searched now.
// sessionID is the CLI conversation this session ran in.
//
// Two sources because the id arrives twice and either can be the only one: the
// terminal result event carries it, and so does the init event the trace sink
// caught on the way past — which is the one that survives when the session died
// before finishing, exactly the case a park or a chat resume needs it for.
func (s session) sessionID() string {
	return firstNonEmpty(s.out.SessionID, s.trace.SessionID())
}

func (s session) failed() bool {
	if s.timedOut || s.parseErr != nil || s.waitErr != nil || !s.out.SawResult || s.out.IsError {
		return true
	}
	switch s.out.Subtype {
	case "", "success", "error_max_turns":
		// error_max_turns is a budget, not a failure: the session did real work
		// and hands it back. See finish.
		return false
	default:
		return true
	}
}

// finish turns the session's outcome into either a response or a typed error.
// Split out of Execute so the mapping — which is all the behaviour worth
// testing — can be exercised without spawning a process.
//
// label is the caller's name for the logs: a board task's key, or a chat
// session's id. finish never needed more of the request than that, which is why
// it takes a string rather than one of the two execution types — it is the same
// mapping for both, and it must stay that way.
//
// The receiver is sessionFinisher rather than *Executor because there are now
// TWO executors — this one, which spawns the CLI here, and the remote one,
// which asks a Mac to spawn it (remote.go) — and the mapping from "what the
// session did" to "what the board is told" must be the same for both. A second
// copy is how a run on somebody's laptop would start reporting a spent quota,
// a max-turns budget or an answerless session differently from a run here.
type sessionFinisher struct {
	runTimeout time.Duration
	maxTurns   int
	now        func() time.Time
}

// finish delegates the mapping to sessionFinisher (shared with the remote
// executor) and then updates the account-wide gate: a QuotaBlock arms it, and
// a nil error clears it, whichever of Execute or ExecuteChat called in. Chat
// deliberately shares this clearing half — a successful chat turn is just as
// good a proof the limit lifted as a board run finishing — while only Execute
// consults the gate on the way in, since a chat has a person watching who
// should see the notice immediately rather than being queued behind it.
func (e *Executor) finish(ctx context.Context, label string, s session) (domain.AgentResponse, error) {
	resp, err := sessionFinisher{runTimeout: e.runTimeout, maxTurns: e.maxTurns, now: e.now}.finish(ctx, label, s)
	if err == nil {
		e.clearQuotaGate()
		return resp, nil
	}
	if block, ok := domain.QuotaBlockOf(err); ok {
		e.armQuotaGate(block)
	}
	return resp, err
}

func (f sessionFinisher) finish(ctx context.Context, label string, s session) (domain.AgentResponse, error) {
	out, stderrTail := s.out, s.stderrTail
	// Token spend goes onto the run row through the same accumulator the loop's
	// recording client feeds, so the task detail shows what this run cost.
	//
	// It is deliberately NOT recorded to llm_usage: that table drives the
	// USD budget, and these tokens were paid for by a flat-rate
	// subscription. Billing them would charge it twice and could pause
	// the board on a budget nothing was actually drawn from. The CLI's own
	// total_cost_usd is logged and traced instead — it is the API-equivalent
	// price of the session, useful to see, wrong to bill.
	usageapp.TokenUsageFromContext(ctx).Add(out.Usage)

	sessionID := s.sessionID()

	// The guard's kill is reported as itself. It happened at the init event, so
	// everything below — the timeout, the quota scan, the missing terminal
	// event — would describe the consequence rather than the cause.
	if s.initFault != nil {
		return domain.AgentResponse{}, s.initFault
	}

	// A deadline this executor imposed is a plain failure, checked before the
	// quota so a wedged session can never be mistaken for a spent subscription:
	// parking on a hang would wait out the window and then hand the same hang
	// another hour.
	if s.timedOut {
		return domain.AgentResponse{}, fmt.Errorf(
			"claude code did not finish within %s and was stopped (cli session %s): %s",
			f.runTimeout, sessionID, domain.TruncateHead(strings.TrimSpace(stderrTail), 500))
	}

	// Quota detection runs on a FAILED session only. See session.failed.
	if s.failed() {
		if block := quotaBlockFrom(out, stderrTail, sessionID, f.now()); block != nil {
			log.Warn().
				Str("task_key", label).
				Str("cli_session_id", sessionID).
				Time("resume_at", block.ResumeAt).
				Msg("claude code usage limit reached, parking the task")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("claude_code_quota_park", map[string]any{
					"resume_at":      block.ResumeAt.UTC().Format(time.RFC3339),
					"cli_session_id": sessionID,
					"detail":         block.Detail,
				})
			}
			return domain.AgentResponse{}, block
		}
	}
	if s.parseErr != nil {
		return domain.AgentResponse{}, s.parseErr
	}
	// No terminal event means the session did not finish: killed, crashed, or
	// stopped by the run's own cancellation. Reporting the partial text as an
	// answer would hand the board a half-run to commit and hand off.
	if !out.SawResult {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.AgentResponse{}, ctxErr
		}
		if sig, ok := domain.ExitSignal(s.waitErr); ok {
			log.Warn().
				Str("task_key", label).
				Str("cli_session_id", sessionID).
				Str("signal", sig.String()).
				Msg("claude code session was killed by an external signal")
			return domain.AgentResponse{}, fmt.Errorf(
				"claude code was killed by signal %s from outside this run (cli session %s): %s",
				sig, sessionID, domain.TruncateHead(strings.TrimSpace(stderrTail), 500))
		}
		return domain.AgentResponse{}, fmt.Errorf("claude code ended without a result (%v): %s",
			s.waitErr, domain.TruncateHead(strings.TrimSpace(stderrTail), 500))
	}

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("claude_code_result", map[string]any{
			"subtype": out.Subtype,
			// num_turns is the CLI's own count (both sides of the conversation);
			// turns is how many assistant turns this trace actually brackets, so
			// the footer and the iterations above it agree on a number.
			"num_turns":      out.NumTurns,
			"turns":          s.trace.Turns(),
			"cost_usd":       out.CostUSD,
			"tool_calls":     out.ToolCalls,
			"tool_failures":  out.ToolFailures,
			"cli_session_id": sessionID,
			"model":          s.trace.Model(),
		})
	}
	log.Info().
		Str("task_key", label).
		Str("cli_session_id", sessionID).
		Str("subtype", out.Subtype).
		Int("turns", out.NumTurns).
		Float64("cost_usd", out.CostUSD).
		Msg("claude code session finished")

	resp := domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: out.Text},
		Usage:   out.Usage,
		// The session a follow-up step (a criteria sweep, a fix round) resumes
		// instead of replaying the whole context. Set here rather than only on
		// the quota-block error path, because a resumable session is just as
		// real when the run succeeded outright or stopped on its turn budget.
		CLISessionID: sessionID,
	}

	switch {
	case out.Subtype == "error_max_turns":
		// The turn budget is the loop's giveUp, not a failure: the session did
		// real work and the runner's gates judge it on that work. The note is
		// appended so the run summary says why the answer stops where it does.
		resp.Message.Content = strings.TrimSpace(out.Text + "\n\n" + maxTurnsNote(f.maxTurns))
		return resp, nil
	case out.IsError || (out.Subtype != "" && out.Subtype != "success"):
		return domain.AgentResponse{}, fmt.Errorf("claude code failed (%s): %s",
			out.Subtype, domain.TruncateHead(firstNonEmpty(out.Text, strings.TrimSpace(stderrTail)), 1000))
	case s.waitErr != nil:
		// A success event with a non-zero exit is contradictory. Trust the exit
		// code: it is the kernel's report, and the event is the process's own.
		return domain.AgentResponse{}, fmt.Errorf("claude code exited with an error after reporting success (%v): %s",
			s.waitErr, domain.TruncateHead(strings.TrimSpace(stderrTail), 500))
	}
	if strings.TrimSpace(resp.Message.Content) == "" {
		// Same rule the loop applies to an empty final turn: an answerless run
		// is not an answer, and the gates downstream read this text.
		return domain.AgentResponse{}, errors.New("claude code finished without producing any answer")
	}
	return resp, nil
}

func maxTurnsNote(maxTurns int) string {
	return fmt.Sprintf("[The Claude Code session stopped at its %d-turn budget; anything above is what it had finished by then.]", maxTurns)
}

// Neither prompt is on the command line: -p with no positional reads the
// prompt from stdin, and the system prompt travels as a file. Both name the
// processes the agent is told to start (vite, agent-server, npm run dev), and
// an agent tidying up with `pkill -f vite` matches every command line that
// contains the word — its own CLI and every other task's — which ended whole
// runs with a bare "exit status 143".
func (e *Executor) buildArgs(inv invocation, systemPromptPath string) []string {
	// Order is fixed rather than assembled from a map so two runs with the same
	// inputs produce byte-identical command lines — which is what makes a
	// failure reproducible from a log line.
	args := []string{"-p"}
	if sid := strings.TrimSpace(inv.resumeSessionID); sid != "" {
		args = append(args, "--resume", sid)
	}
	// A per-agent ceiling wins over the executor-wide one; 0 means the agent
	// carries no opinion and the executor's default stands.
	maxTurns := inv.maxTurns
	if maxTurns <= 0 {
		maxTurns = e.maxTurns
	}
	args = append(args,
		"--output-format", "stream-json",
		// --verbose is REQUIRED by the CLI alongside stream-json in -p mode;
		// without it the stream is a single result object and every assistant
		// turn and tool call along the way is lost to the transcript.
		"--verbose",
		// The workspace is a throwaway clone on a task branch and there is no
		// human at this terminal to answer a prompt: without this the session
		// blocks on its first edit until the run is cancelled.
		"--dangerously-skip-permissions",
		"--max-turns", strconv.Itoa(maxTurns),
		// Deny rules hold under --dangerously-skip-permissions. Both commands
		// match by NAME or command line across the whole machine, so one task's
		// cleanup of "agent-server" or "vite" takes down the backend serving it
		// and every other task's dev servers. Refused, the agent falls back to
		// killing the PID it started.
		"--disallowedTools", disallowedBashCommands,
		// Which settings files the session loads. The operator's own are out by
		// default: their hooks and plugins would run inside a board task, where
		// nothing chose them and their effects read as the agent misbehaving.
		// See DefaultSettingSources.
		"--setting-sources", e.settingSources,
	)
	if systemPromptPath != "" {
		args = append(args, "--append-system-prompt-file", systemPromptPath)
	}
	if inv.effort != "" {
		args = append(args, "--effort", inv.effort)
	}
	// --tools selects from the CLI's BUILT-IN set; the MCP half is narrowed
	// separately by the policy that produced this list, at the endpoint that
	// serves it. Empty means "say nothing and take the CLI's default", which is
	// how an unrestricted policy reaches here.
	if len(inv.tools) > 0 {
		args = append(args, "--tools", strings.Join(inv.tools, ","))
	}
	// An empty model means "omit --model entirely", which hands the choice to
	// whatever the operator's CLI is configured with. That is not an oversight
	// to be tidied up with a default: it is the first option the model picker
	// offers (domain.ClaudeCodeModels) and the one most claude_code agents want,
	// because this provider exists to run on the operator's own subscription and
	// their own default is part of that.
	if model := strings.TrimSpace(inv.model); model != "" {
		args = append(args, "--model", model)
	}
	if inv.mcpPath != "" {
		args = append(args, "--mcp-config", inv.mcpPath,
			// THIS run's endpoint and nothing else. Without it the CLI merges the
			// operator's own MCP configuration on top, and a developer host
			// routinely carries half a dozen personal servers (a task tracker, a
			// design tool, a browser driver, mail) — so the session is handed their
			// hundreds of tools beside TaskTrooper's thirty.
			//
			// Two things then go wrong and the second is the expensive one. A board
			// run can act on a stranger's workspace, and the CLI, past its
			// tool-count threshold, stops sending tool schemas up front and makes
			// the model FIND them with ToolSearch instead. The run then spends turn
			// after turn guessing names for tools it was already granted —
			// list_acceptance_criteria, criteria_list, get_criteria — and one
			// observed session burned ten iterations that way without ever
			// reaching the tool it needed to tick a criterion.
			"--strict-mcp-config")
	}
	return args
}

// gh pr merge is denied here, not just discouraged in the done-column system
// prompt, because the CLI's native Bash bypasses run_terminal's own sandbox
// entirely (see domain.cli_native_tools.go) — without this, an agent can
// land a PR with failing CI before the board's own pipeline gate ever runs.
const disallowedBashCommands = "Bash(pkill:*),Bash(killall:*),Bash(gh pr merge:*)"

// writeSystemPromptFile puts the system prompt where --append-system-prompt-file
// reads it and returns the path with a cleanup that is always non-nil. In the
// OS temp dir for the same reason the MCP config is: the workspace is a
// checkout the agent commits from.
func writeSystemPromptFile(systemPrompt string) (string, func(), error) {
	noop := func() {}
	if systemPrompt == "" {
		return "", noop, nil
	}
	f, err := os.CreateTemp("", "tt-claude-system-*.md")
	if err != nil {
		return "", noop, fmt.Errorf("create system prompt file: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", noop, fmt.Errorf("secure system prompt file: %w", err)
	}
	if _, err := f.WriteString(systemPrompt); err != nil {
		f.Close()
		cleanup()
		return "", noop, fmt.Errorf("write system prompt file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("close system prompt file: %w", err)
	}
	return path, cleanup, nil
}

// continuePrompt is what a resumed session is told. Short on purpose: the
// session holds the task, the workspace and its own half-finished work, and a
// re-statement of the task would read as a second, competing instruction.
func continuePrompt(req domain.TaskExecution) string {
	task := strings.TrimSpace(req.TaskKey + " " + req.TaskTitle)
	if task == "" {
		task = "this task"
	}
	return "The usage limit that interrupted you has reset. Continue " + task +
		" from where you stopped in this same workspace: finish the remaining work, then reply with a short summary of what you changed."
}

// flattenHistory folds the runner's message list into the two strings the CLI
// takes.
//
// Deterministic by construction: it walks the slice in order and never sorts or
// maps, so the same history always produces the same two strings — which is
// what lets a resumed or retried run be compared with the one before it.
//
// System blocks become one --append-system-prompt-file body ("append" because
// the CLI keeps its own system prompt underneath; this is the agent's persona,
// project context and evidence on top of it). Everything else becomes the
// prompt on stdin. Assistant turns are labelled rather than dropped: a
// revision run's history can contain the previous attempt, and silently losing
// it would make the run repeat work it was told about.
func flattenHistory(history []domain.Message) (systemPrompt, prompt string) {
	var system, user []string
	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch msg.Role {
		case domain.RoleSystem:
			system = append(system, content)
		case domain.RoleAssistant:
			user = append(user, "Earlier assistant turn:\n"+content)
		default:
			user = append(user, content)
		}
	}
	return strings.Join(system, "\n\n"), strings.Join(user, "\n\n")
}

// claudeEnvPassthrough are the variables Claude Code needs for ITS OWN
// configuration and authentication, which childenv's allowlist has no reason to
// know about.
//
// The rule that decides this list: a name belongs here only if the CLI reads it
// to find its own subscription or config. Nothing that authenticates
// agent-server may be here — DATABASE_URL, INTERNAL_AUTH_KEY and MCP_SECRETS_KEY
// are already gone from this process's environment (platform/runtime/envscrub.go)
// and childenv drops the rest, and this overlay must not put any of them back.
//
// ANTHROPIC_API_KEY is deliberately absent even though the CLI would accept it.
// It is a credential this server holds for its own HTTP provider, and
// forwarding it would both hand a child process a secret it was never given and
// silently move the session off the subscription onto metered API billing — the
// opposite of why this provider exists.
var claudeEnvPassthrough = []string{
	// Where the CLI keeps its settings and credentials when it is not ~/.claude.
	"CLAUDE_CONFIG_DIR",
	// The subscription token for a non-interactive login (CI-style auth). This
	// IS Claude Code's own credential, which is exactly what it is here for.
	"CLAUDE_CODE_OAUTH_TOKEN",
	// Corporate gateway routing for the CLI itself. Not credentials: a bedrock
	// or vertex flag and a base URL.
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	// Standard proxy configuration — the CLI has to reach the API through
	// whatever the host reaches the internet through.
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	"http_proxy", "https_proxy", "no_proxy",
}

// childEnv builds the CLI's environment: childenv's scrubbed base (PATH, HOME,
// the toolchain surface, TLS trust — never this pod's secrets), the per-task
// toolchain overlay the rest of the run uses, and the Claude-specific names
// above.
func childEnv(ctx context.Context) []string {
	parent := os.Environ()
	overlay := append([]string{}, registry.TaskEnvFromContext(ctx)...)
	for _, name := range claudeEnvPassthrough {
		if value, ok := os.LookupEnv(name); ok {
			overlay = append(overlay, name+"="+value)
		}
	}
	return childenv.For(parent, overlay)
}

// tailWriter keeps the LAST max bytes written to it. A failing CLI explains
// itself on its final lines, and an unbounded buffer would let a chatty child
// (a build log on stderr) grow this process's memory without limit.
type tailWriter struct {
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.max > 0 && n > w.max {
		p = p[n-w.max:]
	}
	w.buf = append(w.buf, p...)
	if w.max > 0 && len(w.buf) > w.max {
		w.buf = w.buf[len(w.buf)-w.max:]
	}
	return n, nil
}

func (w *tailWriter) String() string { return string(bytes.TrimSpace(w.buf)) }
